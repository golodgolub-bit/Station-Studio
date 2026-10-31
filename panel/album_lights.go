package main

import (
	"time"
)

func ringAlbumWorker() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		mu.Lock()
		wanted := (cfg.Ring == "cover" || cfg.Ring == "music") && cfg.PaletteSource == "cover"
		mu.Unlock()
		if !wanted {
			continue
		}
		cover, _ := refreshCover()
		palette := cover.Palette
		mu.Lock()
		if (cfg.Ring == "cover" || cfg.Ring == "music") && cfg.PaletteSource == "cover" {
			cfg.Palette = palette
		}
		mu.Unlock()
	}
}
