package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
func tvInstallAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		fail(w, "POST", 405)
		return
	}
	if _, e := releaseInfo("station-tv"); e != nil {
		fail(w, "Проверенный выпуск для станции не размещён", 404)
		return
	}
	out, e := run(75*time.Second, "/system/bin/pm", "install", "-r", filepath.Join(base, "releases", "station-tv.apk"))
	if e != nil || !strings.Contains(out, "Success") {
		fail(w, out, 500)
		return
	}
	visualService("observe")
	for i := 0; i < 15; i++ {
		e = helperWrite("panel.key", []byte(token))
		if e == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if e != nil {
		fail(w, e.Error(), 500)
		return
	}
	response(w, map[string]bool{"ok": true})
}
