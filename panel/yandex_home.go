package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Official IoT API plus an explicitly approved, isolated web session.
// No extraction of credentials from installed Yandex apps.
var homeHTTP = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
	return fmt.Errorf("Перенаправление API запрещено")
}}
var homeOrigin = "https://api.iot.yandex.net"
var homeMu sync.Mutex
var homeID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

type homeCapability struct {
	Type       string         `json:"type"`
	Parameters map[string]any `json:"parameters"`
	State      map[string]any `json:"state"`
}
type homeDevice struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Type         string            `json:"type"`
	Room         string            `json:"room"`
	State        string            `json:"state"`
	Capabilities []homeCapability  `json:"capabilities"`
	Properties   []json.RawMessage `json:"properties"`
}
type homeInfo struct {
	Status     string            `json:"status"`
	RequestID  string            `json:"request_id"`
	Rooms      []json.RawMessage `json:"rooms"`
	Devices    []homeDevice      `json:"devices"`
	Scenarios  []json.RawMessage `json:"scenarios"`
	Households []json.RawMessage `json:"households"`
	Groups     []json.RawMessage `json:"groups"`
}
type homeAction struct {
	ID      string `json:"id"`
	Actions []struct {
		Type  string         `json:"type"`
		State map[string]any `json:"state"`
	} `json:"actions"`
}

func clearPluginHomeGrants() {
	pluginMu.Lock()
	defer pluginMu.Unlock()
	directory := filepath.Join(pluginDir(), "trust")
	entries, _ := os.ReadDir(directory)
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "home-") {
			_ = os.Remove(filepath.Join(directory, entry.Name()))
		}
	}
}
func homeToken() string { b, _ := os.ReadFile(filepath.Join(base, "home-oauth")); return string(b) }
func homeCall(key, method, path string, body any, out any) error {
	if key == "" && homeWebConnected() {
		return homeWebCall(method, path, body, out)
	}
	if key == "" {
		return fmt.Errorf("Сначала подключите Дом с Алисой")
	}
	var data []byte
	var e error
	if body != nil {
		data, e = json.Marshal(body)
		if e != nil {
			return e
		}
	}
	req, e := http.NewRequest(method, homeOrigin+path, bytes.NewReader(data))
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "AliceKit/2.5")
	res, e := homeHTTP.Do(req)
	if e != nil {
		return fmt.Errorf("Нет связи с API умного дома")
	}
	defer res.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(res.Body, (4<<20)+1))
	if e != nil || len(raw) > 4<<20 {
		return fmt.Errorf("Неполный ответ умного дома")
	}
	if res.StatusCode == 401 || res.StatusCode == 403 {
		return fmt.Errorf("Яндекс отклонил доступ. Проверьте срок токена и права iot:view, iot:control")
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("Умный дом ответил HTTP %d. Команда повторно не отправлялась", res.StatusCode)
	}
	var status struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(raw, &status) != nil || status.Status != "ok" {
		return fmt.Errorf("Яндекс не подтвердил запрос")
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}
func homeSave(name string, data []byte) error {
	f, e := os.CreateTemp(base, ".home-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(data)
	}
	if e == nil {
		e = f.Sync()
	}
	f.Close()
	if e != nil {
		return e
	}
	return os.Rename(f.Name(), filepath.Join(base, name))
}
func homeAccountAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	homeMu.Lock()
	defer homeMu.Unlock()
	if r.Method == "GET" {
		mode := "oauth"
		if homeWebConnected() {
			mode = "web_session"
		}
		response(w, map[string]any{"connected": homeConnected(), "provider": "yandex", "mode": mode, "scopes": func() []string {
			if mode == "web_session" {
				return []string{"yandex_web_session"}
			}
			return []string{"iot:view", "iot:control"}
		}()})
		return
	}
	if r.Method == "DELETE" {
		homeWebMu.Lock()
		homeLogin = nil
		webErr := os.Remove(filepath.Join(base, "home-web-session"))
		homeWebMu.Unlock()
		e := os.Remove(filepath.Join(base, "home-oauth"))
		clearPluginHomeGrants()
		if (e != nil && !os.IsNotExist(e)) || (webErr != nil && !os.IsNotExist(webErr)) {
			fail(w, "Не удалось удалить подключение", 500)
			return
		}
		response(w, map[string]bool{"ok": true})
		return
	}
	if r.Method != "POST" {
		fail(w, "Нужен GET, POST или DELETE", 405)
		return
	}
	if r.TLS == nil {
		fail(w, "Передача токена разрешена только через защищённое подключение приложения", 400)
		return
	}
	var q struct {
		Token string `json:"token"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if json.NewDecoder(r.Body).Decode(&q) != nil || len(q.Token) < 20 || len(q.Token) > 2048 || strings.ContainsAny(q.Token, "\r\n \t") {
		fail(w, "Проверьте OAuth-токен", 400)
		return
	}
	var info homeInfo
	if e := homeCall(q.Token, "GET", "/v1.0/user/info", nil, &info); e != nil {
		fail(w, e.Error(), 400)
		return
	}
	if homeToken() != q.Token {
		clearPluginHomeGrants()
	}
	if e := homeSave("home-oauth", []byte(q.Token)); e != nil {
		fail(w, "Не удалось сохранить подключение", 500)
		return
	}
	homeWebMu.Lock()
	homeLogin = nil
	_ = os.Remove(filepath.Join(base, "home-web-session"))
	homeWebMu.Unlock()
	response(w, map[string]any{"connected": true, "devices": len(info.Devices)})
}
func homeAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" {
		fail(w, "Нужен GET", 405)
		return
	}
	if !homeConnected() {
		response(w, map[string]any{"connected": false, "devices": []any{}, "rooms": []any{}})
		return
	}
	if id := r.URL.Query().Get("id"); id != "" {
		if !homeID.MatchString(id) {
			fail(w, "Некорректное устройство", 400)
			return
		}
		var d map[string]any
		if e := homeCall(homeToken(), "GET", "/v1.0/devices/"+url.PathEscape(id), nil, &d); e != nil {
			fail(w, e.Error(), 502)
			return
		}
		response(w, d)
		return
	}
	var info homeInfo
	if e := homeCall(homeToken(), "GET", "/v1.0/user/info", nil, &info); e != nil {
		fail(w, e.Error(), 502)
		return
	}
	response(w, map[string]any{"connected": true, "devices": info.Devices, "rooms": info.Rooms, "groups": info.Groups, "scenarios": info.Scenarios, "households": info.Households})
}
func homeExecute(q homeAction) (json.RawMessage, error) {
	if !homeID.MatchString(q.ID) || len(q.Actions) == 0 || len(q.Actions) > 10 {
		return nil, fmt.Errorf("Выберите устройство и от 1 до 10 действий")
	}
	var device homeDevice
	key := homeToken()
	if e := homeCall(key, "GET", "/v1.0/devices/"+url.PathEscape(q.ID), nil, &device); e != nil {
		return nil, e
	}
	if device.ID != q.ID {
		return nil, fmt.Errorf("Устройство не найдено")
	}
	for _, a := range q.Actions {
		instance, _ := a.State["instance"].(string)
		if instance == "" || a.State["value"] == nil || len(a.State) > 3 {
			return nil, fmt.Errorf("Некорректное состояние устройства")
		}
		supported := false
		for _, c := range device.Capabilities {
			ci, _ := c.Parameters["instance"].(string)
			if ci == "" {
				ci, _ = c.State["instance"].(string)
			}
			if c.Type == a.Type && (ci == instance || (ci == "" && c.Type == "devices.capabilities.on_off" && instance == "on") || (c.Type == "devices.capabilities.color_setting" && (instance == "rgb" || instance == "hsv" || instance == "temperature_k" || instance == "scene"))) {
				supported = true
				break
			}
		}
		if !supported {
			return nil, fmt.Errorf("Устройство не объявило это действие: %s / %s", a.Type, instance)
		}
	}
	var result json.RawMessage
	if key == "" && homeWebConnected() && homeIsWebColor(q) {
		return homeWebColor(q)
	}
	e := homeCall(key, "POST", "/v1.0/devices/actions", map[string]any{"devices": []homeAction{q}}, &result)
	if e != nil {
		return nil, e
	}
	// A successful HTTP response can still contain per-capability failures.
	var reply struct {
		Devices []struct {
			Capabilities []struct {
				State struct {
					Result struct {
						Status string `json:"status"`
						Code   string `json:"error_code"`
					} `json:"action_result"`
				} `json:"state"`
			} `json:"capabilities"`
		} `json:"devices"`
	}
	if json.Unmarshal(result, &reply) != nil || len(reply.Devices) == 0 {
		return nil, fmt.Errorf("Нет подтверждения устройства. Не повторяем команду автоматически")
	}
	for _, d := range reply.Devices {
		if len(d.Capabilities) == 0 {
			return nil, fmt.Errorf("Нет подтверждения действия")
		}
		for _, c := range d.Capabilities {
			if c.State.Result.Status != "DONE" {
				return nil, fmt.Errorf("Устройство не выполнило команду: %s", c.State.Result.Code)
			}
		}
	}
	return result, nil
}
func homeActionAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		fail(w, "Нужен POST", 405)
		return
	}
	var q homeAction
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		fail(w, "Некорректное действие", 400)
		return
	}
	j, e := homeExecute(q)
	if e != nil {
		fail(w, e.Error(), 502)
		return
	}
	response(w, j)
}

type pluginHomeGrant struct {
	Digest  string `json:"digest"`
	Read    bool   `json:"read"`
	Control bool   `json:"control"`
}

func pluginHomePath(name string) string {
	return filepath.Join(pluginDir(), "trust", "home-"+filepath.Base(installedManifestPath(name)))
}
func pluginHomeAllowed(m PluginManifest) pluginHomeGrant {
	var q pluginHomeGrant
	b, _ := os.ReadFile(pluginHomePath(m.Name))
	_ = json.Unmarshal(b, &q)
	if q.Digest != pluginManifestDigest(m) {
		return pluginHomeGrant{}
	}
	return q
}
func pluginHomePermissionAPI(w http.ResponseWriter, r *http.Request) {
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
		q := pluginHomeAllowed(m)
		response(w, map[string]bool{"read": q.Read, "control": q.Control})
		return
	}
	if r.Method != "POST" {
		fail(w, "Нужен GET или POST", 405)
		return
	}
	var q pluginHomeGrant
	r.Body = http.MaxBytesReader(w, r.Body, 512)
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		fail(w, "Некорректное разрешение", 400)
		return
	}
	q.Digest = pluginManifestDigest(m)
	if q.Control {
		q.Read = true
	}
	raw, _ := json.Marshal(q)
	p := pluginHomePath(name)
	if e = os.MkdirAll(filepath.Dir(p), 0700); e == nil {
		var f *os.File
		f, e = os.CreateTemp(filepath.Dir(p), ".home-grant-*")
		if e == nil {
			defer os.Remove(f.Name())
			_, e = f.Write(raw)
			if e == nil {
				e = f.Sync()
			}
			f.Close()
			if e == nil {
				e = os.Rename(f.Name(), p)
			}
		}
	}
	if e != nil {
		fail(w, "Не удалось сохранить разрешение", 500)
		return
	}
	response(w, map[string]bool{"ok": true, "read": q.Read, "control": q.Control})
}
func pluginHomeAPI(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if !pluginName.MatchString(name) {
		fail(w, "Некорректный плагин", 400)
		return
	}
	pluginMu.Lock()
	m, e := loadInstalledManifest(name)
	grant := pluginHomeAllowed(m)
	pluginMu.Unlock()
	if e != nil {
		fail(w, "Плагин не установлен", 404)
		return
	}
	if r.Method == "GET" {
		if !grant.Read {
			fail(w, "Разрешите плагину просмотр умного дома в его параметрах", 403)
			return
		}
		homeAPI(w, r)
		return
	}
	if r.Method == "POST" {
		if !grant.Control {
			fail(w, "Разрешите плагину управление умным домом в его параметрах", 403)
			return
		}
		homeActionAPI(w, r)
		return
	}
	fail(w, "Нужен GET или POST", 405)
}
