package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"
)

func appIconAPI(w http.ResponseWriter, r *http.Request) {
	pkg := r.URL.Query().Get("package")
	if r.Method != "GET" || !allowed.MatchString(pkg) {
		fail(w, "Некорректное приложение", 400)
		return
	}
	b, e := os.ReadFile(avatarFolder + "/icon-" + pkg + ".png")
	if e != nil {
		fail(w, "Изображение пока недоступно", 404)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Write(b)
}

type releaseManifest struct {
	Version int    `json:"versionCode"`
	Name    string `json:"versionName"`
	SHA     string `json:"sha256"`
	Size    int64  `json:"size"`
}

func releaseInfo(kind string) (releaseManifest, error) {
	var m releaseManifest
	dir := filepath.Join(base, "releases")
	b, e := os.ReadFile(filepath.Join(dir, kind+".json"))
	if e != nil {
		return m, e
	}
	if e = json.Unmarshal(b, &m); e != nil {
		return m, e
	}
	data, e := os.ReadFile(filepath.Join(dir, kind+".apk"))
	if e != nil {
		return m, e
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != m.SHA || int64(len(data)) != m.Size {
		return m, fmt.Errorf("Контрольная сумма выпуска не совпала")
	}
	return m, nil
}
func updateAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		fail(w, "GET", 405)
		return
	}
	m, e := releaseInfo("controller")
	if e != nil {
		fail(w, "Новый выпуск пока не размещён на станции", 404)
		return
	}
	response(w, m)
}
func updateAPK(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		fail(w, "GET", 405)
		return
	}
	if _, e := releaseInfo("controller"); e != nil {
		fail(w, "Выпуск недоступен", 404)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	http.ServeFile(w, r, filepath.Join(base, "releases", "controller.apk"))
}

type tvInstallJob struct {
	Active    bool   `json:"active"`
	Phase     string `json:"phase"`
	Error     string `json:"error,omitempty"`
	Version   string `json:"version,omitempty"`
	Installed bool   `json:"installed"`
}

var tvInstallMu sync.Mutex
var currentTVInstall tvInstallJob

func installedTVHashMatches(expected string) bool {
	out, e := run(5*time.Second, "/system/bin/pm", "path", "local.station.studio")
	if e != nil {
		return false
	}
	path := strings.TrimPrefix(strings.TrimSpace(out), "package:")
	if !trustedHelperAPKPath(path) {
		return false
	}
	data, e := os.ReadFile(path)
	if e != nil {
		return false
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]) == expected
}
func tvInstallAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		tvInstallMu.Lock()
		job := currentTVInstall
		tvInstallMu.Unlock()
		response(w, job)
		return
	}
	if r.Method != "POST" {
		fail(w, "Нужен GET или POST", 405)
		return
	}
	m, e := releaseInfo("station-tv")
	if e != nil {
		fail(w, "Проверенный выпуск для станции не размещён", 404)
		return
	}
	tvInstallMu.Lock()
	if currentTVInstall.Active {
		tvInstallMu.Unlock()
		fail(w, "Установка уже выполняется", 409)
		return
	}
	currentTVInstall = tvInstallJob{Active: true, Phase: "Проверка установленной версии", Version: m.Name}
	tvInstallMu.Unlock()
	// The app being updated may own the requesting WebView. Acknowledge before
	// PackageManager stops it; clients reconnect and read the job instead of
	// interpreting a severed connection as a failed or repeatable installation.
	go installTVWorker(m)
	w.WriteHeader(http.StatusAccepted)
	response(w, map[string]any{"ok": true, "started": true, "version": m.Name})
}
func installTVWorker(m releaseManifest) {
	set := func(phase string) { tvInstallMu.Lock(); currentTVInstall.Phase = phase; tvInstallMu.Unlock() }
	var failure error
	defer func() {
		if p := recover(); p != nil {
			os.WriteFile(filepath.Join(base, "tv-install-error.txt"), append([]byte(fmt.Sprintln(p)), debug.Stack()...), 0600)
			failure = fmt.Errorf("Внутренняя ошибка подготовки приложения; проверьте установленную версию перед повтором")
		}
		tvInstallMu.Lock()
		defer tvInstallMu.Unlock()
		currentTVInstall.Active = false
		if failure != nil {
			currentTVInstall.Error = failure.Error()
			currentTVInstall.Phase = "Установка не подтверждена"
		} else {
			currentTVInstall.Phase = "Актуальная версия установлена"
			currentTVInstall.Installed = true
		}
	}()
	if !installedTVHashMatches(m.SHA) {
		var space syscall.Statfs_t
		if e := syscall.Statfs(base, &space); e != nil || space.Bfree*uint64(space.Bsize) < apkInstallReserve(space, uint64(m.Size)) {
			failure = fmt.Errorf("Недостаточно свободной памяти с учётом резерва Android. Освободите около 260 МБ и повторите проверку")
			return
		}
		set("Установка на станцию")
		out, e := run(75*time.Second, "/system/bin/pm", "install", "-r", filepath.Join(base, "releases", "station-tv.apk"))
		if e != nil || !strings.Contains(out, "Success") {
			// A timeout is ambiguous. Verify the installed bytes, never replay pm.
			if !installedTVHashMatches(m.SHA) {
				failure = fmt.Errorf("Android не подтвердил установку: %s", strings.TrimSpace(out))
				return
			}
		}
		if !installedTVHashMatches(m.SHA) {
			failure = fmt.Errorf("Контрольная сумма установленного приложения не совпала")
			return
		}
	}
	set("Подключение локального управления")
	if e := visualService("observe"); e != nil {
		failure = e
		return
	}
	var e error
	for i := 0; i < 15; i++ {
		e = helperWrite("panel.key", []byte(token))
		if e == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if e != nil {
		failure = e
		return
	}
}
