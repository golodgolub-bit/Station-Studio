package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type appCatalogItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Package   string `json:"package"`
	Note      string `json:"note"`
	Source    string `json:"source"`
	Version   string `json:"version,omitempty"`
	Installed bool   `json:"installed"`
	Enabled   bool   `json:"enabled"`
}
type appInstallJob struct {
	Active bool   `json:"active"`
	ID     string `json:"id"`
	Phase  string `json:"phase"`
	Error  string `json:"error,omitempty"`
}

var appInstallMu sync.Mutex
var currentAppInstall appInstallJob

const launcherURL = "https://github.com/spocky/miproja1/releases/download/4.73/ProjectivyLauncher-4.73-c98-xda-release.apk"
const launcherSHA = "74e33af1e5e13831a69a0669d2b68b02150bf9df1ae1df437dc67e3f8d52c790"
const launcherBytes = 11437133

type officialAPK struct {
	URL, SHA, Label, Package string
	Bytes                    int64
}

var officialAPKs = map[string]officialAPK{
	"projectivy": {launcherURL, launcherSHA, "Projectivy Launcher 4.73", "com.spocky.projengmenu", launcherBytes},
	"airplay":    {"https://github.com/jqssun/android-airplay-server/releases/download/v0.0.36/app-release.apk", "b8c7c595ed93f91562feedce92c27800f08f9c84df424f6ac2045e82324cf2a4", "AirPlay Server 0.0.36", "io.github.jqssun.airplay", 27335496},
}

func appCatalog() []appCatalogItem {
	list := []appCatalogItem{
		{ID: "airplay", Name: "AirPlay", Package: "io.github.jqssun.airplay", Note: "Музыка и экран с iPhone, iPad и Mac. Бесплатный открытый приёмник; устройства должны быть в одной сети. DRM-видео не поддерживается.", Source: "github", Version: "0.0.36"},
		{ID: "projectivy", Name: "Projectivy Launcher", Package: "com.spocky.projengmenu", Note: "Главный экран для пульта. Проверенный официальный APK 4.73; без замены аккаунтов.", Source: "github", Version: "4.73"},
		{ID: "spotify", Name: "Spotify для ТВ", Package: "com.spotify.tv.android", Note: "Официальная версия Android TV. Обновления устанавливает Google Play. Поиск и команды проверяются отдельно.", Source: "play"},
		{ID: "youtube", Name: "YouTube для Android TV", Package: "com.google.android.youtube.tv", Note: "Версия для телевизора с поддержкой пульта. Отличается от мобильного YouTube.", Source: "play"},
		{ID: "netflix", Name: "Netflix для Android TV", Package: "com.netflix.ninja", Note: "Установка зависит от совместимости модели и сертификации. Панель не обходит ограничения Netflix.", Source: "play"},
		{ID: "vlc", Name: "VLC", Package: "org.videolan.vlc", Note: "Видео и локальная музыка. Имеет интерфейс для Android TV и медиасессию.", Source: "play"},
		{ID: "play", Name: "Google Play", Package: "com.android.vending", Note: "Магазин и сертификация — разные вещи. На этой станции используется отдельный профиль установки служб Google.", Source: "system"},
	}
	out, _ := run(5*time.Second, "/system/bin/pm", "list", "packages")
	disabled, _ := run(5*time.Second, "/system/bin/pm", "list", "packages", "-d")
	for i := range list {
		list[i].Installed = strings.Contains("\n"+out+"\n", "\npackage:"+list[i].Package+"\n")
		list[i].Enabled = list[i].Installed && !strings.Contains("\n"+disabled+"\n", "\npackage:"+list[i].Package+"\n")
	}
	return list
}
func appCatalogAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		list := appCatalog()
		appInstallMu.Lock()
		job := currentAppInstall
		appInstallMu.Unlock()
		response(w, map[string]any{"apps": list, "job": job})
		return
	}
	if r.Method != "POST" {
		fail(w, "Нужен GET или POST", 405)
		return
	}
	var q struct {
		ID     string `json:"id"`
		Action string `json:"action"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		fail(w, "Выберите приложение", 400)
		return
	}
	var item *appCatalogItem
	for _, v := range appCatalog() {
		if v.ID == q.ID {
			v := v
			item = &v
			break
		}
	}
	if item == nil {
		fail(w, "Приложения нет в каталоге", 404)
		return
	}
	if q.Action == "open-store" && item.Source == "play" {
		out, e := run(10*time.Second, "/system/bin/am", "start", "-a", "android.intent.action.VIEW", "-d", "market://details?id="+item.Package, "-p", "com.android.vending")
		if e != nil || strings.Contains(out, "Error") {
			fail(w, "Не удалось открыть Google Play на станции", 409)
			return
		}
		response(w, map[string]bool{"ok": true})
		return
	}
	if q.Action == "launch" && item.Installed {
		if !item.Enabled {
			fail(w, "Приложение отключено. Сначала включите его в карточке.", 409)
			return
		}
		if _, e := launch(item.Package); e != nil {
			fail(w, e.Error(), 409)
			return
		}
		response(w, map[string]bool{"ok": true})
		return
	}
	if q.Action == "enable" && item.Installed {
		out, e := run(8*time.Second, "/system/bin/pm", "enable", "--user", "0", item.Package)
		if e != nil || !strings.Contains(out, "enabled") {
			fail(w, "Android не подтвердил включение приложения", 409)
			return
		}
		response(w, map[string]bool{"ok": true})
		return
	}
	if q.Action == "set-home" && q.ID == "projectivy" && item.Installed {
		out, e := run(6*time.Second, "/system/bin/cmd", "package", "resolve-activity", "--brief", "-a", "android.intent.action.MAIN", "-c", "android.intent.category.HOME", item.Package)
		activity := ""
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, item.Package+"/") && !strings.ContainsAny(line, " \r\t") {
				activity = line
			}
		}
		if e != nil || activity == "" {
			fail(w, "Установленный лаунчер не объявляет домашний экран", 409)
			return
		}
		out, e = run(6*time.Second, "/system/bin/cmd", "package", "set-home-activity", activity)
		if e != nil || !strings.Contains(out, "Success") {
			fail(w, "Android не подтвердил выбор главного экрана", 409)
			return
		}
		response(w, map[string]bool{"ok": true})
		return
	}
	if _, known := officialAPKs[q.ID]; q.Action != "install" || !known {
		fail(w, "Выберите доступное действие", 400)
		return
	}
	appInstallMu.Lock()
	if currentAppInstall.Active {
		appInstallMu.Unlock()
		fail(w, "Другая установка ещё выполняется", 409)
		return
	}
	currentAppInstall = appInstallJob{Active: true, ID: q.ID, Phase: "Проверка памяти"}
	appInstallMu.Unlock()
	go installOfficialAPK(q.ID)
	response(w, map[string]bool{"ok": true, "started": true})
}
func installOfficialAPK(id string) {
	apk := officialAPKs[id]
	set := func(phase string) { appInstallMu.Lock(); currentAppInstall.Phase = phase; appInstallMu.Unlock() }
	var failure error
	defer func() {
		appInstallMu.Lock()
		currentAppInstall.Active = false
		if failure != nil {
			currentAppInstall.Phase = "Установка не завершена"
			currentAppInstall.Error = failure.Error()
		} else {
			currentAppInstall.Phase = apk.Label + " установлен"
		}
		appInstallMu.Unlock()
	}()
	if officialInstalledMatches(apk) {
		return
	}
	var space syscall.Statfs_t
	// Keep 100 MiB after the compressed APK and two additional copies for
	// extraction/compilation. Download never consumes the final reserve.
	if e := syscall.Statfs(base, &space); e != nil || space.Bfree*uint64(space.Bsize) < uint64((100<<20)+3*apk.Bytes) {
		failure = fmt.Errorf("Недостаточно места для APK и резерва Android. Очистите кэш в разделе обслуживания")
		return
	}
	target := filepath.Join(base, "official-incoming.apk")
	defer os.Remove(target)
	set("Загрузка официального APK")
	client := http.Client{Timeout: 75 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 || req.URL.Scheme != "https" {
			return fmt.Errorf("Недопустимое перенаправление")
		}
		host := req.URL.Hostname()
		if host != "github.com" && !strings.HasSuffix(host, ".githubusercontent.com") {
			return fmt.Errorf("Загрузка должна идти с GitHub")
		}
		return nil
	}}
	r, e := client.Get(apk.URL)
	if e != nil {
		failure = e
		return
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		failure = fmt.Errorf("Источник APK вернул HTTP %d", r.StatusCode)
		return
	}
	f, e := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		failure = e
		return
	}
	hash := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, hash), io.LimitReader(r.Body, apk.Bytes+1))
	syncError := f.Sync()
	f.Close()
	if e != nil || syncError != nil || n != apk.Bytes || hex.EncodeToString(hash.Sum(nil)) != apk.SHA {
		failure = fmt.Errorf("APK не прошёл проверку размера и SHA-256; установка отменена")
		return
	}
	set("Подготовка установщика Android")
	if failure = installVerifiedOfficial(apk, target); failure != nil {
		return
	}
	event("Установлен официальный " + apk.Label)
}

func apkFileMatches(path, expected string) bool {
	f, e := os.Open(path)
	if e != nil {
		return false
	}
	defer f.Close()
	s, e := f.Stat()
	if e != nil || !s.Mode().IsRegular() || s.Size() > 64<<20 {
		return false
	}
	h := sha256.New()
	_, e = io.Copy(h, f)
	return e == nil && hex.EncodeToString(h.Sum(nil)) == expected
}
func officialInstalledMatches(apk officialAPK) bool {
	out, e := run(5*time.Second, "/system/bin/pm", "path", apk.Package)
	if e != nil {
		return false
	}
	path := strings.TrimPrefix(strings.TrimSpace(out), "package:")
	if !strings.HasPrefix(path, "/data/app/"+apk.Package+"-") || filepath.Base(path) != "base.apk" || strings.ContainsAny(path, "\r\n") {
		return false
	}
	return apkFileMatches(path, apk.SHA)
}
func installVerifiedOfficial(apk officialAPK, target string) error {
	out, e := run(20*time.Second, "/system/bin/pm", "install-create", "-r", "--abi", "arm64-v8a", "-S", strconv.FormatInt(apk.Bytes, 10))
	match := regexp.MustCompile(`Success: created install session \[(\d+)\]`).FindStringSubmatch(out)
	if e != nil || len(match) != 2 {
		return fmt.Errorf("Android не выделил память для установки. Очистите кэш или удалите неиспользуемые приложения: %s", strings.TrimSpace(out))
	}
	session := match[1]
	commitAttempted := false
	defer func() {
		if !commitAttempted {
			run(8*time.Second, "/system/bin/pm", "install-abandon", session)
		}
	}()
	out, e = run(45*time.Second, "/system/bin/pm", "install-write", "-S", strconv.FormatInt(apk.Bytes, 10), session, "base.apk", target)
	if e != nil || !strings.Contains(out, "Success") {
		return fmt.Errorf("Android не подтвердил копирование APK: %s", strings.TrimSpace(out))
	}
	// Android 9 Max profile: verify PackageInstaller's private copy before
	// removing our redundant download. This avoids retaining two large APKs.
	if !apkFileMatches("/data/app/vmdl"+session+".tmp/base.apk", apk.SHA) {
		return fmt.Errorf("Копия APK в установщике не прошла SHA-256")
	}
	if e = os.Remove(target); e != nil {
		return e
	}
	commitAttempted = true
	out, e = run(75*time.Second, "/system/bin/pm", "install-commit", session)
	if !officialInstalledMatches(apk) {
		return fmt.Errorf("Установка не подтверждена. Перед повтором обновите список приложений: %s (%v)", strings.TrimSpace(out), e)
	}
	return nil
}

func apkInstallReserve(space syscall.Statfs_t, size uint64) uint64 {
	// Android's default low-space threshold is 5% (capped at 500 MiB).
	// Root-visible free blocks also include ext4's system reserve, unavailable
	// to PackageInstaller. Count both before budgeting temporary APK copies.
	reserve := space.Blocks * uint64(space.Bsize) / 20
	if reserve > 500<<20 {
		reserve = 500 << 20
	}
	if reserve < 100<<20 {
		reserve = 100 << 20
	}
	if space.Bfree > space.Bavail {
		reserve += (space.Bfree - space.Bavail) * uint64(space.Bsize)
	}
	return reserve + size*2 + (16 << 20)
}
