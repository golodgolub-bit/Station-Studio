package main

import (
	"encoding/xml"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var musicQueryMu sync.Mutex

type uiNode struct {
	Text        string   `xml:"text,attr"`
	Package     string   `xml:"package,attr"`
	Description string   `xml:"content-desc,attr"`
	Clickable   string   `xml:"clickable,attr"`
	Bounds      string   `xml:"bounds,attr"`
	Nodes       []uiNode `xml:"node"`
}

var boundsPattern = regexp.MustCompile(`^\[(\d+),(\d+)\]\[(\d+),(\d+)\]$`)

func nodeTexts(n uiNode) string {
	var b strings.Builder
	b.WriteString(n.Text)
	for _, c := range n.Nodes {
		b.WriteByte(' ')
		b.WriteString(nodeTexts(c))
	}
	return b.String()
}
func spotifySearchTarget(data []byte, query string) (int, int, string, error) {
	var tree struct {
		Nodes []uiNode `xml:"node"`
	}
	if len(data) > 1<<20 || xml.Unmarshal(data, &tree) != nil {
		return 0, 0, "", fmt.Errorf("Не удалось прочитать результаты Spotify")
	}
	foundQuery := false
	type card struct {
		x, y int
		name string
	}
	cards := []card{}
	var walk func(uiNode)
	walk = func(n uiNode) {
		text := normalizePhrase(n.Text)
		if n.Package == "com.spotify.tv.android" && strings.Contains(text, normalizePhrase(query)) {
			foundQuery = true
		}
		if n.Package == "com.spotify.tv.android" && n.Clickable == "true" && n.Description != "" {
			all := strings.ToLower(nodeTexts(n))
			track := strings.Contains(all, "трек •") || strings.Contains(all, "song •") || strings.Contains(all, "track •")
			if track {
				m := boundsPattern.FindStringSubmatch(n.Bounds)
				if len(m) == 5 {
					var v [4]int
					for i := 0; i < 4; i++ {
						v[i], _ = strconv.Atoi(m[i+1])
					}
					if v[2] > v[0] && v[3] > v[1] && v[1] > 160 {
						cards = append(cards, card{(v[0] + v[2]) / 2, (v[1] + v[3]) / 2, n.Description})
					}
				}
			}
		}
		for _, c := range n.Nodes {
			walk(c)
		}
	}
	for _, n := range tree.Nodes {
		walk(n)
	}
	if !foundQuery || len(cards) == 0 {
		return 0, 0, "", fmt.Errorf("Spotify ещё не показал результаты этого запроса")
	}
	sort.Slice(cards, func(i, j int) bool {
		if cards[i].y != cards[j].y {
			return cards[i].y < cards[j].y
		}
		return cards[i].x < cards[j].x
	})
	return cards[0].x, cards[0].y, cards[0].name, nil
}
func spotifyPlaySearch(query string, generation int64) error {
	// The TV release implements voice search but leaves the media-session flag unset.
	// Its search page requires selecting a result; verify that exact page before input.
	_, e := helperMediaWithGeneration("search", query, "com.spotify.tv.android", generation)
	if e != nil {
		if generation != musicSearchGeneration.Load() {
			return fmt.Errorf("Поиск отменён новой командой")
		}
		uri := "spotify:search:" + url.PathEscape(query)
		out, err := run(10*time.Second, "/system/bin/am", "start", "-a", "android.intent.action.VIEW", "-d", uri, "-p", "com.spotify.tv.android")
		if err != nil || strings.Contains(out, "Error") {
			return fmt.Errorf("Откройте Spotify и войдите в аккаунт")
		}
	}
	deadline := time.Now().Add(25 * time.Second)
	path := filepath.Join(base, "search-view-"+randomID()+".xml")
	defer os.Remove(path)
	var last error
	for time.Now().Before(deadline) {
		if generation != musicSearchGeneration.Load() {
			return fmt.Errorf("Поиск отменён новой командой")
		}
		dump, _ := run(3*time.Second, "/system/bin/dumpsys", "media_session")
		title, artist, playing := sessionMetadata(dump, "com.spotify.tv.android")
		if playing && spotifyQueryMatches(query, title, artist) {
			event("Spotify: играет «" + title + "»")
			return nil
		}
		if assistantBusy() {
			time.Sleep(250 * time.Millisecond)
			continue
		}
		_, e = run(12*time.Second, "/system/bin/uiautomator", "dump", path)
		if e != nil {
			last = e
			continue
		}
		data, e := os.ReadFile(path)
		if e != nil {
			last = e
			continue
		}
		x, y, name, e := spotifySearchTarget(data, query)
		if e != nil {
			last = e
			time.Sleep(time.Second)
			continue
		}
		top, _ := run(4*time.Second, "/system/bin/dumpsys", "activity", "activities")
		foreground := false
		for _, line := range strings.Split(top, "\n") {
			if strings.Contains(line, "mResumedActivity:") && strings.Contains(line, "com.spotify.tv.android/") {
				foreground = true
			}
		}
		if !foreground {
			return fmt.Errorf("Поиск прерван: на экране открыто другое приложение")
		}
		if generation != musicSearchGeneration.Load() {
			return fmt.Errorf("Поиск отменён новой командой")
		}
		if _, e = run(4*time.Second, "/system/bin/input", "tap", fmt.Sprint(x), fmt.Sprint(y)); e != nil {
			return e
		}
		for n := 0; n < 8; n++ {
			time.Sleep(700 * time.Millisecond)
			dump, _ := run(3*time.Second, "/system/bin/dumpsys", "media_session")
			title, _, playing := sessionMetadata(dump, "com.spotify.tv.android")
			if playing && title != "" && normalizePhrase(title) == normalizePhrase(name) {
				event("Spotify: запущен результат «" + name + "»")
				return nil
			}
		}
		return fmt.Errorf("Spotify открыл результат, но воспроизведение не началось. Проверьте аккаунт и доступ к сервису")
	}
	return fmt.Errorf("Не удалось выбрать песню: %v", last)
}

// Search may start playback directly. Require every requested word to occur in
// the actual track metadata; an unrelated session must never count as success.
func spotifyQueryMatches(query, title, artist string) bool {
	if normalizePhrase(title) == "" {
		return false
	}
	words := strings.Fields(normalizePhrase(query))
	metadata := " " + normalizePhrase(title+" "+artist) + " "
	if len(words) == 0 {
		return false
	}
	for _, word := range words {
		if !strings.Contains(metadata, " "+word+" ") {
			return false
		}
	}
	return true
}
