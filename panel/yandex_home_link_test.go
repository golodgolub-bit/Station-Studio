package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func homeLinkFixture(t *testing.T) (*int, *int, *string) {
	t.Helper()
	oldBase, oldPassport, oldWeb, oldPage, oldLogin := base, homePassport, homeWebOrigin, homePageOrigin, homeLogin
	base = t.TempDir()
	homeLogin = nil
	sessions, actions := 0, 0
	actionResult := "DONE"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/pwl-yandex":
			http.SetCookie(w, &http.Cookie{Name: "prelogin", Value: "random-prelogin", Path: "/"})
			w.Write([]byte(`__CSRF__ = "fixture-csrf"`))
		case "/pwl-yandex/api/passport/auth/password/submit":
			if r.Header.Get("X-CSRF-Token") != "fixture-csrf" {
				t.Error("missing CSRF")
			}
			var data map[string]any
			json.NewDecoder(r.Body).Decode(&data)
			if data["client_id"] != nil || data["client_secret"] != nil {
				t.Error("borrowed OAuth identity")
			}
			w.Write([]byte(`{"track_id":"random-track"}`))
		case "/pwl-yandex/api/passport/auth/magic/code":
			r.ParseForm()
			if r.Form.Get("magic_track_id") != "random-track" {
				t.Error("wrong track")
			}
			w.Write([]byte(`{"link":"https://passport.yandex.ru/auth/magic/?track_id=fixture"}`))
		case "/pwl-yandex/api/passport/auth/magic/code/status":
			w.Write([]byte(`{"state":"otp_auth_finished","trackId":"approved-track"}`))
		case "/pwl-yandex/api/passport/sessions/get_session":
			r.ParseForm()
			if r.Form.Get("track_id") != "approved-track" {
				t.Error("wrong confirmation")
			}
			sessions++
			http.SetCookie(w, &http.Cookie{Name: "Session_id", Value: "fake-private-session", HttpOnly: true, Path: "/"})
			w.Write([]byte(`{"status":"ok"}`))
		case "/m/v3/user/devices":
			c, e := r.Cookie("Session_id")
			if e != nil || c.Value != "fake-private-session" {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte(`{"status":"ok","households":[{"id":"house","name":"Home","rooms":[{"id":"room-1","name":"Room","items":[{"id":"lamp-1"}]}],"all":[{"id":"lamp-1","item_type":"device","name":"Lamp","type":"devices.types.light","capabilities":[]},{"id":"group-1","item_type":"group","name":"Ignore group"}]}]}`))
		case "/m/user/devices/lamp-1":
			w.Write([]byte(`{"status":"ok","id":"lamp-1","name":"Lamp","capabilities":[{"type":"devices.capabilities.on_off","state":{"instance":"on","value":false}},{"type":"devices.capabilities.color_setting","parameters":{"color_model":"hsv","instance":"color","temperature_k":{"min":2700,"max":6500}}}]}`))
		case "/iot":
			w.Write([]byte(`{"csrfToken2":"fixture-device-csrf"}`))
		case "/m/v3/user/custom/group/color/apply":
			if r.Header.Get("X-CSRF-Token") != "fixture-device-csrf" {
				t.Error("missing color CSRF")
			}
			var payload map[string]any
			json.NewDecoder(r.Body).Decode(&payload)
			if payload["hsv"] == nil {
				t.Error("missing HSV")
			}
			actions++
			if actionResult == "DONE" {
				w.Write([]byte(`{"status":"ok"}`))
			} else {
				w.Write([]byte(`{"status":"error"}`))
			}
		case "/m/user/devices/lamp-1/actions":
			if r.Header.Get("X-CSRF-Token") != "fixture-device-csrf" {
				t.Error("missing action CSRF")
			}
			actions++
			w.Write([]byte(`{"status":"ok","devices":[{"id":"lamp-1","capabilities":[{"state":{"action_result":{"status":"` + actionResult + `","error_code":"TEST_FAILURE"}}}]}]}`))
		default:
			t.Error("unexpected endpoint", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	homePassport, homeWebOrigin, homePageOrigin = server.URL, server.URL, server.URL
	t.Cleanup(func() {
		server.Close()
		base, homePassport, homeWebOrigin, homePageOrigin, homeLogin = oldBase, oldPassport, oldWeb, oldPage, oldLogin
	})
	return &sessions, &actions, &actionResult
}
func callHomeLink(method string) *httptest.ResponseRecorder {
	out := httptest.NewRecorder()
	homeLinkAPI(out, httptest.NewRequest(method, "/api/home/link", nil))
	return out
}
func TestHomeLinkApprovedSessionAndNoCredentialDisclosure(t *testing.T) {
	sessions, actions, result := homeLinkFixture(t)
	out := callHomeLink("POST")
	if out.Code != 200 || homeConnected() {
		t.Fatal(out.Body.String())
	}
	out = callHomeLink("GET")
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"connected"`) || !homeConnected() || *sessions != 1 {
		t.Fatal(out.Body.String())
	}
	stat, e := os.Stat(filepath.Join(base, "home-web-session"))
	if e != nil || stat.Mode().Perm() != 0600 {
		t.Fatal("session storage is not private")
	}
	out = callHomeLink("GET")
	if *sessions != 1 || !strings.Contains(out.Body.String(), `"connected"`) {
		t.Fatal("session approval replayed")
	}
	out = httptest.NewRecorder()
	homeAPI(out, httptest.NewRequest("GET", "/api/home", nil))
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"room-1"`) || strings.Contains(out.Body.String(), "fake-private-session") || strings.Contains(out.Body.String(), "group-1") {
		t.Fatal(out.Body.String())
	}
	out = httptest.NewRecorder()
	homeAccountAPI(out, httptest.NewRequest("GET", "/api/home/account", nil))
	if strings.Contains(out.Body.String(), "fake-private-session") || !strings.Contains(out.Body.String(), "web_session") {
		t.Fatal("wrong credential metadata")
	}
	var q homeAction
	json.Unmarshal([]byte(`{"id":"lamp-1","actions":[{"type":"devices.capabilities.on_off","state":{"instance":"on","value":true}}]}`), &q)
	if _, e = homeExecute(q); e != nil || *actions != 1 {
		t.Fatal(e, *actions)
	}
	*result = "ERROR"
	if _, e = homeExecute(q); e == nil || *actions != 2 {
		t.Fatal("failure ignored or command repeated")
	}
	out = httptest.NewRecorder()
	homeAccountAPI(out, httptest.NewRequest("DELETE", "/api/home/account", nil))
	if out.Code != 200 || homeConnected() {
		t.Fatal("local session survived unlink")
	}
}
func TestHomeLinkExpiryCancelAndUnconfirmedSession(t *testing.T) {
	sessions, _, _ := homeLinkFixture(t)
	out := callHomeLink("POST")
	if out.Code != 200 {
		t.Fatal(out.Body.String())
	}
	homeLogin.LastPoll = time.Now()
	out = callHomeLink("GET")
	if homeConnected() || *sessions != 0 || !strings.Contains(out.Body.String(), "waiting") {
		t.Fatal("unconfirmed login accepted")
	}
	homeLogin.Expires = time.Now().Add(-6 * time.Minute)
	out = callHomeLink("GET")
	if !strings.Contains(out.Body.String(), "expired") || homeLogin != nil {
		t.Fatal("expired link survived")
	}
	callHomeLink("POST")
	callHomeLink("DELETE")
	if homeLogin != nil || homeConnected() {
		t.Fatal("cancel failed")
	}
}
func TestHomeLinkRenewConsumesBodyAndBuildsLocalQR(t *testing.T) {
	homeLinkFixture(t)
	first := callHomeLink("POST")
	if first.Code != 200 {
		t.Fatal(first.Body.String())
	}
	old := homeLogin
	request := httptest.NewRequest("POST", "/", strings.NewReader(`{"renew":true}`))
	out := httptest.NewRecorder()
	homeLinkAPI(out, request)
	if out.Code != 200 || homeLogin == old {
		t.Fatal("stale link reused")
	}
	if homeLogin.Expires.After(time.Now().Add(301 * time.Second)) {
		t.Fatal("overstated link lifetime")
	}
	var result struct {
		QR string `json:"qr"`
	}
	json.Unmarshal(out.Body.Bytes(), &result)
	data, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(result.QR, "data:image/png;base64,"))
	if e != nil {
		t.Fatal(e)
	}
	config, e := png.DecodeConfig(bytes.NewReader(data))
	if e != nil || config.Width != 384 || config.Height != 384 {
		t.Fatal("invalid local QR", e)
	}
	request = httptest.NewRequest("POST", "/", strings.NewReader(strings.Repeat("x", 1025)))
	out = httptest.NewRecorder()
	homeLinkAPI(out, request)
	if out.Code != 400 {
		t.Fatal("unbounded login body")
	}
}
func TestHomeLinkChecksApprovedSessionAfterUIDeadline(t *testing.T) {
	sessions, _, _ := homeLinkFixture(t)
	callHomeLink("POST")
	homeLogin.Expires = time.Now().Add(-time.Minute)
	out := callHomeLink("GET")
	if out.Code != 200 || *sessions != 1 || !homeConnected() {
		t.Fatal("approved session discarded", out.Body.String())
	}
}
func TestHomeWebColorUsesDedicatedEndpointWithoutClaimingDeviceReceipt(t *testing.T) {
	_, calls, result := homeLinkFixture(t)
	callHomeLink("POST")
	callHomeLink("GET")
	var q homeAction
	json.Unmarshal([]byte(`{"id":"lamp-1","actions":[{"type":"devices.capabilities.color_setting","state":{"instance":"rgb","value":16711680}}]}`), &q)
	raw, e := homeExecute(q)
	if e != nil || *calls != 1 || !strings.Contains(string(raw), `"confirmation":"cloud"`) || strings.Contains(string(raw), "DONE") {
		t.Fatal(e, string(raw), *calls)
	}
	*result = "ERROR"
	if _, e = homeExecute(q); e == nil || *calls != 2 {
		t.Fatal("color failure ignored or command repeated")
	}
}
func TestHomeWebColorValueValidation(t *testing.T) {
	for _, value := range []string{`{"instance":"rgb","value":16711680}`, `{"instance":"hsv","value":{"h":120,"s":100,"v":80}}`, `{"instance":"temperature_k","value":4000}`} {
		var q homeAction
		json.Unmarshal([]byte(`{"id":"lamp-1","actions":[{"type":"devices.capabilities.color_setting","state":`+value+`}]}`), &q)
		payload, e := homeColorPayload(q)
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(value, `"rgb"`) {
			v := payload["hsv"].(map[string]any)
			if v["h"] != float64(0) || v["s"] != float64(100) || v["v"] != float64(100) {
				t.Fatal("RGB conversion", v)
			}
		}
	}
	for _, value := range []string{`{"instance":"rgb","value":16777216}`, `{"instance":"hsv","value":{"h":120,"s":101,"v":80}}`, `{"instance":"temperature_k","value":0}`} {
		var q homeAction
		json.Unmarshal([]byte(`{"id":"lamp-1","actions":[{"type":"devices.capabilities.color_setting","state":`+value+`}]}`), &q)
		if _, e := homeColorPayload(q); e == nil {
			t.Fatal("invalid color accepted")
		}
	}
}
func TestHomeWebRoomsMapMembershipWithoutDuplicatingDeviceData(t *testing.T) {
	homeLinkFixture(t)
	callHomeLink("POST")
	callHomeLink("GET")
	var info homeInfo
	if e := homeCall("", "GET", "/v1.0/user/info", nil, &info); e != nil {
		t.Fatal(e)
	}
	if len(info.Devices) != 1 || info.Devices[0].Room != "room-1" || len(info.Rooms) != 1 {
		t.Fatal("room membership lost")
	}
	if strings.Contains(string(info.Rooms[0]), "items") {
		t.Fatal("duplicated internal room data")
	}
}
func TestHomeCookiePersistencePreservesScopeAndDeletion(t *testing.T) {
	j := newHomeJar(nil)
	u, _ := url.Parse(homePassport + "/login")
	j.SetCookies(u, []*http.Cookie{{Name: "private", Value: "host-only", Path: "/", Secure: true}, {Name: "shared", Value: "domain", Domain: ".yandex.ru", Path: "/", Secure: true}})
	j = newHomeJar(j.Entries)
	iot, _ := url.Parse(homeWebOrigin + "/")
	for _, c := range j.Cookies(iot) {
		if c.Name == "private" {
			t.Fatal("host-only cookie leaked")
		}
	}
	cs := j.Cookies(iot)
	if len(cs) != 1 || cs[0].Name != "shared" {
		t.Fatal("domain cookie not restored")
	}
	j.SetCookies(u, []*http.Cookie{{Name: "shared", Domain: ".yandex.ru", Path: "/", MaxAge: -1}})
	j = newHomeJar(j.Entries)
	if len(j.Cookies(iot)) != 0 {
		t.Fatal("deleted cookie restored")
	}
	for _, target := range []string{"http://passport.yandex.ru/", "https://passport.yandex.ru.evil.test/", "https://user:pass@passport.yandex.ru/", "https://passport.yandex.ru:444/"} {
		u, _ := url.Parse(target)
		if homeAllowedURL(u) {
			t.Fatal("unsafe origin", target)
		}
	}
}
