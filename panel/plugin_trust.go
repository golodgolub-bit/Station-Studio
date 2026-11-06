package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
)

func pluginTrustPath(name string) string {
	return filepath.Join(pluginDir(), "trust", filepath.Base(installedManifestPath(name)))
}
func pluginManifestDigest(m PluginManifest) string {
	b, _ := json.Marshal(m)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func pluginRootAllowed(m PluginManifest) bool {
	raw, _ := os.ReadFile(pluginTrustPath(m.Name))
	return string(raw) == pluginManifestDigest(m)
}
func setPluginTrust(m PluginManifest, allow bool) error {
	p := pluginTrustPath(m.Name)
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		return e
	}
	value := "revoked"
	if allow {
		value = pluginManifestDigest(m)
	}
	f, e := os.CreateTemp(filepath.Dir(p), ".trust-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	_, e = f.WriteString(value)
	if e == nil {
		e = f.Sync()
	}
	f.Close()
	if e != nil {
		return e
	}
	return os.Rename(f.Name(), p)
}
func pluginTrustAPI(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if !pluginName.MatchString(name) {
		fail(w, "Некорректный плагин", 400)
		return
	}
	pluginMu.Lock()
	defer pluginMu.Unlock()
	m, e := loadInstalledManifest(name)
	if e != nil {
		fail(w, "Плагин не установлен", 404)
		return
	}
	if r.Method == "GET" {
		response(w, map[string]any{"rootAllowed": pluginRootAllowed(m), "declared": m.Permissions, "mode": "native-root", "note": "Разрешение действует на эту версию пакета. Отзыв останавливает управляемые службы и их автозапуск. Уже выполненные root-кодом изменения автоматически не отменяются."})
		return
	}
	if r.Method != "POST" {
		fail(w, "Нужен GET или POST", 405)
		return
	}
	var q struct {
		Allow bool `json:"allow"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 512)
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		fail(w, "Некорректный запрос", 400)
		return
	}
	if e = setPluginTrust(m, q.Allow); e != nil {
		fail(w, e.Error(), 500)
		return
	}
	if !q.Allow {
		stopPluginPackage(name)
	}
	response(w, map[string]bool{"ok": true, "rootAllowed": q.Allow})
}
