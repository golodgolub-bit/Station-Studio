package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDisplayReturnStopsPermanentTrackAndClearsResume(t *testing.T) {
	oldBase, oldClient := base, matrixClient
	base = t.TempDir()
	defer func() { base = oldBase; matrixClient = oldClient }()
	actions := make(chan string, 100)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q map[string]any
		json.NewDecoder(r.Body).Decode(&q)
		actions <- q["action"].(string)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	matrixClient = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", strings.TrimPrefix(server.URL, "http://"))
	}}}
	defer stopAmbient()
	start := httptest.NewRecorder()
	ambientAPI(start, httptest.NewRequest("POST", "/api/ambient", strings.NewReader(`{"action":"start","style":"track","duration":0,"brightness":85,"speed":6}`)))
	if start.Code != 200 {
		t.Fatal(start.Body.String())
	}
	if _, e := os.Stat(filepath.Join(base, "ambient.json")); e != nil {
		t.Fatal("persistent track was not saved")
	}
	stop := httptest.NewRecorder()
	displayControlAPI(stop, httptest.NewRequest("POST", "/api/display", strings.NewReader(`{"action":"stop-track"}`)))
	if stop.Code != 200 {
		t.Fatal(stop.Body.String())
	}
	ambientMu.Lock()
	active := ambientCancel != nil
	ambientMu.Unlock()
	if active {
		t.Fatal("track retained display ownership")
	}
	if _, e := os.Stat(filepath.Join(base, "ambient.json")); !os.IsNotExist(e) {
		t.Fatal("track could resume after restart")
	}
	get := httptest.NewRecorder()
	displayControlAPI(get, httptest.NewRequest("GET", "/api/display", nil))
	if !strings.Contains(get.Body.String(), `"source":"native"`) {
		t.Fatal(get.Body.String())
	}
	blank := httptest.NewRecorder()
	displayControlAPI(blank, httptest.NewRequest("POST", "/api/display", strings.NewReader(`{"action":"blank"}`)))
	if blank.Code != 200 {
		t.Fatal(blank.Body.String())
	}
	if bytes.Count(ambientFrame(ambientConfig{Style: "blank"}, time.Now(), 0, "title", "artist"), []byte{0}) != 400 {
		t.Fatal("blank frame was not blank")
	}
	normal := httptest.NewRecorder()
	displayControlAPI(normal, httptest.NewRequest("POST", "/api/display", strings.NewReader(`{"action":"native"}`)))
	if normal.Code != 200 {
		t.Fatal(normal.Body.String())
	}
	if len(actions) == 0 {
		t.Fatal("display daemon never received release")
	}
	last := ""
	for len(actions) > 0 {
		last = <-actions
	}
	if last != "stop" {
		t.Fatal("last frame wasn't released", last)
	}
}

func TestProfilesAreBoundToPairedClientAndRejectForgedTelegram(t *testing.T) {
	oldBase, oldToken, oldClients := base, token, pairedClients
	base = t.TempDir()
	token = strings.Repeat("a", 48)
	pairedClients = nil
	defer func() { base = oldBase; token = oldToken; pairedClients = oldClients }()
	call := func(method, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/profile", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		profileAPI(w, r)
		return w
	}
	if w := call("POST", `{"name":"Павел","accent":"mint","telegram":{"id":"999"}}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	get := call("GET", "")
	if !strings.Contains(get.Body.String(), "Павел") || strings.Contains(get.Body.String(), `"id":"999"`) {
		t.Fatal(get.Body.String())
	}
	if w := call("POST", `{"name":"bad\nname","accent":"violet"}`); w.Code != 400 {
		t.Fatal("control characters accepted")
	}
	unknown := httptest.NewRecorder()
	profileAPI(unknown, httptest.NewRequest("GET", "/api/profile", nil))
	if unknown.Code != 401 {
		t.Fatal("unknown client read a profile")
	}
	w := httptest.NewRecorder()
	stationAppearanceAPI(w, httptest.NewRequest("POST", "/api/station/appearance", strings.NewReader(`{"variant":"light"}`)))
	if w.Code != 200 || getAppearance().Variant != "light" {
		t.Fatal(w.Body.String())
	}
	invalid := httptest.NewRecorder()
	stationAppearanceAPI(invalid, httptest.NewRequest("POST", "/api/station/appearance", strings.NewReader(`{"variant":"other"}`)))
	if invalid.Code != 400 || getAppearance().Variant != "light" {
		t.Fatal("invalid appearance overwrote selection")
	}
}

func TestTelegramLinkRequiresOwnedRequestAndMatchingNonce(t *testing.T) {
	oldBase, oldToken, oldClients, oldKey := base, token, pairedClients, telegramProfileKey
	base = t.TempDir()
	token = strings.Repeat("a", 48)
	pairedClients = nil
	telegramAttempts = map[string]*telegramAttempt{}
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	telegramProfileKey = public
	defer func() {
		base = oldBase
		token = oldToken
		pairedClients = oldClients
		telegramProfileKey = oldKey
		telegramAttempts = map[string]*telegramAttempt{}
	}()
	call := func(method, key, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/profile/telegram", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		telegramProfileAPI(w, r)
		return w
	}
	start := call("POST", token, `{}`)
	if start.Code != 200 {
		t.Fatal(start.Body.String())
	}
	var q struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	json.Unmarshal(start.Body.Bytes(), &q)
	if !strings.HasSuffix(q.URL, "?start=al_"+q.ID) {
		t.Fatal("wrong bot link")
	}
	sign := func(id string, expired bool) string {
		nonce, _ := hex.DecodeString(id)
		name, user := []byte("Павел"), []byte("fixture")
		raw := make([]byte, 35+len(name)+len(user))
		raw[0] = 1
		copy(raw[1:17], nonce)
		binary.BigEndian.PutUint64(raw[17:25], 12345)
		now := time.Now().Unix()
		if expired {
			now -= 400
		}
		binary.BigEndian.PutUint32(raw[25:29], uint32(now))
		binary.BigEndian.PutUint32(raw[29:33], uint32(now+300))
		raw[33] = byte(len(name))
		copy(raw[34:], name)
		raw[34+len(name)] = byte(len(user))
		copy(raw[35+len(name):], user)
		sig := ed25519.Sign(private, append([]byte(profileProofDomain), raw...))
		return "AK1." + base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(sig)
	}
	confirm := func(proof, key string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"action": "confirm", "proof": proof})
		return call("POST", key, string(body))
	}
	proof := sign(q.ID, false)
	if w := confirm(proof, strings.Repeat("d", 48)); w.Code != 401 {
		t.Fatal("another device accepted login")
	}
	if w := confirm(sign(strings.Repeat("e", 32), false), token); w.Code != 410 {
		t.Fatal("nonce not bound to local request", w.Body.String())
	}
	if w := confirm(sign(q.ID, true), token); w.Code != 400 {
		t.Fatal("expired proof accepted")
	}
	pieces := strings.Split(proof, ".")
	signature, _ := base64.RawURLEncoding.DecodeString(pieces[2])
	signature[0] ^= 1
	bad := pieces[0] + "." + pieces[1] + "." + base64.RawURLEncoding.EncodeToString(signature)
	if w := confirm(bad, token); w.Code != 400 {
		t.Fatal("forged signature accepted")
	}
	if readProfile("owner").Telegram != nil {
		t.Fatal("unverified identity persisted")
	}
	if w := confirm(proof, token); w.Code != 200 || !strings.Contains(w.Body.String(), `"id":"12345"`) {
		t.Fatal(w.Body.String())
	}
	if readProfile("owner").Telegram == nil {
		t.Fatal("confirmed identity was not persisted")
	}
	if w := confirm(proof, token); w.Code != 410 {
		t.Fatal("completed proof reused")
	}
}

// This packet was signed by the actual TypeScript Worker test with an ephemeral
// fixture key. Neither production secrets nor a fixture private key are stored.
func TestTelegramWorkerAndStationUseIdenticalSignedFormat(t *testing.T) {
	b, err := os.ReadFile("testdata/telegram-profile-fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct{ PublicKey, Proof, Nonce, ID, Name string }
	if err = json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	old := telegramProfileKey
	defer func() { telegramProfileKey = old }()
	telegramProfileKey, err = base64.StdEncoding.DecodeString(f.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(f.Proof, ".")
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(int64(binary.BigEndian.Uint32(raw[25:29]))+1, 0)
	identity, nonce, err := decodeTelegramProof(f.Proof, now)
	if err != nil || nonce != f.Nonce || identity.ID != f.ID || identity.Name != f.Name {
		t.Fatal(identity, nonce, err)
	}
}
