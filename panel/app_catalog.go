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

func appCatalog() []appCatalogItem {
	list := []appCatalogItem{
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
	if q.Action != "install" || q.ID != "projectivy" {
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
	go installOfficialLauncher()
	response(w, map[string]bool{"ok": true, "started": true})
}
func installOfficialLauncher() {
	set := func(phase string) { appInstallMu.Lock(); currentAppInstall.Phase = phase; appInstallMu.Unlock() }
	var failure error
	defer func() {
		appInstallMu.Lock()
		currentAppInstall.Active = false
		if failure != nil {
			currentAppInstall.Phase = "Установка не завершена"
			currentAppInstall.Error = failure.Error()
		} else {
			currentAppInstall.Phase = "Лаунчер установлен"
		}
		appInstallMu.Unlock()
	}()
	var space syscall.Statfs_t
	if e := syscall.Statfs(base, &space); e != nil || space.Bfree*uint64(space.Bsize) < apkInstallReserve(space, launcherBytes) {
		failure = fmt.Errorf("Недостаточно места с учётом резерва Android. Освободите около 260 МБ перед установкой")
		return
	}
	target := filepath.Join(base, "projectivy-incoming.apk")
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
	r, e := client.Get(launcherURL)
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
	n, e := io.Copy(io.MultiWriter(f, hash), io.LimitReader(r.Body, launcherBytes+1))
	syncError := f.Sync()
	f.Close()
	if e != nil || syncError != nil || n != launcherBytes || hex.EncodeToString(hash.Sum(nil)) != launcherSHA {
		failure = fmt.Errorf("APK не прошёл проверку размера и SHA-256; установка отменена")
		return
	}
	set("Установка на станцию")
	out, e := run(75*time.Second, "/system/bin/pm", "install", "-r", target)
	if e != nil || !strings.Contains(out, "Success") {
		failure = fmt.Errorf("Android не подтвердил установку: %s", strings.TrimSpace(out))
		return
	}
	event("Установлен официальный Projectivy Launcher 4.73")
}

func apkInstallReserve(space syscall.Statfs_t, size uint64) uint64 {
	reserve := space.Blocks * uint64(space.Bsize) / 10
	if reserve > 500<<20 {
		reserve = 500 << 20
	}
	if reserve < 100<<20 {
		reserve = 100 << 20
	}
	return reserve + size*3 + (16 << 20)
}
