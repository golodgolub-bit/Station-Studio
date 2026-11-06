package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const pluginAssetLimit = 16 << 20

func readPluginAsset(m PluginManifest, relative string) ([]byte, error) {
	return readPluginAssetAt(yamRoot, m, relative)
}
func readPluginAssetAt(root string, m PluginManifest, relative string) ([]byte, error) {
	if relative == "" || len(relative) > 512 || strings.ContainsAny(relative, "\\\x00") || strings.HasPrefix(relative, "/") || path.Clean(relative) != relative || relative == ".." || strings.HasPrefix(relative, "../") {
		return nil, fmt.Errorf("Некорректный путь ресурса")
	}
	panel, e := readPluginPanelAt(root, m)
	if e != nil || panel.UI != "station-ui.html" {
		return nil, fmt.Errorf("У плагина нет интерфейса")
	}
	rel := path.Join(path.Dir(pluginPanelPath(m)), relative)
	expected := m.Files[rel]
	if expected == "" {
		return nil, fmt.Errorf("Ресурс не указан в манифесте")
	}
	file := filepath.Join(root, "pkgs", m.Name, m.Version, filepath.FromSlash(rel))
	resolved, e := filepath.EvalSymlinks(file)
	if e != nil || resolved != file {
		return nil, fmt.Errorf("Ресурс недоступен")
	}
	f, e := os.Open(file)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, pluginAssetLimit+1))
	if e != nil || len(raw) > pluginAssetLimit {
		return nil, fmt.Errorf("Ресурс превышает 16 МиБ")
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != expected {
		return nil, fmt.Errorf("Ресурс изменён после установки")
	}
	return raw, nil
}
func pluginAssetAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		fail(w, "Нужен GET", 405)
		return
	}
	name, relative := r.URL.Query().Get("name"), r.URL.Query().Get("path")
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
	raw, e := readPluginAsset(m, relative)
	if e != nil {
		fail(w, e.Error(), 409)
		return
	}
	typ := mime.TypeByExtension(path.Ext(relative))
	if typ == "" {
		typ = "application/octet-stream"
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Write(raw)
}
