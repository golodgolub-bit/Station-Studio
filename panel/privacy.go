package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var privacyMu sync.Mutex

func privacyHome() string {
	out, _ := run(4*time.Second, "/system/bin/cmd", "package", "resolve-activity", "--brief", "-a", "android.intent.action.MAIN", "-c", "android.intent.category.HOME")
	for _, line := range strings.Split(out, "\n") {
		if i := strings.Index(line, "/"); i > 0 {
			pkg := strings.TrimSpace(line[:i])
			if allowed.MatchString(pkg) {
				return pkg
			}
		}
	}
	return ""
}

var optionalPackages = map[string]string{"com.yandex.launcher.updaterapp": "Обновления оболочки Яндекса", "com.yandex.tv.home": "Главный экран Яндекса", "com.yandex.tv.daydream": "Заставка Яндекса", "com.yandex.tv.live": "Телеканалы Яндекса", "com.yandex.tv.input.efir": "Эфир Яндекса", "com.yandex.tv.ytplayer": "Проигрыватель YouTube Яндекса"}
var privacyProtected = map[string]bool{"com.yandex.io.sdk": true, "com.yandex.tv.alice": true, "com.yandex.tv.services": true, "com.yandex.tv.services.platform": true, "com.yandex.tv.services.privileged": true, "com.yandex.server.SystemExtensionService": true, "com.yandex.tv.settings": true, "com.yandex.tv.keyboard": true, "com.android.vending": true, "com.google.android.gms": true, "com.google.android.gsf": true, "com.spocky.projengmenu": true, "local.station.studio": true}

func packageSet(option string) map[string]bool {
	out, _ := run(5*time.Second, "/system/bin/pm", "list", "packages", option)
	list := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "package:") {
			list[strings.TrimSpace(strings.TrimPrefix(line, "package:"))] = true
		}
	}
	return list
}
func privacyCanDisable(pkg string, third map[string]bool) bool {
	_, optional := optionalPackages[pkg]
	return allowed.MatchString(pkg) && !privacyProtected[pkg] && (optional || third[pkg])
}
func privacyAPI(w http.ResponseWriter, r *http.Request) {
	privacyMu.Lock()
	defer privacyMu.Unlock()
	if r.Method == "GET" {
		installed, disabled, third := packageSet(""), packageSet("-d"), packageSet("-3")
		home := privacyHome()
		apps := []map[string]any{}
		for pkg := range installed {
			name := optionalPackages[pkg]
			if name == "" && !third[pkg] && !privacyProtected[pkg] {
				continue
			}
			if name == "" {
				name = pkg
			}
			apps = append(apps, map[string]any{"package": pkg, "name": name, "disabled": disabled[pkg], "protected": privacyProtected[pkg] || pkg == home, "canDisable": privacyCanDisable(pkg, third) && pkg != home})
		}
		response(w, map[string]any{"apps": apps, "scope": "Разрешения и известные библиотеки проверяются на станции. Это не доказательство фактической отправки данных. Облачная Алиса продолжает обращаться к Яндексу.", "root": os.Geteuid() == 0})
		return
	}
	var q struct {
		Package string `json:"package"`
		Action  string `json:"action"`
	}
	if r.Method != "POST" || json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&q) != nil {
		fail(w, "Неверный запрос", 400)
		return
	}
	if !packageSet("")[q.Package] || !privacyCanDisable(q.Package, packageSet("-3")) {
		fail(w, "Этот компонент нужен станции или не поддерживает отключение из панели", 409)
		return
	}
	if q.Action == "disable" && q.Package == privacyHome() {
		fail(w, "Это текущий главный экран. Сначала выберите другой лаунчер в Android", 409)
		return
	}
	operation := ""
	if q.Action == "disable" {
		operation = "disable-user"
	} else if q.Action == "restore" {
		operation = "enable"
	} else {
		fail(w, "Выберите отключение или восстановление", 400)
		return
	}
	out, e := run(8*time.Second, "/system/bin/pm", operation, "--user", "0", q.Package)
	if e != nil || strings.Contains(out, "Exception") {
		fail(w, "Android не применил изменение", 409)
		return
	}
	disabled := packageSet("-d")[q.Package]
	if disabled != (q.Action == "disable") {
		fail(w, "Состояние приложения не изменилось", 409)
		return
	}
	response(w, map[string]any{"ok": true, "disabled": disabled})
}
func trackerSignatures() map[string][]byte {
	return map[string][]byte{"AppMetrica": []byte("com/yandex/metrica/"), "AppMetrica Analytics": []byte("io/appmetrica/analytics/"), "Firebase Analytics": []byte("com/google/firebase/analytics/"), "Crashlytics · отчёты об ошибках": []byte("com/google/firebase/crashlytics/"), "Facebook App Events": []byte("com/facebook/appevents/"), "Google Ads": []byte("com/google/android/gms/ads/"), "Yandex Ads": []byte("com/yandex/mobile/ads/")}
}
func scanAnalytics(paths []string) ([]string, bool, error) {
	found := map[string]bool{}
	remaining := int64(100 << 20)
	complete := true
	signatures := trackerSignatures()
	for _, path := range paths {
		path = filepath.Clean(path)
		if !strings.HasPrefix(path, "/data/app/") && !strings.HasPrefix(path, "/system/") && !strings.HasPrefix(path, "/vendor/") && !strings.HasPrefix(path, "/odm/") {
			return nil, false, fmt.Errorf("Недопустимый пакет")
		}
		archive, e := zip.OpenReader(filepath.Clean(path))
		if e != nil {
			return nil, false, e
		}
		for _, file := range archive.File {
			if !strings.HasPrefix(filepath.Base(file.Name), "classes") || !strings.HasSuffix(file.Name, ".dex") {
				continue
			}
			if int64(file.UncompressedSize64) > remaining {
				complete = false
				continue
			}
			remaining -= int64(file.UncompressedSize64)
			stream, e := file.Open()
			if e != nil {
				archive.Close()
				return nil, false, e
			}
			reader := io.LimitReader(stream, int64(file.UncompressedSize64))
			buffer := make([]byte, 32768)
			tail := []byte{}
			for {
				n, e := reader.Read(buffer)
				if n > 0 {
					block := append(tail, buffer[:n]...)
					for name, signature := range signatures {
						if bytes.Contains(block, signature) {
							found[name] = true
						}
					}
					if len(block) > 128 {
						tail = append([]byte(nil), block[len(block)-128:]...)
					} else {
						tail = append([]byte(nil), block...)
					}
				}
				if e != nil {
					if e != io.EOF {
						complete = false
					}
					break
				}
			}
			stream.Close()
		}
		archive.Close()
	}
	list := []string{}
	for name := range found {
		list = append(list, name)
	}
	return list, complete, nil
}
func privacyAppAPI(w http.ResponseWriter, r *http.Request) {
	pkg := r.URL.Query().Get("package")
	if r.Method != "GET" || !allowed.MatchString(pkg) || !packageSet("")[pkg] {
		fail(w, "Выберите установленное приложение", 400)
		return
	}
	out, e := run(4*time.Second, "/system/bin/pm", "path", pkg)
	if e != nil {
		fail(w, "Не удалось прочитать пакет", 409)
		return
	}
	paths := []string{}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "package:") {
			paths = append(paths, strings.TrimSpace(strings.TrimPrefix(line, "package:")))
		}
	}
	found, complete, e := scanAnalytics(paths)
	if e != nil {
		fail(w, "Не удалось проверить библиотеки приложения", 409)
		return
	}
	dump, _ := run(4*time.Second, "/system/bin/dumpsys", "package", pkg)
	permissions := []string{}
	for _, permission := range []string{"RECORD_AUDIO", "CAMERA", "ACCESS_FINE_LOCATION", "ACCESS_COARSE_LOCATION", "READ_PHONE_STATE", "PACKAGE_USAGE_STATS", "INTERNET"} {
		if strings.Contains(dump, "android.permission."+permission) {
			permissions = append(permissions, permission)
		}
	}
	response(w, map[string]any{"package": pkg, "libraries": found, "complete": complete, "permissions": permissions, "note": "Наличие библиотеки или разрешения не доказывает сбор данных. Отсутствие сигнатуры не исключает обфусцированную или собственную аналитику. Разрешения здесь перечислены по объявлению пакета, а не по факту использования."})
}
