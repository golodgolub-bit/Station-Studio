package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

func readPluginUI(m PluginManifest) (string, error) {
	p, e := readPluginPanel(m)
	if e != nil {
		return "", e
	}
	rel := filepath.ToSlash(filepath.Join(filepath.Dir(pluginPanelPath(m)), p.UI))
	if p.UI != "station-ui.html" || m.Files[rel] == "" {
		return "", fmt.Errorf("Автор не добавил собственный интерфейс")
	}
	file := filepath.Join(yamRoot, "pkgs", m.Name, m.Version, rel)
	resolved, e := filepath.EvalSymlinks(file)
	if e != nil || resolved != file {
		return "", fmt.Errorf("Файл интерфейса недоступен")
	}
	f, e := os.Open(file)
	if e != nil {
		return "", e
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, (256<<10)+1))
	if e != nil || len(raw) > 256<<10 {
		return "", fmt.Errorf("Интерфейс превышает 256 КБ")
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != m.Files[rel] {
		return "", fmt.Errorf("Интерфейс изменён после установки")
	}
	return string(raw), nil
}
func pluginUIAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		fail(w, "Нужен GET", 405)
		return
	}
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
	html, e := readPluginUI(m)
	if e != nil {
		fail(w, e.Error(), 409)
		return
	}
	response(w, map[string]any{"html": html, "bridge": []string{"settings.read", "settings.write", "home.read", "home.control", "assets.read", "runtime.info", "station.read", "media.control", "display.preview"}, "isolated": true})
}
