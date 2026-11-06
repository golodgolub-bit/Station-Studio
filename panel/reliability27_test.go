package main

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
