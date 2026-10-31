package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type builtinArtwork struct {
	Name   string   `json:"name"`
	Frames [][]byte `json:"frames"`
	Delays []int    `json:"delays"`
}

var assetsOnce sync.Once
var builtinArt map[string]builtinArtwork
var builtinFlags [][2]string

func loadBuiltinAssets() {
	assetsOnce.Do(func() {
		b, _ := web.ReadFile("web/presets.json")
		json.Unmarshal(b, &builtinArt)
		b, _ = web.ReadFile("web/pride.json")
		json.Unmarshal(b, &builtinFlags)
	})
}
func matrixPresetAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		fail(w, "Нужен POST", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	var q struct {
		ID         string `json:"id"`
		Duration   int    `json:"duration"`
		Brightness int    `json:"brightness"`
	}
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		fail(w, "Выберите анимацию", 400)
		return
	}
	loadBuiltinAssets()
	a, ok := builtinArt[q.ID]
	if !ok {
		fail(w, "Анимация не найдена", 404)
		return
	}
	b, _ := json.Marshal(map[string]any{"action": "play", "frames": a.Frames, "delays": a.Delays, "duration": q.Duration, "brightness": q.Brightness})
	req, _ := http.NewRequest("POST", "http://local/api/matrix", bytes.NewReader(b))
	matrixAPI(w, req)
}
func assetCommands() []commandTemplate {
	loadBuiltinAssets()
	list := []commandTemplate{}
	keys := []string{}
	for id := range builtinArt {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	aliases := map[string][]string{"heart": {"сердечко", "сердце"}, "cat": {"котика", "кота", "пиксельного кота"}, "stars": {"звезды"}, "eyes": {"глазки"}, "rain": {"дождь"}, "fire": {"огонь"}, "jellyfish": {"медузу"}, "butterfly": {"бабочку"}, "comettrail": {"комету"}, "spiral": {"спираль"}, "grid": {"сетку"}}
	for _, id := range keys {
		a := builtinArt[id]
		names := aliases[id]
		if len(names) == 0 {
			names = []string{strings.ToLower(a.Name)}
		}
		phrases := []string{}
		for _, name := range names {
			for _, verb := range []string{"покажи", "отобрази", "включи"} {
				for _, where := range []string{"на дисплее", "на лед дисплее", "на лэд дисплее", "на экране станции"} {
					phrases = append(phrases, verb+" "+name+" "+where)
				}
			}
		}
		list = append(list, commandTemplate{"led-" + id, a.Name, "Анимации", phrases, []scenarioStep{step("matrix/preset", map[string]any{"id": id, "duration": 0, "brightness": 80})}})
	}
	seenFlags := map[string]bool{}
	for i, f := range builtinFlags {
		full := strings.TrimSpace(strings.Split(f[0], "·")[0])
		name := strings.Split(full, ",")[0]
		names := []string{full}
		if !seenFlags[name] && full != name {
			names = append(names, name)
		}
		seenFlags[name] = true
		phrases := []string{}
		for _, label := range names {
			for _, verb := range []string{"включи", "покажи", "отобрази"} {
				phrases = append(phrases, verb+" "+label+" флаг на кольце", verb+" флаг "+label+" на кольце")
			}
		}
		palette := []string{}
		for _, c := range strings.Fields(f[1]) {
			palette = append(palette, "#"+c)
		}
		list = append(list, commandTemplate{fmt.Sprintf("flag-%d", i), f[0], "Флаги", phrases, []scenarioStep{step("action", map[string]any{"action": "ring", "effect": "flag", "palette": palette, "motion": "static", "color": palette[0], "value": 40})}})
	}
	return list
}

// Saved user media get qualified phrases, rather than competing with ordinary music requests.
func personalCommands() []commandTemplate {
	list := []commandTemplate{}
	files, _ := os.ReadDir(filepath.Join(base, "artworks"))
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		b, _ := os.ReadFile(filepath.Join(base, "artworks", f.Name()))
		var a matrixArtwork
		if json.Unmarshal(b, &a) == nil && libraryID.MatchString(a.ID) {
			list = append(list, commandTemplate{"art-" + a.ID, a.Name, "Мои работы", []string{"покажи рисунок " + a.Name + " на станции", "покажи анимацию " + a.Name + " на дисплее"}, []scenarioStep{step("matrix/show", map[string]any{"id": a.ID, "duration": a.Duration})}})
		}
	}
	for _, a := range audioList() {
		list = append(list, commandTemplate{"sound-" + a.ID, a.Name, "Мои звуки", []string{"воспроизведи звук " + a.Name, "включи звук " + a.Name + " из панели"}, []scenarioStep{step("audio/play", map[string]any{"action": "play", "id": a.ID, "volume": 80})}})
	}
	designMu.Lock()
	b, _ := os.ReadFile(filepath.Join(base, "clock-designs.json"))
	designMu.Unlock()
	var designs []clockDesign
	json.Unmarshal(b, &designs)
	for _, d := range designs {
		c := d.Config
		c.Action = "start"
		list = append(list, commandTemplate{"design-" + d.ID, d.Name, "Мои работы", []string{"покажи дизайн часов " + d.Name}, []scenarioStep{step("ambient", c)}})
	}
	return list
}
