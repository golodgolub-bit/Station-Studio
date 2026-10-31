package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
)

type audioClip struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}
type scenarioStep struct {
	API  string          `json:"api"`
	Data json.RawMessage `json:"data"`
}
type voiceRule struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Phrases []string       `json:"phrases"`
	Enabled bool           `json:"enabled"`
	Steps   []scenarioStep `json:"steps"`
}
type voiceSettings struct {
	Rules           []voiceRule `json:"rules"`
	MusicRouting    bool        `json:"musicRouting"`
	Provider        string      `json:"provider"`
	BuiltinCommands bool        `json:"builtinCommands"`
	QuickCommands   bool        `json:"quickCommands"`
	BuiltinDisabled []string    `json:"builtinDisabled"`
}

var scenarioMu sync.Mutex
var scenarioRunMu sync.Mutex
var voiceCfg = voiceSettings{Rules: []voiceRule{}, Provider: "com.spotify.tv.android", BuiltinCommands: true, QuickCommands: true}
var lastVoiceState = "unknown"
var lastVoiceSeen time.Time
var voiceRequests = map[string]time.Time{}
var scenarioLast string

// Short-lived local diagnostics; never written to disk or event logs.
var voiceDiagnostic map[string]any
var nativeOverlayUntil int64
var nativeAlarmActive bool

func writeAliceStateLocked() {
	b, _ := json.Marshal(map[string]any{"state": lastVoiceState, "time": lastVoiceSeen.UnixMilli(), "overlayUntil": nativeOverlayUntil, "alarmActive": nativeAlarmActive})
	p := filepath.Join(base, "alice-state.json")
	if os.WriteFile(p+".tmp", b, 0600) == nil {
		os.Rename(p+".tmp", p)
	}
}

func randomID() string { b := make([]byte, 12); rand.Read(b); return hex.EncodeToString(b) }
func normalizePhrase(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ReplaceAll(strings.ToLower(s), "ё", "е"), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }), " ")
}
func loadScenarios() {
	b, _ := os.ReadFile(filepath.Join(base, "scenarios.json"))
	scenarioMu.Lock()
	defer scenarioMu.Unlock()
	json.Unmarshal(b, &voiceCfg)
	if voiceCfg.Rules == nil {
		voiceCfg.Rules = []voiceRule{}
	}
	if voiceCfg.Provider == "" {
		voiceCfg.Provider = "com.spotify.tv.android"
	}
}
func saveScenariosLocked() error {
	b, e := json.Marshal(voiceCfg)
	if e == nil {
		e = os.WriteFile(filepath.Join(base, "scenarios.json.tmp"), b, 0600)
	}
	if e == nil {
		e = os.Rename(filepath.Join(base, "scenarios.json.tmp"), filepath.Join(base, "scenarios.json"))
	}
	return e
}
func selectedProvider() string {
	scenarioMu.Lock()
	defer scenarioMu.Unlock()
	return voiceCfg.Provider
}

func audioList() []audioClip {
	list := []audioClip{}
	files, _ := os.ReadDir(filepath.Join(base, "audio"))
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		b, _ := os.ReadFile(filepath.Join(base, "audio", f.Name()))
		var clip audioClip
		if json.Unmarshal(b, &clip) == nil {
			list = append(list, clip)
		}
	}
	return list
}
func audioAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		response(w, map[string]any{"ok": true, "clips": audioList(), "state": json.RawMessage(validJSONFile(avatarFolder + "/audio-state.json"))})
	case "POST":
		name := strings.TrimSpace(r.URL.Query().Get("name"))
		if name == "" || len([]rune(name)) > 80 {
			fail(w, "Укажите название до 80 символов", 400)
			return
		}
		scenarioMu.Lock()
		defer scenarioMu.Unlock()
		list := audioList()
		var total int64
		for _, c := range list {
			total += c.Size
		}
		if len(list) >= 60 || total >= 80<<20 {
			fail(w, "Библиотека: до 60 файлов и 80 МБ", 400)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 20<<20)
		data, e := io.ReadAll(r.Body)
		if e != nil || len(data) < 16 {
			fail(w, "Аудиофайл до 20 МБ", 400)
			return
		}
		signature := string(data[:4])
		valid := signature == "RIFF" || signature == "OggS" || signature == "fLaC" || string(data[:3]) == "ID3" || string(data[4:8]) == "ftyp" || data[0] == 0xff && data[1]&0xe0 == 0xe0
		if !valid {
			fail(w, "Нужен MP3, WAV, OGG, FLAC, AAC или M4A", 400)
			return
		}
		var st syscall.Statfs_t
		syscall.Statfs("/data", &st)
		if total+int64(len(data)) > 80<<20 || st.Bavail*uint64(st.Bsize) < uint64(len(data))+(100<<20) {
			fail(w, "Недостаточно места: оставляем 100 МБ для системы", 409)
			return
		}
		clip := audioClip{randomID(), name, int64(len(data))}
		if e = helperWrite("audio-"+clip.ID+".media", data); e != nil {
			fail(w, e.Error(), 500)
			return
		}
		dir := filepath.Join(base, "audio")
		os.MkdirAll(dir, 0700)
		b, _ := json.Marshal(clip)
		if e = os.WriteFile(filepath.Join(dir, clip.ID+".json"), b, 0600); e != nil {
			os.Remove(avatarFolder + "/audio-" + clip.ID + ".media")
			fail(w, e.Error(), 500)
			return
		}
		response(w, map[string]any{"ok": true, "clip": clip})
	case "DELETE":
		id := r.URL.Query().Get("id")
		if !isMediaID(id) {
			fail(w, "Неверный файл", 400)
			return
		}
		scenarioMu.Lock()
		defer scenarioMu.Unlock()
		for _, rule := range voiceCfg.Rules {
			for _, step := range rule.Steps {
				if bytes.Contains(step.Data, []byte(id)) {
					fail(w, "Сначала удалите этот файл из сценария «"+rule.Name+"»", 409)
					return
				}
			}
		}
		os.Remove(filepath.Join(base, "audio", id+".json"))
		os.Remove(avatarFolder + "/audio-" + id + ".media")
		response(w, map[string]bool{"ok": true})
	default:
		fail(w, "Метод не поддерживается", 405)
	}
}
func validJSONFile(path string) []byte {
	b, e := os.ReadFile(path)
	if e != nil || !json.Valid(b) {
		return []byte("{}")
	}
	return b
}
func isMediaID(id string) bool {
	b, e := hex.DecodeString(id)
	return e == nil && len(b) == 12 && id == strings.ToLower(id)
}
func audioPlayAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		fail(w, "Нужен POST", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	var q struct {
		Action string `json:"action"`
		ID     string `json:"id"`
		Volume int    `json:"volume"`
	}
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		fail(w, "Неверный запрос", 400)
		return
	}
	if q.Action != "stop" && (!isMediaID(q.ID) || !fileExists(filepath.Join(base, "audio", q.ID+".json")) || q.Volume < 0 || q.Volume > 100) {
		fail(w, "Выберите аудио и громкость 0–100", 400)
		return
	}
	out, e := run(10*time.Second, "/system/bin/am", "start-foreground-service", "--user", "0", "-n", "local.station.studio/.AudioService", "--es", "operation", q.Action, "--es", "media", q.ID, "--ei", "volume", fmt.Sprint(q.Volume))
	if e != nil || strings.Contains(out, "Error") {
		fail(w, out, 500)
		return
	}
	response(w, map[string]bool{"ok": true})
}

// All scenario steps pass through the same validation as the panel buttons.
var scenarioHandlers = map[string]http.HandlerFunc{}

func initScenarioHandlers() {
	scenarioHandlers = map[string]http.HandlerFunc{"action": action, "audio/play": audioPlayAPI, "matrix": matrixAPI, "matrix/show": matrixShowAPI, "matrix/preset": matrixPresetAPI, "music/visual": musicVisualAPI, "music/play": musicPlayAPI, "ambient": ambientAPI, "alice/visual": aliceVisualAPI, "plugins/action": pluginAction, "lights/action": lightsActionAPI, "maintenance": maintenanceAPI}
}

type captureResponse struct {
	header http.Header
	body   bytes.Buffer
	code   int
}

func (c *captureResponse) Header() http.Header { return c.header }
func (c *captureResponse) WriteHeader(n int)   { c.code = n }
func (c *captureResponse) Write(b []byte) (int, error) {
	if c.code == 0 {
		c.code = 200
	}
	return c.body.Write(b)
}
func executeSteps(steps []scenarioStep) error {
	for _, s := range steps {
		handler := scenarioHandlers[s.API]
		if handler == nil {
			return fmt.Errorf("Неизвестное действие")
		}
		req, _ := http.NewRequest("POST", "http://local/api/"+s.API, bytes.NewReader(s.Data))
		out := &captureResponse{header: make(http.Header)}
		handler(out, req)
		if out.code >= 400 {
			var result struct {
				Error string `json:"error"`
			}
			json.Unmarshal(out.body.Bytes(), &result)
			return fmt.Errorf("%s", result.Error)
		}
	}
	return nil
}
func scenariosAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		scenarioMu.Lock()
		defer scenarioMu.Unlock()
		var diagnostic map[string]any
		if last, ok := voiceDiagnostic["time"].(int64); ok && time.Now().Unix()-last < 180 {
			diagnostic = voiceDiagnostic
		}
		response(w, map[string]any{"ok": true, "settings": voiceCfg, "observer": lastVoiceState, "lastResult": scenarioLast, "diagnostic": diagnostic, "nativeIPC": nativeIPCStatus(), "quickVoice": quickVoiceStatus(), "conversation": json.RawMessage(validJSONFile(filepath.Join(base, "native-conversation.json")))})
		return
	}
	if r.Method != "POST" {
		fail(w, "Нужен GET или POST", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	var next voiceSettings
	if json.NewDecoder(r.Body).Decode(&next) != nil || len(next.Rules) > 100 || !allowed.MatchString(next.Provider) {
		fail(w, "Некорректные настройки", 400)
		return
	}
	seen := map[string]bool{}
	for _, rule := range next.Rules {
		if !libraryID.MatchString(rule.ID) || len(rule.Name) < 1 || len(rule.Name) > 160 || len(rule.Phrases) < 1 || len(rule.Phrases) > 64 || len(rule.Steps) < 1 || len(rule.Steps) > 8 {
			fail(w, "Сценарий: название, 1–64 фразы и 1–8 действий", 400)
			return
		}
		for _, phrase := range rule.Phrases {
			n := normalizeVoicePhrase(phrase)
			if len(n) < 3 || len(n) > 400 || seen[n] {
				fail(w, "Фразы должны быть уникальными, от 3 до 400 байт", 400)
				return
			}
			seen[n] = true
		}
		for _, step := range rule.Steps {
			if scenarioHandlers[step.API] == nil || len(step.Data) > 128<<10 || !json.Valid(step.Data) {
				fail(w, "Неизвестное действие сценария", 400)
				return
			}
		}
	}
	scenarioMu.Lock()
	before := voiceCfg
	voiceCfg = next
	e := saveScenariosLocked()
	if e != nil {
		voiceCfg = before
	}
	scenarioMu.Unlock()
	if e != nil {
		fail(w, e.Error(), 500)
		return
	}
	response(w, map[string]bool{"ok": true})
}
func scenarioRunAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		fail(w, "Нужен POST", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	var q struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		fail(w, "Неверный запрос", 400)
		return
	}
	scenarioMu.Lock()
	var steps []scenarioStep
	for _, rule := range voiceCfg.Rules {
		if rule.ID == q.ID {
			steps = append([]scenarioStep{}, rule.Steps...)
		}
	}
	scenarioMu.Unlock()
	if len(steps) == 0 {
		fail(w, "Сценарий не найден", 404)
		return
	}
	scenarioRunMu.Lock()
	defer scenarioRunMu.Unlock()
	if e := executeSteps(steps); e != nil {
		fail(w, e.Error(), 409)
		return
	}
	response(w, map[string]bool{"ok": true})
}
func matrixShowAPI(w http.ResponseWriter, r *http.Request) {
	var q struct {
		ID       string `json:"id"`
		Duration *int   `json:"duration"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&q) != nil || !libraryID.MatchString(q.ID) {
		fail(w, "Выберите анимацию", 400)
		return
	}
	var a matrixArtwork
	b, e := os.ReadFile(filepath.Join(base, "artworks", q.ID+".json"))
	if e != nil || json.Unmarshal(b, &a) != nil {
		fail(w, "Анимация не найдена", 404)
		return
	}
	duration := a.Duration
	if q.Duration != nil {
		duration = *q.Duration
	}
	for _, f := range a.Frames {
		for i, v := range f {
			f[i] = correctGray(v, a.Gamma, a.Exposure)
		}
	}
	b, _ = json.Marshal(map[string]any{"action": "play", "frames": a.Frames, "delays": a.Delays, "duration": duration, "brightness": a.Brightness})
	req, _ := http.NewRequest("POST", "http://local/api/matrix", bytes.NewReader(b))
	matrixAPI(w, req)
}

func voiceEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		fail(w, "POST", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var q struct {
		State       string `json:"state"`
		Text        string `json:"text"`
		ID          string `json:"requestId"`
		Kind        string `json:"kind"`
		AlarmActive *bool  `json:"alarmActive"`
	}
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		fail(w, "Invalid event", 400)
		return
	}
	scenarioMu.Lock()
	if q.Kind != "quick" {
		lastVoiceState = q.State
		lastVoiceSeen = time.Now()
	}
	if q.AlarmActive != nil {
		nativeAlarmActive = *q.AlarmActive
	}
	if q.Kind == "volume" || q.Kind == "microphone" {
		nativeOverlayUntil = time.Now().Add(2500 * time.Millisecond).UnixMilli()
	}
	if q.State == "idle" || q.State == "listening" || q.State == "thinking" || q.State == "speaking" {
		writeAliceStateLocked()
	}
	now := time.Now()
	for id, t := range voiceRequests {
		if now.Sub(t) > 10*time.Minute {
			delete(voiceRequests, id)
		}
	}
	_, duplicate := voiceRequests[q.ID]
	if q.ID != "" && q.Text != "" && !duplicate {
		voiceRequests[q.ID] = now
	}
	rules := append([]voiceRule{}, voiceCfg.Rules...)
	routing := voiceCfg.MusicRouting
	settings := voiceCfg
	scenarioMu.Unlock()
	if q.ID == "" || q.Text == "" || duplicate {
		response(w, map[string]bool{"ok": true, "handled": false})
		return
	}
	query := normalizeVoicePhrase(q.Text)
	if q.Kind == "quick" {
		rule, ok := quickRule(query, settings)
		if !ok {
			response(w, map[string]bool{"ok": true, "handled": false})
			return
		}
		if pairedQuickDuplicate(query, "quick", time.Now()) {
			quickDuplicates.Add(1)
			response(w, map[string]bool{"ok": true, "handled": true})
			return
		}
		quickRouted.Add(1)
		scenarioMu.Lock()
		voiceDiagnostic = map[string]any{"text": query, "route": "quick", "time": time.Now().Unix()}
		scenarioMu.Unlock()
		response(w, map[string]bool{"ok": true, "handled": true})
		dispatchMusicRule(rule)
		return
	}
	if settings.QuickCommands && settings.BuiltinCommands && isShortMusicCommand(query) && selectedMusicContext() && pairedQuickDuplicate(query, "asr", time.Now()) {
		quickDuplicates.Add(1)
		handledVoice(w)
		return
	}
	setDiagnostic := func(kind string) {
		scenarioMu.Lock()
		voiceDiagnostic = map[string]any{"text": q.Text, "route": kind, "time": time.Now().Unix()}
		scenarioMu.Unlock()
	}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		for _, phrase := range rule.Phrases {
			if normalizeVoicePhrase(phrase) == query {
				setDiagnostic("scenario")
				handledVoice(w)
				go runVoiceScenario(rule)
				return
			}
		}
	}
	if rule, ok := builtInVoice(query, settings); ok {
		setDiagnostic("scenario")
		handledVoice(w)
		if strings.HasPrefix(rule.ID, "music-") {
			dispatchMusicRule(rule)
		} else {
			go runVoiceScenario(rule)
		}
		return
	}
	if routing {
		if query, ok := musicQuery(q.Text); ok {
			setDiagnostic("music")
			handledVoice(w)
			go func() {
				e := playMusicQuery(query)
				scenarioMu.Lock()
				if e != nil {
					scenarioLast = "Музыка: " + e.Error()
				} else {
					scenarioLast = "Песня запущена в выбранном плеере"
				}
				scenarioMu.Unlock()
			}()
			return
		}
	}
	setDiagnostic("native")
	response(w, map[string]bool{"ok": true, "handled": false})
}
func runVoiceScenario(rule voiceRule) {
	scenarioRunMu.Lock()
	defer scenarioRunMu.Unlock()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		scenarioMu.Lock()
		state := lastVoiceState
		scenarioMu.Unlock()
		if state == "idle" {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	e := executeSteps(rule.Steps)
	scenarioMu.Lock()
	scenarioLast = "Сценарий «" + rule.Name + "» выполнен"
	if e != nil {
		scenarioLast = "Сценарий «" + rule.Name + "»: " + e.Error()
	}
	scenarioMu.Unlock()
}
func startVoiceEvents() {
	loadScenarios()
	initScenarioHandlers()
	go func() {
		for n := 0; n < 180 && read("/sys/fs/selinux/enforce") != "0"; n++ {
			time.Sleep(time.Second)
		}
		dir := "/data/local/station-studio-events"
		os.MkdirAll(dir, 0711)
		os.Chmod(dir, 0711)
		path := dir + "/events.sock"
		os.Remove(path)
		listener, e := net.Listen("unix", path)
		if e != nil {
			event("Не удалось запустить обработку голосовых команд")
			return
		}
		for i := 0; i < 90; i++ {
			if folder, e := os.Stat(avatarFolder); e == nil {
				st := folder.Sys().(*syscall.Stat_t)
				os.Chown(path, int(st.Uid), int(st.Gid))
				os.Chmod(path, 0600)
				break
			}
			time.Sleep(time.Second)
		}
		server := &http.Server{Handler: http.HandlerFunc(voiceEvent), ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, MaxHeaderBytes: 4096}
		server.Serve(listener)
	}()
}
