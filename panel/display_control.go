package main

import (
	"context"
	"encoding/json"
	"net/http"
)

// One explicit place to release a persistent artwork. Ring playback is independent.
func displayControlAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		ambientMu.Lock()
		active, config := ambientCancel != nil, ambientCurrent
		ambientMu.Unlock()
		musicMu.Lock()
		visual, target := musicCancel != nil, musicConfig.Target
		musicMu.Unlock()
		source := "native"
		if active {
			source = config.Style
		}
		if visual && target != "ring" {
			source = "visual"
		}
		if displayFeedbackActive() {
			source = "preview"
		}
		response(w, map[string]any{"ok": true, "source": source, "ambient": active, "config": config, "visual": visual, "visualTarget": target, "assistantPriority": true})
		return
	}
	if r.Method != "POST" {
		fail(w, "Нужен GET или POST", 405)
		return
	}
	var q struct {
		Action string `json:"action"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if json.NewDecoder(r.Body).Decode(&q) != nil || (q.Action != "native" && q.Action != "blank" && q.Action != "stop-track") {
		fail(w, "Выберите штатный дисплей, пустой экран или отключение трека", 400)
		return
	}
	musicControlMu.Lock()
	defer musicControlMu.Unlock()
	if q.Action == "stop-track" {
		ambientMu.Lock()
		track := ambientCancel != nil && ambientCurrent.Style == "track"
		ambientMu.Unlock()
		if track {
			stopAmbientLocked()
		}
		response(w, map[string]bool{"ok": true})
		return
	}
	stopDisplayFeedback()
	// Give the display back without turning off an independently selected ring show.
	musicMu.Lock()
	visual, target := musicCancel != nil, musicConfig.Target
	musicMu.Unlock()
	if visual && target != "ring" {
		stopMusicVisualLocked()
	}
	stopAmbientLocked()
	if q.Action == "blank" {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		config := ambientConfig{Style: "blank", Brightness: 85, Speed: 6, Duration: 0}
		ambientMu.Lock()
		ambientCancel = cancel
		ambientDone = done
		ambientCurrent = config
		ambientMu.Unlock()
		if e := saveAmbient(config); e != nil {
			cancel()
			ambientMu.Lock()
			ambientCancel = nil
			ambientMu.Unlock()
			close(done)
			fail(w, "Не удалось сохранить настройку дисплея", 500)
			return
		}
		go ambientWorker(ctx, done, config)
	} else if e := matrixPost(map[string]string{"action": "stop"}); e != nil {
		fail(w, "Не удалось вернуть штатный дисплей: "+e.Error(), 503)
		return
	}
	response(w, map[string]bool{"ok": true})
}
