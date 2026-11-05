package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestASRDoesNotDecideMusicIntent(t *testing.T) {
	scenarioMu.Lock()
	old := voiceCfg
	voiceCfg = voiceSettings{MusicRouting: true}
	scenarioMu.Unlock()
	defer func() { scenarioMu.Lock(); voiceCfg = old; scenarioMu.Unlock() }()
	for i, text := range []string{"включи свет в спальне", "включи песню Blank Space Taylor Swift", "Lady Gaga Bad Romance", "включи музыку из фильма"} {
		b, _ := json.Marshal(map[string]any{"requestId": "native-gate-test-" + string(rune('a'+i)), "text": text, "state": "idle"})
		w := httptest.NewRecorder()
		voiceEvent(w, httptest.NewRequest(http.MethodPost, "/event", bytes.NewReader(b)))
		var got map[string]any
		json.Unmarshal(w.Body.Bytes(), &got)
		if got["handled"] != false {
			t.Fatalf("ASR was classified before native decision: %s %s", text, w.Body.String())
		}
	}
}

func TestNativeMusicQueryAllowsNaturalEllipsis(t *testing.T) {
	for text, want := range map[string]string{
		"Включи \"Blank Space\" Taylor Swift":      "Blank Space Taylor Swift",
		"Lady Gaga Bad Romance":                    "Lady Gaga Bad Romance",
		"Алиса, поставь песню Imagine John Lennon": "Imagine John Lennon",
	} {
		got, ok := nativeMusicQuery(text)
		if !ok || got != want {
			t.Fatalf("%q: %q, %v", text, got, ok)
		}
	}
}
