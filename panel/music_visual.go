package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type musicTap struct {
	f               *os.File
	address, buffer int64
	capacity, rate  int
	last            uint32
	progress        time.Time
}
type musicVisualConfig struct {
	Action           string   `json:"action"`
	Mode             string   `json:"mode"`
	Target           string   `json:"target"`
	Palette          []string `json:"palette"`
	Gain             float64  `json:"gain"`
	Duration         int      `json:"duration"`
	Brightness       int      `json:"brightness"`
	MatrixBrightness int      `json:"matrixBrightness"`
	Source           string   `json:"source"`
	ColorSource      string   `json:"colorSource"`
	Motion           string   `json:"motion"`
}
type musicLight struct {
	Bands [16]float64
	Wave  [25]float64
	Level float64
	Mode  string
	Gain  float64
}

var musicMu sync.Mutex
var musicControlMu sync.Mutex
var musicCancel context.CancelFunc
var musicDone chan struct{}
var musicLastError string
var musicConfig musicVisualConfig
var musicLevels musicLight

func activeMediaPID(dump string) string {
	if i := strings.Index(dump, "PlaybackActivityMonitor dump time:"); i >= 0 {
		dump = dump[i:]
	}
	if i := strings.Index(dump, "Audio event log:"); i >= 0 {
		dump = dump[:i]
	}
	pid := ""
	for _, line := range strings.Split(dump, "\n") {
		if !strings.Contains(line, "state:started") || !strings.Contains(line, "usage=USAGE_MEDIA ") {
			continue
		}
		m := regexp.MustCompile(`u/pid:(\d+)/(\d+)`).FindStringSubmatch(line)
		if len(m) == 3 {
			pid = m[2]
		}
	}
	return pid
}
func openMusicTap(source string) (*musicTap, error) {
	entries, _ := os.ReadDir("/proc")
	pid := ""
	if source == "system" {
		dump, _ := run(3*time.Second, "/system/bin/dumpsys", "audio")
		pid = activeMediaPID(dump)
	}
	for _, entry := range entries {
		if source == "system" {
			break
		}
		b, _ := os.ReadFile("/proc/" + entry.Name() + "/cmdline")
		if strings.SplitN(string(b), "\x00", 2)[0] == selectedProvider() {
			pid = entry.Name()
			break
		}
	}
	if pid == "" {
		return nil, fmt.Errorf("Включите музыку или видео на станции")
	}
	maps := read("/proc/" + pid + "/maps")
	var start, end uint64
	for _, line := range strings.Split(maps, "\n") {
		if strings.Contains(line, "/dev/ashmem/AudioFlinger::Client("+pid+")") && strings.Contains(line, "rw-s") {
			if start != 0 {
				return nil, fmt.Errorf("Неоднозначный музыкальный буфер")
			}
			p := strings.Split(strings.Fields(line)[0], "-")
			start, _ = strconv.ParseUint(p[0], 16, 64)
			end, _ = strconv.ParseUint(p[1], 16, 64)
		}
	}
	if start == 0 || end-start > 4<<20 {
		return nil, fmt.Errorf("У плеера пока нет поддерживаемого звукового буфера")
	}
	f, e := os.Open("/proc/" + pid + "/mem")
	if e != nil {
		return nil, e
	}
	bad := func() (*musicTap, error) {
		f.Close()
		return nil, fmt.Errorf("Музыка на паузе или звуковой буфер пока не готов")
	}
	a := make([]byte, end-start)
	b := make([]byte, end-start)
	if n, _ := f.ReadAt(a, int64(start)); n != len(a) {
		return bad()
	}
	time.Sleep(160 * time.Millisecond)
	if n, _ := f.ReadAt(b, int64(start)); n != len(b) {
		return bad()
	}
	var tap *musicTap
	for i := 0; i+0xd0 < len(a); i += 16 {
		u := func(data []byte, o int) uint32 { return binary.LittleEndian.Uint32(data[i+o:]) }
		rate := int(u(b, 0x14))
		frames := int(u(b, 0xa8))
		if rate != 44100 && rate != 48000 || frames < 128 || frames > 65536 {
			continue
		}
		capacity := 1
		for capacity < frames {
			capacity *= 2
		}
		if i+0xd0+capacity*4 > len(a) {
			continue
		}
		delta := u(b, 0) - u(a, 0)
		front, rear := u(b, 0xb0), u(b, 0xb4)
		if delta < 1000 || delta > 24000 || rear-front > uint32(capacity) || front-u(b, 0) > 4096 {
			continue
		}
		if tap != nil {
			return bad()
		}
		tap = &musicTap{f: f, address: int64(start) + int64(i), buffer: int64(start) + int64(i) + 0xd0, capacity: capacity, rate: rate, progress: time.Now()}
	}
	if tap == nil {
		return bad()
	}
	return tap, nil
}
func (t *musicTap) sample() (musicLight, error) {
	result := musicLight{}
	header := make([]byte, 0xd0)
	if n, _ := t.f.ReadAt(header, t.address); n != len(header) || int(binary.LittleEndian.Uint32(header[0x14:])) != t.rate {
		return result, fmt.Errorf("Музыкальный буфер изменился")
	}
	position := binary.LittleEndian.Uint32(header[0xb0:])
	if position != t.last {
		t.progress = time.Now()
		t.last = position
	}
	if time.Since(t.progress) > 1200*time.Millisecond {
		return result, fmt.Errorf("Музыка на паузе")
	}
	count := 512
	if t.capacity < count {
		count = t.capacity
	}
	data := make([]byte, count*4)
	offset := int((position - uint32(count)) & uint32(t.capacity-1))
	first := count
	if offset+count > t.capacity {
		first = t.capacity - offset
	}
	if n, _ := t.f.ReadAt(data[:first*4], t.buffer+int64(offset*4)); n != first*4 {
		return result, io.ErrUnexpectedEOF
	}
	if first < count {
		if n, _ := t.f.ReadAt(data[first*4:], t.buffer); n != (count-first)*4 {
			return result, io.ErrUnexpectedEOF
		}
	}
	samples := make([]float64, count)
	sum := 0.0
	for i := 0; i < count; i++ {
		l := float64(int16(binary.LittleEndian.Uint16(data[i*4:]))) / 32768
		r := float64(int16(binary.LittleEndian.Uint16(data[i*4+2:]))) / 32768
		v := (l + r) / 2
		if math.Abs(v) < math.Abs(l)*.1 {
			v = l
		}
		samples[i] = v
		sum += v * v
	}
	result.Level = math.Sqrt(sum / float64(count))
	for x := 0; x < 25; x++ {
		v := 0.0
		for k := x * count / 25; k < (x+1)*count/25; k++ {
			if math.Abs(samples[k]) > math.Abs(v) {
				v = samples[k]
			}
		}
		result.Wave[x] = v
	}
	frequencies := []float64{90, 140, 220, 320, 460, 650, 900, 1250, 1700, 2300, 3100, 4200, 5600, 7400, 9800, 13000}
	for band, freq := range frequencies {
		coefficient := 2 * math.Cos(2*math.Pi*freq/float64(t.rate))
		s1, s2 := 0.0, 0.0
		for i, v := range samples {
			v *= .5 - .5*math.Cos(2*math.Pi*float64(i)/float64(count-1))
			s := v + coefficient*s1 - s2
			s2 = s1
			s1 = s
		}
		result.Bands[band] = math.Sqrt(math.Max(0, s1*s1+s2*s2-coefficient*s1*s2)) / float64(count/4)
	}
	clear(data)
	return result, nil
}
func musicSnapshot() musicLight { musicMu.Lock(); defer musicMu.Unlock(); return musicLevels }
func stopMusicVisual()          { musicControlMu.Lock(); defer musicControlMu.Unlock(); stopMusicVisualLocked() }
func stopMusicVisualLocked() {
	musicMu.Lock()
	cancel, done := musicCancel, musicDone
	musicMu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}
func musicMatrixFrame(s musicLight, mode string) []byte {
	f := make([]byte, 400)
	put := func(x, y int, v byte) {
		if x >= 0 && x < 25 && y >= 0 && y < 16 {
			f[y*25+x] = v
		}
	}
	level := math.Min(1, s.Level*s.Gain*3)
	switch mode {
	case "wave":
		for x, v := range s.Wave {
			y := 8 - int(math.Max(-7, math.Min(7, v*s.Gain*9)))
			put(x, y, 255)
			put(x, y+1, 100)
		}
	case "pulse":
		r := 1 + level*10
		for y := 0; y < 16; y++ {
			for x := 0; x < 25; x++ {
				v := 255 * math.Max(0, 1-math.Abs(math.Hypot(float64(x-12), float64(y)-7.5)-r)/2)
				put(x, y, byte(v))
			}
		}
	case "vu":
		for x := 0; x < int(level*25); x++ {
			for y := 4; y < 12; y++ {
				put(x, y, byte(110+x*5))
			}
		}
	default:
		for x := 0; x < 25; x++ {
			v := s.Bands[x*16/25] * s.Gain
			h := int(math.Max(0, math.Min(16, (20*math.Log10(math.Max(1e-8, v))+65)/4)))
			for y := 0; y < h; y++ {
				put(x, 15-y, byte(100+y*10))
			}
		}
	}
	return f
}
func matrixPost(value any) {
	b, _ := json.Marshal(value)
	client := http.Client{Transport: matrixClient.Transport, Timeout: 2 * time.Second}
	if r, e := client.Post("http://matrix/", "application/json", bytes.NewReader(b)); e == nil {
		io.Copy(io.Discard, io.LimitReader(r.Body, 8192))
		r.Body.Close()
	}
}
func musicVisualWorker(ctx context.Context, done chan struct{}, q musicVisualConfig, previous Config) {
	defer close(done)
	var tap *musicTap
	ownedMatrix := false
	defer func() {
		if tap != nil {
			tap.f.Close()
		}
		if ownedMatrix {
			matrixPost(map[string]string{"action": "stop"})
		}
		mu.Lock()
		if cfg.Ring == "music" {
			cfg.Ring = previous.Ring
			cfg.Palette = previous.Palette
			cfg.FlagMotion = previous.FlagMotion
			cfg.Color = previous.Color
			cfg.Brightness = previous.Brightness
		}
		mu.Unlock()
		save()
		musicMu.Lock()
		musicCancel = nil
		musicLevels = musicLight{}
		musicMu.Unlock()
	}()
	deadline := time.Now().Add(time.Duration(q.Duration) * time.Second)
	retry := time.Time{}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if q.Duration != 0 && now.After(deadline) {
				return
			}
			if tap == nil && !now.Before(retry) {
				var e error
				tap, e = openMusicTap(q.Source)
				musicMu.Lock()
				if e != nil {
					musicLastError = e.Error()
				} else {
					musicLastError = ""
				}
				musicMu.Unlock()
				retry = now.Add(2 * time.Second)
			}
			if tap == nil {
				continue
			}
			s, e := tap.sample()
			if e != nil {
				tap.f.Close()
				tap = nil
				musicMu.Lock()
				musicLastError = e.Error()
				musicLevels = musicLight{}
				musicMu.Unlock()
				continue
			}
			s.Mode = q.Mode
			s.Gain = q.Gain
			musicMu.Lock()
			old := musicLevels
			for i := range s.Bands {
				factor := .65
				if s.Bands[i] < old.Bands[i] {
					factor = .25
				}
				s.Bands[i] = old.Bands[i] + factor*(s.Bands[i]-old.Bands[i])
			}
			musicLevels = s
			musicLastError = ""
			musicMu.Unlock()
			if q.Target != "ring" {
				if assistantBusy() {
					if ownedMatrix {
						matrixPost(map[string]string{"action": "stop"})
						ownedMatrix = false
					}
					continue
				}
				matrixPost(map[string]any{"action": "play", "frames": [][]byte{musicMatrixFrame(s, q.Mode)}, "delays": []int{100}, "duration": 5, "brightness": q.MatrixBrightness})
				ownedMatrix = true
			}
		}
	}
}
func musicVisualAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		musicMu.Lock()
		response(w, map[string]any{"active": musicCancel != nil, "config": musicConfig, "error": musicLastError, "level": musicLevels.Level})
		musicMu.Unlock()
		return
	}
	if r.Method != "POST" {
		fail(w, "Нужен POST", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var q musicVisualConfig
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		fail(w, "Некорректный запрос", 400)
		return
	}
	musicControlMu.Lock()
	defer musicControlMu.Unlock()
	if q.Action == "stop" {
		stopMusicVisualLocked()
		response(w, map[string]bool{"ok": true})
		return
	}
	if q.Action != "start" || (q.Target != "both" && q.Target != "ring" && q.Target != "matrix") || (q.Mode != "spectrum" && q.Mode != "wave" && q.Mode != "pulse" && q.Mode != "vu") || q.Gain < .25 || q.Gain > 20 || (q.Duration != 0 && q.Duration < 5) || q.Duration > 86400 || q.Brightness < 0 || q.Brightness > 100 || q.MatrixBrightness < 0 || q.MatrixBrightness > 200 || len(q.Palette) < 2 || len(q.Palette) > 24 {
		fail(w, "Проверьте настройки визуализации", 400)
		return
	}
	if q.ColorSource != "" && q.ColorSource != "palette" && q.ColorSource != "cover" {
		fail(w, "Выберите источник цветов", 400)
		return
	}
	if q.Motion != "" && q.Motion != "static" && q.Motion != "rotate" && q.Motion != "wave" && q.Motion != "breathe" {
		fail(w, "Выберите анимацию цветов", 400)
		return
	}
	if q.Source != "" && q.Source != "selected" && q.Source != "system" {
		fail(w, "Выберите источник звука", 400)
		return
	}
	for _, c := range q.Palette {
		if len(c) != 7 || c[0] != '#' {
			fail(w, "Неверный цвет", 400)
			return
		}
		if _, e := strconv.ParseUint(c[1:], 16, 24); e != nil {
			fail(w, "Неверный цвет", 400)
			return
		}
	}
	stopAmbientLocked()
	stopMusicVisualLocked()
	mu.Lock()
	previous := cfg
	if previous.Ring == "music" {
		previous.Ring = "off"
	}
	if q.Target != "matrix" {
		cfg.Ring = "music"
		cfg.PaletteSource = q.ColorSource
		cfg.FlagMotion = q.Motion
		cfg.Palette = append([]string(nil), q.Palette...)
		cfg.Brightness = q.Brightness
	}
	mu.Unlock()
	save()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	musicMu.Lock()
	musicCancel = cancel
	musicDone = done
	musicConfig = q
	musicLastError = ""
	musicMu.Unlock()
	go musicVisualWorker(ctx, done, q, previous)
	response(w, map[string]bool{"ok": true})
}
