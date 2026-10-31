package main

import (
	"bytes"
	"encoding/json"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type hdmiBadge struct {
	Enabled bool   `json:"enabled"`
	Media   string `json:"media"`
	Size    int    `json:"size"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Shape   string `json:"shape"`
	Backing bool   `json:"backing"`
}

func badgeAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		response(w, json.RawMessage(validJSONFile(filepath.Join(avatarFolder, "badge.json"))))
		return
	}
	if r.Method != "POST" {
		fail(w, "Нужен POST", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	var q hdmiBadge
	if json.NewDecoder(r.Body).Decode(&q) != nil || q.Size < 32 || q.Size > 160 || q.X < 0 || q.X > 300 || q.Y < 0 || q.Y > 200 || (q.Shape != "circle" && q.Shape != "rounded" && q.Shape != "square") {
		fail(w, "Проверьте размер и положение иконки", 400)
		return
	}
	if q.Enabled && !fileExists(avatarFolder+"/badge.media") {
		fail(w, "Сначала загрузите PNG, JPEG или GIF", 409)
		return
	}
	q.Media = "badge.media"
	b, _ := json.Marshal(q)
	if e := helperWrite("badge.json", b); e != nil {
		fail(w, e.Error(), 500)
		return
	}
	if _, e := run(5*time.Second, "/system/bin/cmd", "appops", "set", "local.station.studio", "SYSTEM_ALERT_WINDOW", "allow"); e != nil {
		fail(w, "Не удалось разрешить маленькую иконку поверх HDMI", 500)
		return
	}
	if e := visualService("badge-preview"); e != nil {
		fail(w, e.Error(), 500)
		return
	}
	response(w, map[string]bool{"ok": true})
}
func badgeMediaAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		b, e := os.ReadFile(avatarFolder + "/badge.media")
		if e != nil {
			fail(w, "Нет изображения", 404)
			return
		}
		w.Header().Set("Content-Type", http.DetectContentType(b))
		w.Write(b)
		return
	}
	if r.Method != "POST" {
		fail(w, "Нужен POST", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	b, e := io.ReadAll(r.Body)
	if e != nil {
		fail(w, "Файл до 8 МБ", 400)
		return
	}
	cfg, kind, e := image.DecodeConfig(bytes.NewReader(b))
	if e != nil || (kind != "png" && kind != "jpeg" && kind != "gif") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 1024 || cfg.Height > 1024 {
		fail(w, "PNG, JPEG или GIF до 1024 × 1024", 400)
		return
	}
	if kind == "gif" {
		if e = checkGIF(b); e != nil {
			fail(w, e.Error(), 400)
			return
		}
	}
	if e = helperWrite("badge.media", b); e != nil {
		fail(w, e.Error(), 500)
		return
	}
	response(w, map[string]any{"ok": true, "width": cfg.Width, "height": cfg.Height})
}
