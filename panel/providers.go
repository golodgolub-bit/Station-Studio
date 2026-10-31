package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var nativeMediaMu sync.Mutex
var albumMu sync.Mutex
var albumUpdated time.Time
var musicSearchGeneration atomic.Int64

func cancelMusicSearch() { musicSearchGeneration.Add(1) }

func helperMedia(operation, query, pkg string) (map[string]any, error) {
	return helperMediaWithGeneration(operation, query, pkg, 0)
}
func helperMediaWithGeneration(operation, query, pkg string, generation int64) (map[string]any, error) {
	nativeMediaMu.Lock()
	defer nativeMediaMu.Unlock()
	if generation != 0 && generation != musicSearchGeneration.Load() {
		return nil, fmt.Errorf("Поиск отменён новой командой")
	}
	// Only root can use this abstract Unix socket. Once any request bytes have
	// been sent, a transport failure must never replay a media mutation.
	if c, err := net.DialTimeout("unix", "@stationstudio.media.v1", 250*time.Millisecond); err == nil {
		defer c.Close()
		c.SetDeadline(time.Now().Add(12 * time.Second))
		b, _ := json.Marshal(map[string]string{"operation": operation, "query": query, "package": pkg})
		b = append(b, '\n')
		n, err := c.Write(b)
		if err != nil || n != len(b) {
			return nil, fmt.Errorf("Связь с плеером прервалась; команда автоматически не повторяется")
		}
		line, err := bufio.NewReaderSize(c, 65536).ReadBytes('\n')
		if err != nil {
			return nil, fmt.Errorf("Плеер не подтвердил команду; она автоматически не повторяется")
		}
		var m map[string]any
		if len(line) > 65536 || json.Unmarshal(line, &m) != nil {
			return nil, fmt.Errorf("Неверный ответ плеера")
		}
		if m["ok"] != true {
			return m, fmt.Errorf("%v", m["error"])
		}
		return m, nil
	}
	out, e := run(5*time.Second, "/system/bin/pm", "path", "local.station.studio")
	if e != nil {
		return nil, e
	}
	path := strings.TrimPrefix(strings.TrimSpace(out), "package:")
	if !strings.HasPrefix(path, "/data/app/local.station.studio-") || !strings.HasSuffix(path, "/base.apk") || strings.ContainsAny(path, "\n\r ") {
		return nil, fmt.Errorf("Не установлен помощник панели")
	}
	out, e = run(15*time.Second, "/system/bin/env", "CLASSPATH="+path, "/system/bin/app_process", "/system/bin", "local.station.studio.RootMedia", operation, pkg, query)
	if e != nil {
		return nil, fmt.Errorf("Плеер не ответил: %v", e)
	}
	var m map[string]any
	for _, line := range strings.Split(out, "\n") {
		if json.Unmarshal([]byte(line), &m) == nil && m != nil {
			break
		}
	}
	if m == nil {
		return nil, fmt.Errorf("Неверный ответ плеера")
	}
	if m["ok"] != true {
		return m, fmt.Errorf("%v", m["error"])
	}
	return m, nil
}
func providersAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		fail(w, "Нужен GET", 405)
		return
	}
	known := map[string]string{"com.spotify.tv.android": "Spotify", "com.spotify.music": "Spotify", "com.google.android.apps.youtube.music": "YouTube Music", "com.apple.android.music": "Apple Music", "com.aspiro.tidal": "TIDAL", "ru.yandex.music": "Яндекс Музыка", "com.maxmpz.audioplayer": "Poweramp", "org.videolan.vlc": "VLC"}
	out, _ := run(6*time.Second, "/system/bin/pm", "list", "packages", "-e")
	list := []map[string]string{}
	for p, name := range known {
		if strings.Contains("\n"+out+"\n", "\npackage:"+p+"\n") {
			list = append(list, map[string]string{"package": p, "name": name})
		}
	}
	if extra, e := helperMedia("providers", "", selectedProvider()); e == nil {
		seen := map[string]bool{}
		for _, item := range list {
			seen[item["package"]] = true
		}
		if items, ok := extra["providers"].([]any); ok {
			for _, raw := range items {
				if item, ok := raw.(map[string]any); ok {
					pkg, _ := item["package"].(string)
					name, _ := item["name"].(string)
					if allowed.MatchString(pkg) && name != "" && !seen[pkg] {
						list = append(list, map[string]string{"package": pkg, "name": name})
						seen[pkg] = true
					}
				}
			}
		}
	}
	response(w, map[string]any{"ok": true, "providers": list, "selected": selectedProvider()})
}
func playMusicQuery(query string) error {
	generation := musicSearchGeneration.Add(1)
	return playMusicQueryGeneration(query, generation)
}
func playMusicQueryGeneration(query string, generation int64) error {
	musicQueryMu.Lock()
	defer musicQueryMu.Unlock()
	if generation != musicSearchGeneration.Load() {
		return fmt.Errorf("Поиск отменён новой командой")
	}
	if len(query) < 1 || len(query) > 500 {
		return fmt.Errorf("Введите название песни")
	}
	if selectedProvider() == "com.spotify.tv.android" {
		return spotifyPlaySearch(query, generation)
	}
	_, e := helperMediaWithGeneration("search", query, selectedProvider(), generation)
	return e
}
func musicQuery(text string) (string, bool) {
	s := normalizePhrase(text)
	s = strings.TrimSpace(strings.TrimPrefix(s, "алиса "))
	s = strings.TrimSpace(strings.TrimPrefix(s, "пожалуйста "))
	for _, prefix := range []string{"включи песню ", "включи трек ", "поставь песню ", "поставь трек ", "найди песню ", "включи музыку ", "включи композицию ", "поставь композицию ", "найди трек ", "сыграй песню ", "включи пожалуйста песню ", "поставь пожалуйста песню "} {
		if strings.HasPrefix(s, prefix) {
			q := strings.TrimSpace(strings.TrimPrefix(s, prefix))
			for _, suffix := range []string{"на кольце", "на дисплее", "на лед дисплее", "на экране станции", "на станции"} {
				if strings.HasSuffix(q, suffix) {
					return "", false
				}
			}
			return q, q != ""
		}
	}
	// An explicit service name makes bare 'включи' unambiguous; household
	// requests such as 'включи свет' remain with Alice.
	for _, prefix := range []string{"включи ", "поставь "} {
		for _, suffix := range []string{" в спотифай", " в спотифае", " на спотифай", " в spotify"} {
			if strings.HasPrefix(s, prefix) && strings.HasSuffix(s, suffix) {
				q := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, prefix), suffix))
				return q, q != ""
			}
		}
		if strings.HasPrefix(s, prefix) {
			q := strings.TrimSpace(strings.TrimPrefix(s, prefix))
			// These are device/household intents, not music titles.
			for _, reserved := range []string{"сказ", "шум", "белый шум", "аудиокниг", "подкаст", "ночн", "флаг", "кольц", "сердеч", "сердц", "дизайн", "рисун", "эффект", "свет", "ламп", "подсвет", "розет", "телевизор", "тв", "таймер", "будильник", "напоминан", "кондиционер", "вентилятор", "пылесос", "обогрев", "отоплен", "увлажнител", "ночник", "штор", "жалюзи", "люстр", "бра", "радио", "новост", "погод", "ютуб", "youtube", "spotify", "спотифай", "bluetooth", "блютуз", "режим", "звук", "микрофон", "станци", "дисплей", "экран", "час", "анимаци", "радуг", "сценарий", "игр", "видео", "фильм", "сериал", "мульт", "камер", "чайник", "кофе"} {
				if strings.HasPrefix(q, reserved) {
					return "", false
				}
			}
			return q, q != ""
		}
	}
	return "", false
}
func musicPlayAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		fail(w, "Нужен POST", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var q struct {
		Action   string `json:"action"`
		Query    string `json:"query"`
		Position int64  `json:"position"`
		Delta    int64  `json:"delta"`
		Value    int    `json:"value"`
	}
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		fail(w, "Неверный запрос", 400)
		return
	}
	switch q.Action {
	case "seek", "seek-by", "shuffle", "repeat":
		var value int64
		switch q.Action {
		case "seek":
			value = q.Position
			if value < 0 || value > 86400000 {
				fail(w, "Позиция вне диапазона", 400)
				return
			}
		case "seek-by":
			value = q.Delta
			if value == 0 || value < -3600000 || value > 3600000 {
				fail(w, "Перемотка до одного часа", 400)
				return
			}
		default:
			value = int64(q.Value)
			if value < 0 || value > 2 {
				fail(w, "Неизвестный режим плеера", 400)
				return
			}
		}
		if _, e := helperMedia(q.Action, fmt.Sprint(value), selectedProvider()); e != nil {
			fail(w, e.Error(), 409)
			return
		}
	case "search":
		if e := playMusicQuery(q.Query); e != nil {
			fail(w, e.Error(), 409)
			return
		}
	case "play", "pause", "next", "previous":
		generation := musicSearchGeneration.Add(1)
		pkg := selectedProvider()
		dump, _ := run(3*time.Second, "/system/bin/dumpsys", "media_session")
		title, artist, _ := sessionMetadata(dump, pkg)
		_, e := helperMediaWithGeneration(q.Action, "", pkg, generation)
		if q.Action == "play" {
			playing := false
			for i := 0; i < 4; i++ {
				if generation != musicSearchGeneration.Load() {
					fail(w, "Продолжение отменено новой командой", 409)
					return
				}
				time.Sleep(250 * time.Millisecond)
				current, _ := run(3*time.Second, "/system/bin/dumpsys", "media_session")
				_, _, playing = sessionMetadata(current, pkg)
				if playing {
					break
				}
			}
			if !playing && pkg == "com.spotify.tv.android" && title != "" {
				// An inactive Connect session ignores transport controls. Re-select
				// the known song on this TV, then resume its saved position.
				if err := playMusicQueryGeneration(title+" "+artist, generation); err != nil {
					fail(w, err.Error(), 409)
					return
				}
				if m := regexp.MustCompile(`position=(\d+)`).FindStringSubmatch(mediaSession(dump, pkg)); len(m) > 1 {
					position, _ := strconv.ParseInt(m[1], 10, 64)
					if position > 1000 {
						helperMediaWithGeneration("seek", fmt.Sprint(position), pkg, generation)
					}
				}
				playing = true
				e = nil
			}
			if !playing {
				fail(w, "Плеер не начал воспроизведение. Откройте его на станции и проверьте аккаунт", 409)
				return
			}
		}
		if e != nil {
			fail(w, e.Error(), 409)
			return
		}
	default:
		fail(w, "Неизвестная команда", 400)
		return
	}
	response(w, map[string]bool{"ok": true})
}
func albumAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		fail(w, "Нужен GET", 405)
		return
	}
	albumMu.Lock()
	defer albumMu.Unlock()
	if time.Since(albumUpdated) > 3*time.Second {
		refreshCover()
		albumUpdated = time.Now()
	}
	if !coverSnapshot().Available {
		fail(w, "Плеер пока не передал обложку этого трека", 404)
		return
	}
	b, e := os.ReadFile(filepath.Join(base, "album.jpg"))
	if e != nil {
		fail(w, "Обложка пока недоступна", 404)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Write(b)
}
