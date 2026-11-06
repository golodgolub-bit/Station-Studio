package main

import (
	"bytes"
	"context"
	"encoding/json"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"image"
	"image/color"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"
)

type ambientConfig struct {
	Action      string `json:"action"`
	Style       string `json:"style"`
	Duration    int    `json:"duration"`
	Brightness  int    `json:"brightness"`
	Speed       int    `json:"speed"`
	Layout      string `json:"layout"`
	Text        string `json:"text"`
	Until       int64  `json:"until"`
	Font        string `json:"font"`
	Align       string `json:"align"`
	Content     string `json:"content"`
	Spacing     int    `json:"spacing"`
	OffsetY     int    `json:"offsetY"`
	ArtistLevel int    `json:"artistLevel"`
	ClockFace   string `json:"clockFace"`
	Background  []byte `json:"background,omitempty"`
}

var ambientMu sync.Mutex
var ambientCancel context.CancelFunc
var ambientDone chan struct{}
var ambientCurrent ambientConfig
var ambientFace font.Face
var ambientFaceSmall font.Face
var fontMu sync.Mutex
var zone *time.Location

func initAmbient() {
	f, _ := opentype.Parse(goregular.TTF)
	ambientFace, _ = opentype.NewFace(f, &opentype.FaceOptions{Size: 12, DPI: 72, Hinting: font.HintingFull})
	ambientFaceSmall, _ = opentype.NewFace(f, &opentype.FaceOptions{Size: 8, DPI: 72, Hinting: font.HintingFull})
	name, _ := run(4*time.Second, "/system/bin/getprop", "persist.sys.timezone")
	zone, _ = time.LoadLocation(name)
	if zone == nil {
		zone = time.UTC
	}
	go resumeAmbient()
}
func stopAmbientLocked() {
	os.Remove(filepath.Join(base, "ambient.json"))
	ambientMu.Lock()
	c, d := ambientCancel, ambientDone
	ambientMu.Unlock()
	if c != nil {
		c()
		<-d
	}
}
func stopAmbient() { musicControlMu.Lock(); defer musicControlMu.Unlock(); stopAmbientLocked() }
func assistantBusy() bool {
	scenarioMu.Lock()
	state, seen := lastVoiceState, lastVoiceSeen
	overlay := nativeOverlayUntil
	alarm := nativeAlarmActive
	scenarioMu.Unlock()
	return time.Now().UnixMilli() < overlay || time.Since(seen) < 20*time.Second && (alarm || state == "listening" || state == "thinking" || state == "speaking")
}
func correctGray(v byte, gamma, exposure float64) byte {
	if gamma <= 0 {
		gamma = 1
	}
	if exposure <= 0 {
		exposure = 1
	}
	return byte(math.Round(math.Min(255, 255*math.Pow(float64(v)/255, gamma)*exposure)))
}

var digitGlyphs = [10][]string{{"111", "101", "101", "101", "111"}, {"010", "110", "010", "010", "111"}, {"111", "001", "111", "100", "111"}, {"111", "001", "111", "001", "111"}, {"101", "101", "111", "001", "001"}, {"111", "100", "111", "001", "111"}, {"111", "100", "111", "101", "111"}, {"111", "001", "010", "010", "010"}, {"111", "101", "111", "101", "111"}, {"111", "101", "111", "001", "111"}}

func textFrameLine(dst []byte, text string, y int, face font.Face, offset int) {
	if text == "" {
		return
	}
	width := font.MeasureString(face, text).Ceil()
	x := 0
	if width <= 25 {
		x = (25 - width) / 2
	} else {
		x = 25 - offset%(width+40)
	}
	im := image.NewGray(image.Rect(0, 0, 25, 16))
	d := font.Drawer{Dst: im, Src: image.NewUniform(color.White), Face: face, Dot: fixed.P(x, y)}
	d.DrawString(text)
	for i, v := range im.Pix {
		if v > dst[i] {
			dst[i] = v
		}
	}
}
func ambientFrame(q ambientConfig, now time.Time, tick int, title, artist string) []byte {
	f := make([]byte, 400)
	if len(q.Background) == 400 {
		for i, v := range q.Background {
			f[i] = byte(float64(v) * .28)
		}
	}
	put := func(x, y int, v byte) {
		if x >= 0 && x < 25 && y >= 0 && y < 16 {
			f[y*25+x] = v
		}
	}
	if zone != nil {
		now = now.In(zone)
	}
	h, m, s := now.Hour(), now.Minute(), now.Second()
	speed := q.Speed
	if speed < 1 {
		speed = 4
	}
	offset := tick * speed / 10
	if q.Style == "track" || q.Style == "text" {
		fontMu.Lock()
		defer fontMu.Unlock()
		line := func(text string, top int, intensity byte) {
			if q.Font == "smooth" {
				textFrameLine(f, text, top+7, ambientFaceSmall, offset)
			} else {
				pixelText(f, text, top, offset, q.Spacing, q.Align, intensity)
			}
		}
		if q.Style == "text" {
			line(q.Text, 4+q.OffsetY, 255)
			return f
		}
		if title == "" {
			title = "Музыка на паузе"
		}
		if q.Content == "artist" {
			title = artist
			artist = ""
		}
		if q.Content == "title" {
			artist = ""
		}
		artistLevel := q.ArtistLevel
		if artistLevel == 0 {
			artistLevel = 180
		}
		switch q.Layout {
		case "two":
			line(title, q.OffsetY, 255)
			line(artist, 9+q.OffsetY, byte(artistLevel))
		case "alternate":
			text := title
			if tick/50%2 == 1 && artist != "" {
				text = artist
			}
			line(text, 4+q.OffsetY, 255)
		default:
			text := title
			if artist != "" {
				text += " · " + artist
			}
			line(text, 4+q.OffsetY, 255)
		}
		return f
	}
	if q.Style == "analog" {
		for a := 0; a < 12; a++ {
			angle := float64(a)*math.Pi/6 - math.Pi/2
			put(12+int(math.Round(math.Cos(angle)*9)), 8+int(math.Round(math.Sin(angle)*7)), 90)
		}
		hand := func(angle, r float64, v byte) {
			for d := 0.; d <= r; d += .4 {
				put(12+int(math.Round(math.Cos(angle)*d*1.3)), 8+int(math.Round(math.Sin(angle)*d)), v)
			}
		}
		hand((float64(h%12)+float64(m)/60)*math.Pi/6-math.Pi/2, 3.7, 220)
		hand(float64(m)*math.Pi/30-math.Pi/2, 6, 255)
		hand(float64(s)*math.Pi/30-math.Pi/2, 6, 80)
		return f
	}
	if q.Style == "binary" {
		for col, n := range []int{h, m, s} {
			for bit := 0; bit < 6; bit++ {
				v := byte(22)
				if n&(1<<bit) != 0 {
					v = 240
				}
				for dx := 0; dx < 4; dx++ {
					for dy := 0; dy < 2; dy++ {
						put(3+col*7+dx, 13-bit*2+dy, v)
					}
				}
			}
		}
		return f
	}
	if q.Style == "stack" || q.Style == "segments" || q.ClockFace == "stack" || q.ClockFace == "segments" {
		clockDigits(f, h, m, s, q.Style == "stack" || q.ClockFace == "stack", q.OffsetY)
	} else {
		for col, n := range []int{h / 10, h % 10, m / 10, m % 10} {
			x := 2 + col*5
			if col > 1 {
				x++
			}
			for y, line := range digitGlyphs[n] {
				for dx, c := range line {
					if c == '1' {
						put(x+dx, 5+y+q.OffsetY, 245)
					}
				}
			}
		}
		if s%2 == 0 {
			put(12, 6+q.OffsetY, 150)
			put(12, 8+q.OffsetY, 150)
		}
	}
	switch q.Style {
	case "stars":
		for k := 0; k < 12; k++ {
			x := (k*17 + 3) % 25
			y := (k * 11) % 16
			if y >= 4 && y <= 10 {
				continue
			}
			v := byte(30 + 100*(1+math.Sin(float64(tick)/7+float64(k)))/2)
			put(x, y, v)
		}
	case "orbit":
		angle := float64(tick) / 25
		for a := 0.; a < math.Pi*2; a += .1 {
			v := byte(20)
			if math.Abs(math.Atan2(math.Sin(a-angle), math.Cos(a-angle))) < .35 {
				v = 200
			}
			put(12+int(math.Round(math.Cos(a)*12)), 8+int(math.Round(math.Sin(a)*7)), v)
		}
	case "cat":
		for _, cx := range []int{6, 18} {
			put(cx, 1, 160)
			put(cx-1, 2, 160)
			put(cx+1, 2, 160)
		}
		for x := 6; x <= 18; x++ {
			put(x, 13, 100)
		}
		if tick%80 < 73 {
			put(7, 11, 220)
			put(17, 11, 220)
		}
	}
	return f
}
func ambientWorker(ctx context.Context, done chan struct{}, q ambientConfig) {
	defer close(done)
	defer func() {
		os.Remove(filepath.Join(base, "ambient.json"))
		if !musicOwnsMatrix.Load() {
			matrixPost(map[string]string{"action": "stop"})
		}
		ambientMu.Lock()
		ambientCancel = nil
		ambientMu.Unlock()
	}()
	deadline := time.Now().Add(time.Duration(q.Duration) * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	tick := 0
	title, artist := "", ""
	var nextMeta time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if displayFeedbackActive() {
				continue
			}
			if q.Duration != 0 && now.After(deadline) {
				return
			}
			if musicOwnsMatrix.Load() {
				continue
			}
			if assistantBusy() {
				matrixPost(map[string]string{"action": "stop"})
				continue
			}
			if q.Style == "track" && now.After(nextMeta) {
				dump, _ := run(3*time.Second, "/system/bin/dumpsys", "media_session")
				title, artist, _ = sessionMetadata(dump, selectedProvider())
				nextMeta = now.Add(3 * time.Second)
			}
			matrixPost(map[string]any{"action": "play", "frames": [][]byte{ambientFrame(q, now, tick, title, artist)}, "delays": []int{100}, "duration": 5, "brightness": q.Brightness})
			tick++
		}
	}
}
func ambientAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		ambientMu.Lock()
		defer ambientMu.Unlock()
		response(w, map[string]any{"ok": true, "active": ambientCancel != nil, "config": ambientCurrent})
		return
	}
	if r.Method != "POST" {
		fail(w, "Нужен POST", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var q ambientConfig
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		fail(w, "Неверный запрос", 400)
		return
	}
	musicControlMu.Lock()
	defer musicControlMu.Unlock()
	if q.Action == "stop" {
		stopDisplayFeedback()
		stopAmbientLocked()
		response(w, map[string]bool{"ok": true})
		return
	}
	styles := map[string]bool{"digital": true, "stars": true, "orbit": true, "cat": true, "analog": true, "binary": true, "stack": true, "segments": true, "track": true, "text": true}
	if !styles[q.Style] || (q.Duration != 0 && q.Duration < 5) || q.Duration > 86400 || q.Brightness < 0 || q.Brightness > 200 || q.Speed < 1 || q.Speed > 30 || len([]rune(q.Text)) > 300 || q.Spacing < 0 || q.Spacing > 2 || q.OffsetY < -3 || q.OffsetY > 3 || q.ArtistLevel < 0 || q.ArtistLevel > 255 || (len(q.Background) != 0 && len(q.Background) != 400) {
		fail(w, "Проверьте параметры дисплея", 400)
		return
	}
	stopAmbientLocked()
	stopMusicVisualLocked()
	stopDisplayFeedback()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	ambientMu.Lock()
	ambientCancel = cancel
	ambientDone = done
	q.Until = 0
	if q.Duration != 0 {
		q.Until = time.Now().Unix() + int64(q.Duration)
	}
	ambientCurrent = q
	ambientMu.Unlock()
	if b, e := json.Marshal(q); e == nil {
		os.WriteFile(filepath.Join(base, "ambient.json"), b, 0600)
	}
	go ambientWorker(ctx, done, q)
	response(w, map[string]bool{"ok": true})
}
func resumeAmbient() {
	time.Sleep(2 * time.Second)
	var q ambientConfig
	b, e := os.ReadFile(filepath.Join(base, "ambient.json"))
	if e != nil || json.Unmarshal(b, &q) != nil || (q.Duration != 0 && q.Until-time.Now().Unix() < 5) {
		return
	}
	if q.Duration != 0 {
		q.Duration = int(q.Until - time.Now().Unix())
	}
	b, _ = json.Marshal(q)
	out := &captureResponse{header: make(http.Header)}
	request, _ := http.NewRequest("POST", "http://local/api/ambient", bytes.NewReader(b))
	ambientAPI(out, request)
}
func ambientPreviewAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		fail(w, "GET", 405)
		return
	}
	q := ambientConfig{Style: r.URL.Query().Get("style"), Speed: 8}
	if r.Method == "GET" {
		json.Unmarshal([]byte(r.URL.Query().Get("config")), &q)
	}
	if len(q.Background) != 0 && len(q.Background) != 400 || q.Spacing < 0 || q.Spacing > 2 || q.OffsetY < -3 || q.OffsetY > 3 || q.ArtistLevel < 0 || q.ArtistLevel > 255 {
		fail(w, "Проверьте настройки предпросмотра", 400)
		return
	}
	title, artist := r.URL.Query().Get("title"), r.URL.Query().Get("artist")
	if len(title) > 500 || len(artist) > 500 {
		fail(w, "Слишком длинная строка", 400)
		return
	}
	response(w, map[string]any{"frame": ambientFrame(q, time.Now(), int(time.Now().UnixMilli()/100%100000), title, artist), "width": 25, "height": 16})
}
func sessionMetadata(dump, pkg string) (string, string, bool) {
	session := mediaSession(dump, pkg)
	title, artist := "", ""
	if m := regexp.MustCompile(`metadata:size=\d+, description=(.*)`).FindStringSubmatch(session); len(m) > 1 {
		parts := strings.Split(m[1], ", ")
		title = parts[0]
		if title == "null" {
			title = ""
		}
		if len(parts) > 1 {
			artist = parts[1]
		}
	}
	return title, artist, strings.Contains(session, "state=PlaybackState {state=3,")
}
