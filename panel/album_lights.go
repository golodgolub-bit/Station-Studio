package main

import (
	"time"
)

func ringAlbumWorker() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		mu.Lock()
		wanted := (cfg.Ring == "cover" || cfg.Ring == "music") && cfg.PaletteSource == "cover"
		mu.Unlock()
		if !wanted {
			continue
		}
		m, e := helperMedia("metadata", "", selectedProvider())
		if e != nil {
			continue
		}
		raw, ok := m["palette"].([]any)
		if !ok || len(raw) < 2 {
			continue
		}
		palette := []string{}
		for _, v := range raw {
			if c, ok := v.(string); ok {
				palette = append(palette, c)
			}
		}
		if len(palette) < 2 {
			continue
		}
		mu.Lock()
		if (cfg.Ring == "cover" || cfg.Ring == "music") && cfg.PaletteSource == "cover" {
			cfg.Palette = palette
		}
		mu.Unlock()
	}
}
