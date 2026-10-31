package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

var maintenanceMu sync.Mutex
var cachePackages = []string{"com.spotify.tv.android", "com.spocky.projengmenu", "com.android.vending", "com.phlox.tvwebbrowser"}

func folderBytes(path string) int64 {
	var n int64
	filepath.Walk(path, func(p string, info os.FileInfo, e error) error {
		if e != nil {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return filepath.SkipDir
		}
		if info.Mode().IsRegular() {
			n += info.Size()
		}
		return nil
	})
	return n
}
func cleanCacheFolder(path string) error {
	resolved, e := filepath.EvalSymlinks(path)
	if os.IsNotExist(e) {
		return nil
	}
	expected := path
	if strings.HasPrefix(path, "/data/user/0/") {
		parent, err := filepath.EvalSymlinks("/data/user/0")
		if err != nil {
			return err
		}
		expected = filepath.Join(parent, strings.TrimPrefix(path, "/data/user/0/"))
	}
	if e != nil || resolved != expected {
		return fmt.Errorf("Небезопасный путь к кэшу")
	}
	entries, e := os.ReadDir(path)
	if e != nil {
		return e
	}
	for _, entry := range entries {
		target := filepath.Join(path, entry.Name())
		if !strings.HasPrefix(target, path+"/") {
			return fmt.Errorf("Небезопасный путь")
		}
		if e = os.RemoveAll(target); e != nil {
			return e
		}
	}
	return nil
}
func maintenanceAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		items := []map[string]any{}
		for _, pkg := range cachePackages {
			path := "/data/user/0/" + pkg + "/cache"
			items = append(items, map[string]any{"package": pkg, "bytes": folderBytes(path)})
		}
		response(w, map[string]any{"ok": true, "caches": items, "root": os.Geteuid() == 0, "model": read("/system/build.prop") != "", "compatible": buildID() == "5000000921", "osUpdate": "Для установки другой версии Android нужен проверенный образ именно для этой модели и сборки. Установщик прошивки не включён."})
		return
	}
	if r.Method != "POST" {
		fail(w, "Нужен GET или POST", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	var q struct {
		Action   string   `json:"action"`
		Packages []string `json:"packages"`
	}
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		fail(w, "Некорректный запрос", 400)
		return
	}
	maintenanceMu.Lock()
	defer maintenanceMu.Unlock()
	switch q.Action {
	case "idle-apps":
		top, _ := run(5*time.Second, "/system/bin/dumpsys", "activity", "activities")
		media, _ := run(4*time.Second, "/system/bin/dumpsys", "media_session")
		closed := []string{}
		for _, pkg := range []string{"com.phlox.tvwebbrowser", "cm.aptoidetv.pt", "me.zhanghai.android.files", "com.yablio.sendfilestotv"} {
			foreground := false
			for _, line := range strings.Split(top, "\n") {
				if strings.Contains(line, "mResumedActivity:") && strings.Contains(line, pkg+"/") {
					foreground = true
				}
			}
			if foreground || pkg == selectedProvider() || strings.Contains(media, "package="+pkg) {
				continue
			}
			if _, e := run(5*time.Second, "/system/bin/am", "force-stop", pkg); e == nil {
				closed = append(closed, pkg)
			}
		}
		event("Закрыты неиспользуемые приложения")
		response(w, map[string]any{"ok": true, "closed": closed})
		return
	case "cache":
		if len(q.Packages) == 0 || len(q.Packages) > len(cachePackages) {
			fail(w, "Выберите приложения", 400)
			return
		}
		for _, p := range q.Packages {
			ok := false
			for _, a := range cachePackages {
				ok = ok || p == a
			}
			if !ok {
				fail(w, "Приложение не поддерживается", 400)
				return
			}
		}
		var removed int64
		for _, p := range q.Packages {
			path := "/data/user/0/" + p + "/cache"
			before := folderBytes(path)
			if e := cleanCacheFolder(path); e != nil {
				fail(w, e.Error(), 500)
				return
			}
			removed += before - folderBytes(path)
		}
		var st syscall.Statfs_t
		syscall.Statfs("/data", &st)
		event("Очищен кэш выбранных приложений")
		response(w, map[string]any{"ok": true, "removed": removed, "free": st.Bavail * uint64(st.Bsize)})
		return
	case "optimize":
		out, e := run(90*time.Second, "/system/bin/cmd", "package", "compile", "-m", "speed-profile", selectedProvider())
		if e != nil || strings.Contains(out, "Failure") {
			fail(w, out, 500)
			return
		}
		response(w, map[string]any{"ok": true, "output": out})
		return
	default:
		fail(w, "Неизвестное действие", 400)
	}
}
