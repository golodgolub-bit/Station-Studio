package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPreviewCancellationFinishesBeforeAnotherDisplayCommand(t *testing.T) {
	oldClient := matrixClient
	calls := make(chan string, 100)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var value map[string]any
		json.NewDecoder(r.Body).Decode(&value)
		if value["action"] == "play" {
			if lease, _ := value["duration"].(float64); lease < 5 {
				t.Error("display daemon rejects a lease below five seconds")
				w.WriteHeader(400)
				return
			}
		}
		calls <- value["action"].(string)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	matrixClient = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", strings.TrimPrefix(server.URL, "http://"))
	}}}
	defer func() { matrixClient = oldClient }()
	if e := startDisplayFeedback([][]byte{make([]byte, 400)}, []int{66}, 1, 50); e != nil {
		t.Fatal(e)
	}
	if e := startDisplayFeedback([][]byte{make([]byte, 400)}, []int{66}, 1, 50); e == nil {
		t.Fatal("overlapping preview accepted")
	}
	select {
	case action := <-calls:
		if action != "play" {
			t.Fatal(action)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("preview did not start")
	}
	stopDisplayFeedback()
	if displayFeedbackActive() {
		t.Fatal("preview retained display ownership")
	}
	last := ""
	for len(calls) > 0 {
		last = <-calls
	}
	if last != "stop" {
		t.Fatal("preview did not restore display", last)
	}
	time.Sleep(100 * time.Millisecond)
	if len(calls) != 0 {
		t.Fatal("old preview overwrote a later command")
	}
}

func TestAutomaticAmbientRestorePreservesNewerChoicesAndPreview(t *testing.T) {
	oldBase, oldClient := base, matrixClient
	oldCancel, oldDone, oldCurrent := ambientCancel, ambientDone, ambientCurrent
	oldMusic, oldMusicDone := musicCancel, musicDone
	oldUntil, oldFeedbackCancel := displayFeedbackUntil.Load(), displayFeedbackCancel.Load()
	base = t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"ok":true}`)) }))
	defer server.Close()
	matrixClient = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", strings.TrimPrefix(server.URL, "http://"))
	}}}
	defer func() {
		base = oldBase
		matrixClient = oldClient
		ambientCancel = oldCancel
		ambientDone = oldDone
		ambientCurrent = oldCurrent
		musicCancel = oldMusic
		musicDone = oldMusicDone
		displayFeedbackUntil.Store(oldUntil)
		displayFeedbackCancel.Store(oldFeedbackCancel)
	}()
	canceled := 0
	closed := make(chan struct{})
	close(closed)
	ambientCancel = func() { canceled++ }
	ambientDone = closed
	musicCancel = func() { canceled++ }
	musicDone = closed
	displayFeedbackUntil.Store(1)
	displayFeedbackCancel.Store(false)
	call := func() {
		request := httptest.NewRequest("POST", "/api/ambient", strings.NewReader(`{"action":"start","style":"digital","font":"pixel","speed":6,"brightness":60}`))
		request = request.WithContext(context.WithValue(request.Context(), ambientResumeKey{}, true))
		out := httptest.NewRecorder()
		ambientAPI(out, request)
		if out.Code != 200 {
			t.Fatal(out.Body.String())
		}
	}
	call()
	if canceled != 0 || displayFeedbackCancel.Load() {
		t.Fatal("startup restore canceled a newer user choice")
	}
	ambientCancel = nil
	ambientDone = nil
	call()
	if canceled != 0 || musicCancel == nil || displayFeedbackCancel.Load() {
		t.Fatal("background restoration stopped music or pairing preview")
	}
	stopAmbient()
}

func TestNewMusicScenesRemainSilentWithoutAudio(t *testing.T) {
	s := musicLight{Gain: 6, Level: .1, Phase: 1.2}
	for i := range s.Bands {
		s.Bands[i] = .01 * float64(i+1)
	}
	for _, mode := range []string{"spark", "tunnel"} {
		if !validMusicMode(mode) {
			t.Fatal(mode)
		}
		f := musicMatrixFrame(s, mode)
		quiet := musicMatrixFrame(musicLight{Gain: 6}, mode)
		if len(f) != 400 || bytes.Equal(f, quiet) || !bytes.Equal(quiet, make([]byte, 400)) {
			t.Fatal("scene does not follow sound", mode)
		}
	}
}

func TestMusicDiscoversBothCrossfadeTracksAndRejectsStaticMapping(t *testing.T) {
	capacity := 2048
	size := 0xd0 + capacity*4
	a, b := make([]byte, size*2), make([]byte, size*2)
	for track := 0; track < 2; track++ {
		off := size * track
		put := func(data []byte, at int, v uint32) { binary.LittleEndian.PutUint32(data[off+at:], v) }
		for _, data := range [][]byte{a, b} {
			put(data, 0x14, 48000)
			put(data, 0xa8, uint32(capacity))
		}
		put(a, 0, 0)
		put(b, 0, 7000)
		put(a, 0xb0, 0)
		put(b, 0xb0, 7000)
		put(b, 0xb4, 7000)
		for j := 0; j < capacity; j++ {
			v := int16(math.Sin(float64(j)*.1) * float64(1000+track*5000))
			binary.LittleEndian.PutUint16(b[off+0xd0+j*4:], uint16(v))
		}
	}
	found := musicBufferCandidates(a, b)
	if len(found) != 2 || found[1].Score <= found[0].Score {
		t.Fatal("crossfade streams lost", found)
	}
	if len(musicBufferCandidates(b, b)) != 0 {
		t.Fatal("static/paused buffers accepted")
	}
	binary.LittleEndian.PutUint32(b[0xa8:], 0xffffffff)
	if len(musicBufferCandidates(a, b)) != 1 {
		t.Fatal("invalid frame count accepted")
	}
	if len(musicBufferCandidates(a, b[:100])) != 0 {
		t.Fatal("short sample accepted")
	}
}
func TestHomeMembershipAcceptsReferencesWithoutDuplicatingIDs(t *testing.T) {
	ids := homeMemberIDs(json.RawMessage(`["lamp-1",{"id":"lamp-2"},"lamp-1",{"id":"../private"},null]`))
	if strings.Join(ids, ",") != "lamp-1,lamp-2" {
		t.Fatal(ids)
	}
}
func TestWebHomePreservesRealGroupsAndInfersMemberReferences(t *testing.T) {
	old := homeWebOrigin
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","households":[{"id":"house","all":[{"id":"lamp-1","name":"Lamp","type":"devices.types.light","item_type":"device","groups":["group-1"]},{"id":"group-1","name":"Living room","item_type":"group"},{"id":"empty","name":"Empty","item_type":"group"}]}]}`))
	}))
	defer server.Close()
	homeWebOrigin = server.URL
	defer func() { homeWebOrigin = old }()
	info, e := homeWebInfo(server.Client())
	if e != nil {
		t.Fatal(e)
	}
	if len(info.Devices) != 1 || len(info.Groups) != 1 {
		t.Fatal("group conversion lost membership", info.Groups)
	}
	var group struct {
		ID      string   `json:"id"`
		Devices []string `json:"devices"`
	}
	if json.Unmarshal(info.Groups[0], &group) != nil || group.ID != "group-1" || len(group.Devices) != 1 || group.Devices[0] != "lamp-1" {
		t.Fatal(group)
	}
}
func TestRuntimeGrantsAreRevocableAndVersionBound(t *testing.T) {
	old := base
	base = t.TempDir()
	defer func() { base = old }()
	m := PluginManifest{Name: "test/scene", Version: "1.0.0", Files: map[string]string{"share/file": "digest"}}
	if pluginRuntimeAllowed(m).Telemetry {
		t.Fatal("implicit telemetry")
	}
	g := pluginRuntimeGrant{Digest: pluginManifestDigest(m), Telemetry: true, Media: true, Display: true}
	raw, _ := json.Marshal(g)
	p := pluginRuntimePath(m.Name)
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, raw, 0600)
	if !pluginRuntimeAllowed(m).Display {
		t.Fatal("grant missing")
	}
	m.Version = "1.0.1"
	if pluginRuntimeAllowed(m).Media {
		t.Fatal("update inherited permission")
	}
	m.Version = "1.0.0"
	m.Files["share/file"] = "changed"
	if pluginRuntimeAllowed(m).Telemetry {
		t.Fatal("modified code inherited permission")
	}
	os.Remove(p)
	if pluginRuntimeAllowed(m).Display {
		t.Fatal("revocation ignored")
	}
}
func TestCloudLampUsesOnlyAdvertisedColorAndPreservesRestoreValue(t *testing.T) {
	oldBase, oldOrigin := base, homeOrigin
	base = t.TempDir()
	defer func() { base = oldBase; homeOrigin = oldOrigin }()
	os.WriteFile(filepath.Join(base, "home-oauth"), []byte("fixture-only-key"), 0600)
	values := []any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1.0/devices/lamp-1":
			w.Write([]byte(`{"status":"ok","id":"lamp-1","name":"Lamp","type":"devices.types.light","state":"online","capabilities":[{"type":"devices.capabilities.on_off","state":{"instance":"on","value":true}},{"type":"devices.capabilities.color_setting","parameters":{"color_model":"hsv"},"state":{"instance":"hsv","value":{"h":210,"s":50,"v":60}}}]}`))
		case "/v1.0/devices/actions":
			var q struct {
				Devices []homeAction `json:"devices"`
			}
			json.NewDecoder(r.Body).Decode(&q)
			if len(q.Devices) != 1 || q.Devices[0].Actions[0].Type != "devices.capabilities.color_setting" {
				t.Error("unsupported cloud command")
			}
			values = append(values, q.Devices[0].Actions[0].State["value"])
			w.Write([]byte(`{"status":"ok","devices":[{"capabilities":[{"state":{"action_result":{"status":"DONE"}}}]}]}`))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	homeOrigin = server.URL
	d, e := homeLightSnapshot(lamp{ID: "home:lamp-1", Name: "Lamp", Kind: "yandex"})
	if e != nil {
		t.Fatal(e)
	}
	if e = homeLightColor(d, [3]int{255, 0, 0}, 37); e != nil {
		t.Fatal(e)
	}
	if e = restoreHomeLight(d); e != nil {
		t.Fatal(e)
	}
	if len(values) != 2 || values[0].(map[string]any)["v"] != float64(37) || values[1].(map[string]any)["h"] != float64(210) {
		t.Fatal(values)
	}
	d.State = "offline"
	if homeColorModel(d) != "" {
		t.Fatal("offline lamp offered")
	}
}
