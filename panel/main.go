package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed web/*
var web embed.FS
var base string
var token string
var started = time.Now()
var mu sync.Mutex
var commandMu sync.Mutex
var cfg = Config{Mode: "balanced", Ring: "off", Color: "#9364ff", Brightness: 35}
var events []Event
var allowed = regexp.MustCompile(`^[a-zA-Z0-9_]+(?:\.[a-zA-Z0-9_]+)+$`)

type Config struct {
	Mode          string   `json:"mode"`
	Ring          string   `json:"ring"`
	Color         string   `json:"color"`
	Brightness    int      `json:"brightness"`
	Palette       []string `json:"palette"`
	FlagMotion    string   `json:"flagMotion"`
	PaletteSource string   `json:"paletteSource"`
	SleepAt       int64    `json:"sleepAt"`
}
type Event struct {
	Time int64  `json:"time"`
	Text string `json:"text"`
}
type Request struct {
	Palette []string `json:"palette"`
	Motion  string   `json:"motion"`
	Action  string   `json:"action"`
	Value   int      `json:"value"`
	Text    string   `json:"text"`
	Package string   `json:"package"`
	Color   string   `json:"color"`
	Effect  string   `json:"effect"`
	Source  string   `json:"source"`
}

func event(s string) {
	mu.Lock()
	defer mu.Unlock()
	events = append(events, Event{time.Now().Unix(), s})
	if len(events) > 50 {
		events = events[len(events)-50:]
	}
}
func save() {
	mu.Lock()
	defer mu.Unlock()
	persist := cfg
	if persist.Ring == "music" {
		persist.Ring = "off"
	}
	b, _ := json.Marshal(persist)
	os.WriteFile(filepath.Join(base, "settings.json.tmp"), b, 0600)
	os.Rename(filepath.Join(base, "settings.json.tmp"), filepath.Join(base, "settings.json"))
}
func read(p string) string { b, _ := os.ReadFile(p); return strings.TrimSpace(string(b)) }
func run(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	commandArgs := append([]string{"-c", "exec \"$@\"", "station-panel", name}, args...)
	cmd := exec.CommandContext(ctx, "/system/bin/sh", commandArgs...)
	cmd.Env = append(os.Environ(), "PATH=/system/bin:/system/xbin:/vendor/bin")
	var b commandOutput
	cmd.Stdout = &b
	cmd.Stderr = &b
	e := cmd.Run()
	if ctx.Err() != nil {
		return b.String(), ctx.Err()
	}
	s := b.String()
	if len(s) > 64000 {
		s = s[:64000]
	}
	return strings.TrimSpace(s), e
}

type commandOutput struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (o *commandOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	n := len(p)
	remaining := 64000 - o.b.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		o.b.Write(p)
	}
	return n, nil
}
func (o *commandOutput) String() string { o.mu.Lock(); defer o.mu.Unlock(); return o.b.String() }
func shell(s string) (string, error)    { return run(25*time.Second, "/system/bin/sh", "-c", s) }
func response(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, s string, code int) {
	w.WriteHeader(code)
	response(w, map[string]any{"ok": false, "error": s})
}
func auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		a := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !accessValid(a) {
			fail(w, "Нужен ключ доступа", 401)
			return
		}
		if o := r.Header.Get("Origin"); o != "" {
			u, e := url.Parse(o)
			if e != nil || u.Host != r.Host {
				fail(w, "Другой источник запроса", 403)
				return
			}
		}
		next(w, r)
	}
}

type Metrics struct {
	CPU          float64 `json:"cpu"`
	MemUsed      int64   `json:"memUsed"`
	MemTotal     int64   `json:"memTotal"`
	MemAvailable int64   `json:"memAvailable"`
	Temp         float64 `json:"temp"`
	DiskFree     uint64  `json:"diskFree"`
	DiskTotal    uint64  `json:"diskTotal"`
	Uptime       float64 `json:"uptime"`
	Load         string  `json:"load"`
}

var lastTotal, lastIdle uint64
var metrics Metrics

func sample() {
	var t, i uint64
	fields := strings.Fields(strings.Split(read("/proc/stat"), "\n")[0])
	if len(fields) < 5 {
		return
	}
	for n, f := range fields[1:] {
		v, _ := strconv.ParseUint(f, 10, 64)
		t += v
		if n == 3 || n == 4 {
			i += v
		}
	}
	m := Metrics{}
	if t > lastTotal {
		m.CPU = math.Round((1-float64(i-lastIdle)/float64(t-lastTotal))*1000) / 10
	}
	lastTotal, lastIdle = t, i
	for _, line := range strings.Split(read("/proc/meminfo"), "\n") {
		f := strings.Fields(line)
		if len(f) > 1 {
			v, _ := strconv.ParseInt(f[1], 10, 64)
			switch f[0] {
			case "MemTotal:":
				m.MemTotal = v * 1024
			case "MemAvailable:":
				m.MemAvailable = v * 1024
			}
		}
	}
	m.MemUsed = m.MemTotal - m.MemAvailable
	for n := 0; n < 3; n++ {
		v, _ := strconv.ParseFloat(read(fmt.Sprintf("/sys/class/thermal/thermal_zone%d/temp", n)), 64)
		if v/1000 > m.Temp {
			m.Temp = v / 1000
		}
	}
	var st syscall.Statfs_t
	if syscall.Statfs("/data", &st) == nil {
		m.DiskTotal = st.Blocks * uint64(st.Bsize)
		m.DiskFree = st.Bavail * uint64(st.Bsize)
	}
	u := strings.Fields(read("/proc/uptime"))
	if len(u) > 0 {
		m.Uptime, _ = strconv.ParseFloat(u[0], 64)
	}
	m.Load = read("/proc/loadavg")
	mu.Lock()
	metrics = m
	mu.Unlock()
}
func main() {
	if len(os.Args) == 2 && os.Args[1] == "--access-bootstrap" {
		accessBootstrap()
		return
	}
	flag.StringVar(&base, "base", "/data/local/tmp/station-panel", "data directory")
	listen := flag.String("listen", ":8787", "listen address")
	flag.Parse()
	os.MkdirAll(base, 0750)
	token = read(filepath.Join(base, "access.key"))
	if len(token) < 32 {
		b := make([]byte, 24)
		rand.Read(b)
		token = hex.EncodeToString(b)
		if e := os.WriteFile(filepath.Join(base, "access.key"), []byte(token), 0600); e != nil {
			log.Fatal(e)
		}
	}
	if b, e := os.ReadFile(filepath.Join(base, "settings.json")); e == nil {
		json.Unmarshal(b, &cfg)
	}
	if cfg.Ring == "music" {
		cfg.Ring = "off"
		save()
	}
	if err := loadPairing(); err != nil {
		log.Fatal(err)
	}
	runtime.GOMAXPROCS(2)
	go func() {
		for {
			sample()
			time.Sleep(2 * time.Second)
		}
	}()
	go effects()
	go resumeAvatar()
	go ringAlbumWorker()
	go nativeConversationWorker()
	startVoiceEvents()
	loadLamps()
	initAmbient()
	go discoveryLoop()
	go restorePluginServices()
	event("Панель запущена на станции")
	sub, _ := fs.Sub(web, "web")
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/api/status", auth(status))
	mux.HandleFunc("/api/apps", auth(apps))
	mux.HandleFunc("/api/apps/icon", auth(appIconAPI))
	mux.HandleFunc("/api/pair/start", auth(pairingStartAPI))
	mux.HandleFunc("/api/pair", pairingAPI)
	mux.HandleFunc("/api/clients", auth(clientsAPI))
	mux.HandleFunc("/api/update", auth(updateAPI))
	mux.HandleFunc("/api/update/controller.apk", auth(updateAPK))
	mux.HandleFunc("/api/tv/install", auth(tvInstallAPI))
	mux.HandleFunc("/api/action", auth(action))
	mux.HandleFunc("/api/screen", auth(screenshot))
	mux.HandleFunc("/api/diagnostics", auth(diagnostics))
	mux.HandleFunc("/api/audio", auth(audioAPI))
	mux.HandleFunc("/api/audio/play", auth(audioPlayAPI))
	mux.HandleFunc("/api/scenarios", auth(scenariosAPI))
	mux.HandleFunc("/api/scenarios/run", auth(scenarioRunAPI))
	mux.HandleFunc("/api/music/providers", auth(providersAPI))
	mux.HandleFunc("/api/music/play", auth(musicPlayAPI))
	mux.HandleFunc("/api/music/album", auth(albumAPI))
	mux.HandleFunc("/api/maintenance", auth(maintenanceAPI))
	mux.HandleFunc("/api/ambient", auth(ambientAPI))
	mux.HandleFunc("/api/matrix/preset", auth(matrixPresetAPI))
	mux.HandleFunc("/api/ambient/preview", auth(ambientPreviewAPI))
	mux.HandleFunc("/api/ambient/designs", auth(clockDesignsAPI))
	mux.HandleFunc("/api/scenarios/catalog", auth(commandCatalogAPI))
	mux.HandleFunc("/api/alice/badge", auth(badgeAPI))
	mux.HandleFunc("/api/alice/badge/media", auth(badgeMediaAPI))
	mux.HandleFunc("/api/matrix/show", auth(matrixShowAPI))
	mux.HandleFunc("/api/lights", auth(lampsAPI))
	mux.HandleFunc("/api/lights/discover", auth(discoverLamps))
	mux.HandleFunc("/api/lights/action", auth(lightsActionAPI))
	mux.HandleFunc("/api/discovery", discovery)
	mux.HandleFunc("/api/plugins", auth(plugins))
	mux.HandleFunc("/api/plugins/upload", auth(pluginUpload))
	mux.HandleFunc("/api/plugins/action", auth(pluginAction))
	mux.HandleFunc("/api/matrix", auth(matrixAPI))
	mux.HandleFunc("/api/matrix/media", auth(matrixMedia))
	mux.HandleFunc("/api/matrix/library", auth(matrixLibrary))
	mux.HandleFunc("/api/alice/visual", auth(aliceVisualAPI))
	mux.HandleFunc("/api/music/visual", auth(musicVisualAPI))
	newServer := func(address string) *http.Server {
		return &http.Server{Addr: address, Handler: panelHeaders(mux), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 120 * time.Second, WriteTimeout: 180 * time.Second, IdleTimeout: 40 * time.Second, MaxHeaderBytes: 16384}
	}
	srv := newServer(*listen)
	secure := newServer(tlsAddress(*listen))
	log.Printf("Station Studio listening on %s", *listen)
	go func() {
		if err := secure.ListenAndServeTLS(filepath.Join(base, "tls.crt"), filepath.Join(base, "tls.key")); err != nil {
			log.Printf("Protected app connection: %v", err)
		}
	}()
	log.Fatal(srv.ListenAndServe())
}
func status(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		fail(w, "Метод не поддерживается", 405)
		return
	}
	media, _ := shell("dumpsys media_session")
	media = mediaSession(media, selectedProvider())
	volume, _ := run(8*time.Second, "/system/bin/media", "volume", "--stream", "3", "--get")
	vol := 0
	max := 15
	re := regexp.MustCompile(`volume is (\d+) in range \[(\d+)\.\.(\d+)\]`)
	if m := re.FindStringSubmatch(volume); len(m) > 3 {
		vol, _ = strconv.Atoi(m[1])
		max, _ = strconv.Atoi(m[3])
	}
	track := ""
	artist := ""
	if m := regexp.MustCompile(`metadata:size=\d+, description=(.*)`).FindStringSubmatch(media); len(m) > 1 {
		p := strings.Split(m[1], ", ")
		track = p[0]
		if track == "null" {
			track = ""
		}
		if len(p) > 1 {
			artist = p[1]
		}
	}
	playing := strings.Contains(media, "state=PlaybackState {state=3,")
	mu.Lock()
	m, c, e := metrics, cfg, append([]Event{}, events...)
	mu.Unlock()
	response(w, map[string]any{"ok": true, "model": "Яндекс Станция Макс", "android": "9", "build": buildID(), "root": os.Geteuid() == 0, "selinux": read("/sys/fs/selinux/enforce"), "rootStatus": read("/data/local/tmp/rootstatus"), "metrics": m, "config": c, "events": e, "volume": vol, "volumeMax": max, "track": track, "artist": artist, "playing": playing, "serverUptime": time.Since(started).Seconds(), "provider": selectedProvider()})
}
func fileExists(p string) bool { _, e := os.Stat(p); return e == nil }
func apps(w http.ResponseWriter, r *http.Request) {
	if data, e := os.ReadFile(avatarFolder + "/apps-index.json"); e == nil && json.Valid(data) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
		return
	}
	out, e := run(10*time.Second, "/system/bin/pm", "list", "packages", "-3", "-e")
	if e != nil {
		fail(w, out, 500)
		return
	}
	names := map[string]string{"com.spocky.projengmenu": "Projectivy · главный экран", "com.spotify.tv.android": "Spotify", "com.android.chrome": "Chrome", "com.netflix.mediaclient": "Netflix", "com.phlox.tvwebbrowser": "TV Bro", "cm.aptoidetv.pt": "Aptoide TV", "com.amazon.tv.leanbacklauncher": "Главный экран", "me.efesser.flauncher": "FLauncher", "me.zhanghai.android.files": "Файлы", "com.yablio.sendfilestotv": "Send Files to TV", "su.happ.proxyutility": "Happ", "com.android.vending": "Google Play", "local.station.studio": "Station Studio"}
	a := []map[string]string{}
	if p, e := run(5*time.Second, "/system/bin/pm", "path", "com.android.vending"); e == nil && strings.Contains(p, "package:") {
		a = append(a, map[string]string{"package": "com.android.vending", "name": "Google Play"})
	}
	for _, line := range strings.Fields(out) {
		p := strings.TrimPrefix(line, "package:")
		if p == "com.google.android.gms" || p == "com.google.android.gsf" {
			continue
		}
		n := names[p]
		if n == "" {
			n = p
		}
		a = append(a, map[string]string{"package": p, "name": n})
	}
	response(w, a)
}
func action(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		fail(w, "Нужен POST", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16000)
	var q Request
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		fail(w, "Некорректный запрос", 400)
		return
	}
	commandMu.Lock()
	defer commandMu.Unlock()
	var out string
	var e error
	switch q.Action {
	case "key":
		codes := map[string]int{"up": 19, "down": 20, "left": 21, "right": 22, "ok": 23, "home": 3, "back": 4, "play": 85, "next": 87, "previous": 88, "mute": 164, "volup": 24, "voldown": 25, "search": 84, "assist": 219, "tvpower": 177, "tvinput": 178}
		n, ok := codes[q.Text]
		if !ok {
			fail(w, "Неизвестная кнопка", 400)
			return
		}
		out, e = run(5*time.Second, "/system/bin/input", "keyevent", strconv.Itoa(n))
	case "volume":
		if q.Value < 0 || q.Value > 100 {
			fail(w, "Громкость 0–100", 400)
			return
		}
		out, e = run(8*time.Second, "/system/bin/media", "volume", "--stream", "3", "--set", strconv.Itoa(q.Value))
	case "launch":
		if !allowed.MatchString(q.Package) {
			fail(w, "Некорректное приложение", 400)
			return
		}
		out, e = launch(q.Package)
	case "text":
		if len(q.Text) > 1200 {
			fail(w, "Слишком длинный текст", 400)
			return
		}
		text := strings.ReplaceAll(q.Text, " ", "%s")
		out, e = run(8*time.Second, "/system/bin/input", "text", text)
	case "spotify-search":
		if len(q.Text) < 1 || len(q.Text) > 400 {
			fail(w, "Введите запрос", 400)
			return
		}
		uri := "spotify:search:" + url.PathEscape(q.Text)
		out, e = run(12*time.Second, "/system/bin/am", "start", "-a", "android.intent.action.VIEW", "-d", uri, "-p", "com.spotify.tv.android")
		event("Поиск в Spotify: " + q.Text)
	case "spotify-open":
		if !regexp.MustCompile(`^spotify:(track|album|artist|playlist):[A-Za-z0-9]{22}$`).MatchString(q.Text) {
			fail(w, "Нужен Spotify URI трека, альбома или плейлиста", 400)
			return
		}
		out, e = run(12*time.Second, "/system/bin/am", "start", "-a", "android.intent.action.VIEW", "-d", q.Text, "-p", "com.spotify.tv.android")
	case "ring":
		stopMusicVisual()
		valid := map[string]bool{"off": true, "solid": true, "breathe": true, "rainbow": true, "comet": true, "candle": true, "cover": true, "flag": true, "pixels": true}
		if !valid[q.Effect] || !regexp.MustCompile(`^#[0-9a-fA-F]{6}$`).MatchString(q.Color) || q.Value < 0 || q.Value > 100 {
			fail(w, "Некорректный эффект", 400)
			return
		}
		if q.Effect == "cover" && q.Motion != "" && q.Motion != "static" && q.Motion != "rotate" && q.Motion != "wave" && q.Motion != "breathe" {
			fail(w, "Выберите анимацию обложки", 400)
			return
		}
		if q.Effect == "pixels" && len(q.Palette) != 24 {
			fail(w, "Для кольца нужны 24 цвета", 400)
			return
		}
		if q.Effect == "flag" || q.Effect == "pixels" {
			if len(q.Palette) < 2 || len(q.Palette) > 24 || (q.Motion != "static" && q.Motion != "rotate" && q.Motion != "wave" && q.Motion != "breathe") {
				fail(w, "Выберите 2–24 цвета и анимацию", 400)
				return
			}
			for _, color := range q.Palette {
				if !regexp.MustCompile(`^#[0-9a-fA-F]{6}$`).MatchString(color) {
					fail(w, "Некорректный цвет", 400)
					return
				}
			}
		}
		mu.Lock()
		if q.Effect == "flag" || q.Effect == "pixels" {
			cfg.Palette = append([]string(nil), q.Palette...)
			cfg.FlagMotion = q.Motion
		}
		cfg.Ring = q.Effect
		cfg.PaletteSource = "palette"
		if q.Effect == "cover" {
			cfg.PaletteSource = "cover"
			cfg.FlagMotion = q.Motion
			if cfg.FlagMotion == "" {
				cfg.FlagMotion = "rotate"
			}
			if len(cfg.Palette) < 2 {
				cfg.Palette = []string{q.Color, "#ffffff"}
			}
		}
		cfg.Color = q.Color
		cfg.Brightness = q.Value
		mu.Unlock()
		save()
		event("Кольцо: " + q.Effect)
	case "sleep":
		if q.Value < 0 || q.Value > 180 {
			fail(w, "Таймер до 180 минут", 400)
			return
		}
		mu.Lock()
		cfg.SleepAt = 0
		if q.Value > 0 {
			cfg.SleepAt = time.Now().Add(time.Duration(q.Value) * time.Minute).Unix()
		}
		mu.Unlock()
		save()
		event("Таймер сна обновлён")
	case "mode":
		if q.Text != "balanced" && q.Text != "fast" && q.Text != "cinema" {
			fail(w, "Неизвестный режим", 400)
			return
		}
		scale := "0.5"
		if q.Text == "fast" {
			scale = "0"
		}
		if q.Text == "cinema" {
			scale = "1"
		}
		out, e = shell("settings put global window_animation_scale " + scale + "; settings put global transition_animation_scale " + scale + "; settings put global animator_duration_scale " + scale)
		if e == nil {
			mu.Lock()
			cfg.Mode = q.Text
			mu.Unlock()
			save()
			event("Режим интерфейса: " + q.Text)
		}
	case "optimize":
		out, e = run(65*time.Second, "/system/bin/sh", "-c", "am force-stop me.efesser.flauncher; cmd package compile -m speed -f com.spocky.projengmenu && cmd package compile -m speed -f com.spotify.tv.android")
		if e == nil {
			event("Подготовка приложений завершена")
		}
	case "voice", "chime":
		fail(w, "Озвучка удалена. Используйте аудиосценарии.", 410)
		return
	case "reboot":
		event("Перезагрузка по запросу владельца")
		response(w, map[string]any{"ok": true})
		go func() { time.Sleep(time.Second); run(5*time.Second, "/system/bin/reboot") }()
		return
	default:
		fail(w, "Неизвестное действие", 400)
		return
	}
	if e != nil {
		fail(w, out+": "+e.Error(), 500)
		return
	}
	response(w, map[string]any{"ok": true, "output": out})
}
func screenshot(w http.ResponseWriter, r *http.Request) {
	ctx, c := context.WithTimeout(r.Context(), 12*time.Second)
	defer c()
	data, e := exec.CommandContext(ctx, "/system/bin/sh", "-c", "exec screencap -p").Output()
	if e != nil {
		fail(w, e.Error(), 500)
		return
	}
	img, _, e := image.Decode(bytes.NewReader(data))
	if e != nil {
		fail(w, "Не удалось прочитать экран", 500)
		return
	}
	bounds := img.Bounds()
	width := 960
	height := bounds.Dy() * width / bounds.Dx()
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			dst.Set(x, y, img.At(bounds.Min.X+x*bounds.Dx()/width, bounds.Min.Y+y*bounds.Dy()/height))
		}
	}
	w.Header().Set("Content-Type", "image/jpeg")
	jpeg.Encode(w, dst, &jpeg.Options{Quality: 75})
}
func diagnostics(w http.ResponseWriter, r *http.Request) {
	s, e := shell("id; getenforce; getprop ro.build.display.id; getprop ro.build.version.release; cat /data/local/tmp/rootstatus; df -h /data; top -b -n 1 -m 8; stat -c '%a %u %g' /cache /cache/recovery; mount | grep ' /odm '")
	if e != nil {
		s += "\n" + e.Error()
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, s)
}
