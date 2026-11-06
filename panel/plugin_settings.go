package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// A hashed data file, not executable HTML, describes controls on both editions.
// Keeping it in share/ preserves compatibility with the existing YAM manifest.
const pluginPanelFile = "share/station-studio.json"

func pluginPanelPath(m PluginManifest) string {
	scoped := "share/" + strings.ReplaceAll(m.Name, "/", ".") + "/station-studio.json"
	if m.Files[scoped] != "" {
		return scoped
	}
	return pluginPanelFile
}

type pluginField struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Type    string   `json:"type"`
	Hint    string   `json:"hint,omitempty"`
	Default any      `json:"default"`
	Min     float64  `json:"min,omitempty"`
	Max     float64  `json:"max,omitempty"`
	Step    float64  `json:"step,omitempty"`
	Options []string `json:"options,omitempty"`
}
type pluginPanel struct {
	Version int           `json:"version"`
	Title   string        `json:"title"`
	Emoji   string        `json:"emoji,omitempty"`
	UI      string        `json:"ui,omitempty"`
	Fields  []pluginField `json:"fields"`
}

var pluginFieldID = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

func validatePluginValue(f pluginField, value any) error {
	bad := fmt.Errorf("Проверьте поле «%s»", f.Label)
	switch f.Type {
	case "toggle":
		if _, ok := value.(bool); !ok {
			return bad
		}
	case "range":
		n, ok := value.(float64)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < f.Min || n > f.Max {
			return bad
		}
		q := (n - f.Min) / f.Step
		if math.Abs(q-math.Round(q)) > 1e-6 {
			return bad
		}
	case "select":
		s, ok := value.(string)
		if !ok {
			return bad
		}
		for _, option := range f.Options {
			if option == s {
				return nil
			}
		}
		return bad
	case "text":
		s, ok := value.(string)
		if !ok || len(s) > 512 || bytes.IndexByte([]byte(s), 0) >= 0 {
			return bad
		}
	default:
		return bad
	}
	return nil
}
func decodePluginPanel(raw []byte) (pluginPanel, error) {
	var p pluginPanel
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if len(raw) > 32768 || dec.Decode(&p) != nil || p.Version != 1 || p.Title == "" || len(p.Title) > 120 || len(p.Fields) > 24 || len(p.Emoji) > 32 || (p.UI != "" && p.UI != "station-ui.html") {
		return p, fmt.Errorf("Некорректное описание настроек плагина")
	}
	var tail any
	if dec.Decode(&tail) != io.EOF {
		return p, fmt.Errorf("Лишние данные в описании настроек")
	}
	seen := map[string]bool{}
	for _, f := range p.Fields {
		if !pluginFieldID.MatchString(f.ID) || seen[f.ID] || f.Label == "" || len(f.Label) > 120 || len(f.Hint) > 400 {
			return p, fmt.Errorf("Некорректное поле настроек")
		}
		seen[f.ID] = true
		if f.Type == "range" && (f.Max <= f.Min || f.Step <= 0 || f.Max-f.Min > 1000000) {
			return p, fmt.Errorf("Некорректный диапазон")
		}
		if f.Type == "select" && (len(f.Options) == 0 || len(f.Options) > 24) {
			return p, fmt.Errorf("Некорректный список вариантов")
		}
		for _, o := range f.Options {
			if len(o) > 120 {
				return p, fmt.Errorf("Слишком длинный вариант")
			}
		}
		if e := validatePluginValue(f, f.Default); e != nil {
			return p, e
		}
	}
	return p, nil
}
func readPluginPanel(m PluginManifest) (pluginPanel, error) {
	var p pluginPanel
	panelPath := pluginPanelPath(m)
	if !pluginName.MatchString(m.Name) || !pluginVersion.MatchString(m.Version) || m.Files[panelPath] == "" {
		return p, fmt.Errorf("Автор не добавил настройки для панели")
	}
	path := filepath.Join(yamRoot, "pkgs", m.Name, m.Version, panelPath)
	resolved, e := filepath.EvalSymlinks(path)
	if e != nil || resolved != path {
		return p, fmt.Errorf("Описание настроек недоступно")
	}
	f, e := os.Open(path)
	if e != nil {
		return p, e
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, 32769))
	if e != nil {
		return p, e
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != m.Files[panelPath] {
		return p, fmt.Errorf("Описание настроек изменено после установки")
	}
	return decodePluginPanel(raw)
}
func pluginSettingsPath(name string) string {
	return filepath.Join(pluginDir(), "settings", filepath.Base(installedManifestPath(name)))
}
func pluginSettingsValues(name string, p pluginPanel) map[string]any {
	stored := map[string]any{}
	raw, _ := os.ReadFile(pluginSettingsPath(name))
	json.Unmarshal(raw, &stored)
	values := map[string]any{}
	for _, f := range p.Fields {
		v := stored[f.ID]
		if validatePluginValue(f, v) != nil {
			v = f.Default
		}
		values[f.ID] = v
	}
	return values
}
func writePluginSettings(name string, values map[string]any) error {
	path := pluginSettingsPath(name)
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	raw, e := json.Marshal(values)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".settings-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(raw); e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
func pluginSettingsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		fail(w, "Нужен GET или POST", 405)
		return
	}
	name := r.URL.Query().Get("name")
	if !pluginName.MatchString(name) {
		fail(w, "Некорректное имя пакета", 400)
		return
	}
	pluginMu.Lock()
	defer pluginMu.Unlock()
	m, e := loadInstalledManifest(name)
	if e != nil {
		fail(w, "Плагин не установлен", 404)
		return
	}
	p, e := readPluginPanel(m)
	if e != nil {
		fail(w, e.Error(), 409)
		return
	}
	values := pluginSettingsValues(name, p)
	if r.Method == "POST" {
		r.Body = http.MaxBytesReader(w, r.Body, 16384)
		var q struct {
			Values map[string]any `json:"values"`
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if dec.Decode(&q) != nil || len(q.Values) != len(p.Fields) {
			fail(w, "Передайте все поля настроек", 400)
			return
		}
		for _, f := range p.Fields {
			if e := validatePluginValue(f, q.Values[f.ID]); e != nil {
				fail(w, e.Error(), 400)
				return
			}
		}
		values = q.Values
		if e := writePluginSettings(name, values); e != nil {
			fail(w, "Не удалось сохранить настройки", 500)
			return
		}
	}
	response(w, map[string]any{"panel": p, "values": values, "restartRequired": true})
}
