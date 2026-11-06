package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const airplayPackage = "io.github.jqssun.airplay"

func airplaySettingValid(key, value string) bool {
	switch key {
	case "server_name":
		return utf8.ValidString(value) && len(value) > 0 && len(value) <= 100 && !strings.ContainsAny(value, "\x00\r\n")
	case "require_pin", "boot_auto_start", "run_in_background", "advertise_video", "advertise_mirroring", "advertise_audio", "sync_client_volume", "launch_on_connect", "alac_enabled":
		return value == "true" || value == "false"
	case "resolution":
		return value == "auto" || value == "1280x720" || value == "1920x1080"
	case "max_fps":
		return value == "24" || value == "30" || value == "60"
	case "audio_latency_ms":
		n, e := strconv.Atoi(value)
		return e == nil && n >= -1 && n <= 1000
	}
	return false
}
func airplayBroadcast(action string, extra ...string) (string, error) {
	args := []string{"broadcast", "--receiver-foreground", "-n", airplayPackage + "/.SettingsReceiver", "-a", airplayPackage + "." + action}
	args = append(args, extra...)
	out, e := run(8*time.Second, "/system/bin/am", args...)
	if e != nil || !strings.Contains(out, "result=-1") {
		return "", fmt.Errorf("Приёмник не подтвердил изменение. Откройте AirPlay на станции: %s", out)
	}
	return out, nil
}
func airplayAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		path, _ := run(5*time.Second, "/system/bin/pm", "path", airplayPackage)
		if !strings.Contains(path, "package:") {
			response(w, map[string]any{"installed": false})
			return
		}
		out, e := airplayBroadcast("LIST_SETTINGS")
		if e != nil {
			fail(w, e.Error(), 409)
			return
		}
		start := strings.Index(out, "data=\"")
		end := strings.LastIndex(out, "\"")
		values := map[string]any{}
		if start < 0 || end < start+6 || json.Unmarshal([]byte(out[start+6:end]), &values) != nil {
			fail(w, "Приёмник вернул неизвестный формат настроек", 409)
			return
		}
		response(w, map[string]any{"installed": true, "settings": values})
		return
	}
	if r.Method != "POST" {
		fail(w, "Нужен GET или POST", 405)
		return
	}
	var q struct {
		Action string            `json:"action"`
		Values map[string]string `json:"values"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		fail(w, "Некорректный запрос", 400)
		return
	}
	if q.Action == "open" {
		_, e := launch(airplayPackage)
		if e != nil {
			fail(w, e.Error(), 409)
			return
		}
		response(w, map[string]bool{"ok": true})
		return
	}
	if q.Action != "settings" || len(q.Values) == 0 || len(q.Values) > 14 {
		fail(w, "Выберите настройки", 400)
		return
	}
	for k, v := range q.Values {
		if !airplaySettingValid(k, v) {
			fail(w, "Недопустимая настройка AirPlay: "+k, 400)
			return
		}
	}
	// All values are checked before any mutation; arguments never pass through a shell.
	for k, v := range q.Values {
		if _, e := airplayBroadcast("SET_SETTING", "--es", "key", k, "--es", "value", v); e != nil {
			fail(w, e.Error(), 409)
			return
		}
	}
	response(w, map[string]any{"ok": true, "note": "Часть параметров применяется после перезапуска AirPlay через его экран настроек."})
}
