package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPixelFontHasCrispCyrillicAndLatin(t *testing.T) {
	for _, text := range []string{"АЛИСА", "GAGA", "ЁЖ"} {
		f := make([]byte, 400)
		pixelText(f, text, 4, 30, 0, "center", 255)
		lit := 0
		for _, v := range f {
			if v != 0 && v != 255 {
				t.Fatal("Antialiasing in pixel font")
			}
			if v == 255 {
				lit++
			}
		}
		if lit < 10 {
			t.Fatal("Empty text: ", text)
		}
	}
}
func TestClockFacesHaveDistinctGeometry(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 34, 20, 0, time.UTC)
	a := ambientFrame(ambientConfig{Style: "digital"}, now, 0, "", "")
	b := ambientFrame(ambientConfig{Style: "segments"}, now, 0, "", "")
	c := ambientFrame(ambientConfig{Style: "stack"}, now, 0, "", "")
	if bytes.Equal(a, b) || bytes.Equal(b, c) || bytes.Equal(a, c) {
		t.Fatal("Clock faces identical")
	}
}
func TestRingPaletteInterpolatesAcrossColours(t *testing.T) {
	r, g, b := paletteBlend([]string{"#ff0000", "#0000ff"}, .25)
	if r < 127 || r > 128 || b < 127 || b > 128 || g != 0 {
		t.Fatal(r, g, b)
	}
}

func TestSystemAudioSelectsLiveMediaInsteadOfEarlierLogs(t *testing.T) {
	dump := "Audio event log: volume changes\nold lines\nPlaybackActivityMonitor dump time: 17:00\nplayers:\nID:23 -- u/pid:10061/4354 -- state:paused -- attr:usage=USAGE_MEDIA content=TYPE\nID:599 -- u/pid:10063/9266 -- state:started -- attr:usage=USAGE_MEDIA content=TYPE\nID:15 -- u/pid:1000/3737 -- state:started -- attr:usage=USAGE_ASSISTANCE_SONIFICATION content=TYPE\nAudio event log: playback\nold state:started u/pid:10061/999 usage=USAGE_MEDIA content=TYPE"
	if activeMediaPID(dump) != "9266" {
		t.Fatal("Wrong media PID: ", activeMediaPID(dump))
	}
}

func TestVoiceNormalization(t *testing.T) {
	if normalizePhrase("  Покажи ЗВЁЗДНЫЕ часы! ") != "покажи звездные часы" {
		t.Fatal("Voice normalization")
	}
	if _, ok := musicQuery("поставь таймер на пять минут"); ok {
		t.Fatal("Non-music request was intercepted")
	}
	for _, s := range []string{"Алиса, включи песню Lady Gaga Disease!", "Включи Lady Gaga Cure Your Disease", "Поставь Adele Hello в Spotify"} {
		if _, ok := musicQuery(s); !ok {
			t.Fatal("Music missed: ", s)
		}
	}
	for _, s := range []string{"включи свет на кухне", "поставь будильник на семь", "включи телевизор", "включи радугу на станции"} {
		if _, ok := musicQuery(s); ok {
			t.Fatal("Household intent intercepted: ", s)
		}
	}
}
func TestSpotifySelectionRequiresExactSearch(t *testing.T) {
	data := []byte(`<hierarchy><node package="com.spotify.tv.android" text="Лучшие результаты по запросу Out Of The Woods Taylor Swift"><node package="com.spotify.tv.android" clickable="true" content-desc="Out Of The Woods" bounds="[100,568][312,848]"><node text="Трек • Taylor Swift"/></node><node package="com.spotify.tv.android" clickable="true" content-desc="Playlist" bounds="[50,300][200,400]"><node text="Плейлист"/></node></node></hierarchy>`)
	x, y, name, e := spotifySearchTarget(data, "Out Of The Woods Taylor Swift")
	if e != nil || x != 206 || y != 708 || name != "Out Of The Woods" {
		t.Fatalf("%d %d %s %v", x, y, name, e)
	}
	if _, _, _, e = spotifySearchTarget(data, "Adele Hello"); e == nil {
		t.Fatal("Clicked stale results")
	}
	if _, _, _, e = spotifySearchTarget([]byte(strings.ReplaceAll(string(data), "com.spotify.tv.android", "another.app")), "Out Of The Woods Taylor Swift"); e == nil {
		t.Fatal("Clicked a different app")
	}
}
func TestSpotifyAutoplayRequiresMatchingMetadata(t *testing.T) {
	if !spotifyQueryMatches("Lady Gaga Disease", "Disease", "Lady Gaga") {
		t.Fatal("Direct playback missed")
	}
	if spotifyQueryMatches("Lady Gaga Disease", "Hello", "Adele") {
		t.Fatal("Unrelated playback accepted")
	}
	if spotifyQueryMatches("Disease", "Disease Remix", "Someone") == false {
		t.Fatal("Matching title word missed")
	}
	if spotifyQueryMatches("", "Disease", "Lady Gaga") {
		t.Fatal("Empty query accepted")
	}
}
func TestQualifiedVoiceAssetsAndMediaParameters(t *testing.T) {
	settings := voiceSettings{BuiltinCommands: true}
	for _, q := range []string{"пожалуйста выключи музыку", "алиса следующую песню пожалуйста", "отключи музыку", "включи музыку", "следующую песню", "предыдущую песню", "отобрази сердечко на лед дисплее", "включи гей флаг на кольце", "покажи медузу на дисплее", "перемотай назад на сорок секунд", "перемотай на две минуты вперед"} {
		if _, ok := builtInVoice(q, settings); !ok {
			t.Fatal("Missed qualified command: ", q)
		}
	}
	for _, q := range []string{"что такое гей флаг", "расскажи про медузу", "поставь таймер на две минуты", "перемотай на сто часов"} {
		if _, ok := builtInVoice(q, settings); ok {
			t.Fatal("Unrelated command intercepted: ", q)
		}
	}
	seen := map[string]string{}
	for _, c := range commandCatalog() {
		for _, p := range c.Phrases {
			n := normalizePhrase(p)
			if id, ok := seen[n]; ok && id != c.ID {
				t.Fatalf("Ambiguous phrase %q: %s and %s", p, id, c.ID)
			}
			seen[n] = c.ID
		}
	}
}
func TestNativeCancelUsesConversationMethod(t *testing.T) {
	b := nativeCancelFrame()
	if len(b) < 5 {
		t.Fatal("Empty native frame")
	}
	envelope := b[4:]
	if ipcFind(envelope, 253).value != 210 {
		t.Fatal("Wrong native message type")
	}
	rpc := ipcFind(envelope, 252).data
	if string(ipcFind(rpc, 7).data) != "AliceCapability" || ipcFind(ipcFind(rpc, 8).data, 1).value != 3 {
		t.Fatal("Wrong conversation method")
	}
}
func TestEarlyVoiceRequiresFinalMatchingASR(t *testing.T) {
	makeFrame := func(final uint64, echo string) []byte {
		asr := ipcBytes(nil, 1, []byte("включи песню disease"))
		asr = ipcNumber(asr, 2, 1)
		asr = ipcNumber(asr, 4, final)
		s := ipcNumber(nil, 1, 1)
		s = ipcBytes(s, 2, []byte(echo))
		s = ipcBytes(s, 7, []byte("11111111-1111-1111-1111-111111111111"))
		s = ipcBytes(s, 15, asr)
		b := ipcBytes(nil, 252, s)
		return ipcNumber(b, 253, 3)
	}
	if _, _, _, ok := earlyNativeRequest(makeFrame(1, "включи песню disease")); !ok {
		t.Fatal("Early final ASR missed")
	}
	for _, frame := range [][]byte{makeFrame(0, "включи песню disease"), makeFrame(1, "другая команда"), {0xff, 0xff, 0xff}} {
		if _, _, _, ok := earlyNativeRequest(frame); ok {
			t.Fatal("Non-final or mismatched native ASR accepted")
		}
	}
}
func TestPairTokensCanBeRevoked(t *testing.T) {
	oldToken, oldClients := token, pairedClients
	defer func() { token = oldToken; pairedClients = oldClients }()
	token = strings.Repeat("a", 48)
	clientToken := strings.Repeat("b", 48)
	digest := sha256.Sum256([]byte(clientToken))
	pairedClients = []pairedClient{{ID: "one", Hash: hex.EncodeToString(digest[:])}}
	if !accessValid(token) || !accessValid(clientToken) || accessValid(strings.Repeat("c", 48)) {
		t.Fatal("Token validation")
	}
	pairedClients = nil
	if accessValid(clientToken) {
		t.Fatal("Revoked client still works")
	}
}
func TestPairProofBindsCertificate(t *testing.T) {
	code := "ABCDEFGHIJKLMNOP"
	if pairingMAC(code, "nonce|cert-a") == pairingMAC(code, "nonce|cert-b") {
		t.Fatal("Pair proof does not bind certificate")
	}
}
func TestScenarioRejectsDuplicatePhrases(t *testing.T) {
	old := voiceCfg
	defer func() { voiceCfg = old }()
	initScenarioHandlers()
	body := `{"provider":"com.spotify.tv.android","rules":[{"id":"r1","name":"One","enabled":true,"phrases":["звёздные часы","звездные часы!"],"steps":[{"api":"action","data":{"action":"volume","value":20}}]}]}`
	w := httptest.NewRecorder()
	scenariosAPI(w, httptest.NewRequest("POST", "/api/scenarios", strings.NewReader(body)))
	if w.Code != 400 {
		t.Fatalf("Duplicate phrases accepted: %d", w.Code)
	}
}
func TestImportedMediaIDsStayInsideLibrary(t *testing.T) {
	for _, bad := range []string{"../file", "/data/secret", strings.Repeat("A", 24), strings.Repeat("f", 26), ""} {
		if isMediaID(bad) {
			t.Fatal(bad)
		}
	}
	if !isMediaID(strings.Repeat("b", 24)) {
		t.Fatal("Valid ID rejected")
	}
}
func TestCacheCleanupDoesNotFollowChildLinks(t *testing.T) {
	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	outside := filepath.Join(root, "outside")
	os.Mkdir(cache, 0700)
	os.Mkdir(outside, 0700)
	file := filepath.Join(outside, "important")
	os.WriteFile(file, []byte("keep"), 0600)
	os.Symlink(outside, filepath.Join(cache, "link"))
	os.WriteFile(filepath.Join(cache, "temporary"), []byte("remove"), 0600)
	if e := cleanCacheFolder(cache); e != nil {
		t.Fatal(e)
	}
	if read(file) != "keep" {
		t.Fatal("Cleanup crossed cache boundary")
	}
}
func TestLampAddressRejectsPublicAndLoopback(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "8.8.8.8", "0.0.0.0", "example.com", "169.254.1.1"} {
		if privateLampHost(host) {
			t.Fatal(host)
		}
	}
}
