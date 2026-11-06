package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
)

type pluginRuntimeGrant struct {
	Digest    string `json:"digest,omitempty"`
	Telemetry bool   `json:"telemetry"`
	Media     bool   `json:"media"`
	Display   bool   `json:"display"`
}

func pluginRuntimePath(name string) string {
	return filepath.Join(pluginDir(), "trust", "runtime-"+filepath.Base(installedManifestPath(name)))
}
func pluginRuntimeAllowed(m PluginManifest) pluginRuntimeGrant {
	var g pluginRuntimeGrant
	b, _ := os.ReadFile(pluginRuntimePath(m.Name))
	_ = json.Unmarshal(b, &g)
	if g.Digest != pluginManifestDigest(m) {
		return pluginRuntimeGrant{}
	}
	return g
}
func pluginRuntimeManifest(w http.ResponseWriter, r *http.Request) (PluginManifest, bool) {
	name := r.URL.Query().Get("name")
	if !pluginName.MatchString(name) {
		fail(w, "Некорректный плагин", 400)
		return PluginManifest{}, false
	}
	m, e := loadInstalledManifest(name)
	if e != nil {
		fail(w, "Плагин не установлен", 404)
		return m, false
	}
	return m, true
}
func pluginRuntimePermissionAPI(w http.ResponseWriter, r *http.Request) {
	pluginMu.Lock()
	defer pluginMu.Unlock()
	m, ok := pluginRuntimeManifest(w, r)
	if !ok {
		return
	}
	if r.Method == "GET" {
		g := pluginRuntimeAllowed(m)
		g.Digest = ""
		response(w, g)
		return
	}
	if r.Method != "POST" {
		fail(w, "Нужен GET или POST", 405)
		return
	}
	var g pluginRuntimeGrant
	r.Body = http.MaxBytesReader(w, r.Body, 512)
	if json.NewDecoder(r.Body).Decode(&g) != nil {
		fail(w, "Некорректное разрешение", 400)
		return
	}
	g.Digest = pluginManifestDigest(m)
	raw, _ := json.Marshal(g)
	path := pluginRuntimePath(m.Name)
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		fail(w, "Не удалось сохранить разрешение", 500)
		return
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".runtime-*")
	if e != nil {
		fail(w, "Не удалось сохранить разрешение", 500)
		return
	}
	defer os.Remove(f.Name())
	_, e = f.Write(raw)
	if e == nil {
		e = f.Sync()
	}
	f.Close()
	if e == nil {
		e = os.Rename(f.Name(), path)
	}
	if e != nil {
		fail(w, "Не удалось сохранить разрешение", 500)
		return
	}
	g.Digest = ""
	response(w, g)
}
func pluginRuntimeAPI(w http.ResponseWriter, r *http.Request) {
	pluginMu.Lock()
	m, ok := pluginRuntimeManifest(w, r)
	g := pluginRuntimeAllowed(m)
	pluginMu.Unlock()
	if !ok {
		return
	}
	if r.Method == "GET" {
		if !g.Telemetry {
			fail(w, "Разрешите плагину просмотр состояния станции", 403)
			return
		}
		liveTwinAPI(w, r)
		return
	}
	if r.Method != "POST" {
		fail(w, "Нужен GET или POST", 405)
		return
	}
	var q struct {
		Action     string   `json:"action"`
		Command    string   `json:"command"`
		Frames     [][]byte `json:"frames"`
		Delays     []int    `json:"delays"`
		Duration   int      `json:"duration"`
		Brightness int      `json:"brightness"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 48<<10)
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		fail(w, "Некорректный запрос", 400)
		return
	}
	switch q.Action {
	case "media.control":
		if !g.Media {
			fail(w, "Разрешите плагину управление плеером", 403)
			return
		}
		switch q.Command {
		case "play", "pause", "next", "previous":
		default:
			fail(w, "Доступны play, pause, next, previous", 400)
			return
		}
		result, e := helperMedia(q.Command, "", selectedProvider())
		if e != nil {
			fail(w, e.Error(), 502)
			return
		}
		response(w, result)
	case "display.preview":
		if !g.Display {
			fail(w, "Разрешите плагину предпросмотр LED", 403)
			return
		}
		if e := startDisplayFeedback(q.Frames, q.Delays, q.Duration, q.Brightness); e != nil {
			fail(w, e.Error(), 409)
			return
		}
		response(w, map[string]bool{"ok": true})
	default:
		fail(w, "Действие недоступно", 400)
	}
}
