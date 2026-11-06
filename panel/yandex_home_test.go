package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func homeFixture(t *testing.T, actionStatus string) (*int, string) {
	t.Helper()
	oldBase, oldOrigin := base, homeOrigin
	base = t.TempDir()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer test-oauth-secret-for-home" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/v1.0/user/info":
			w.Write([]byte(`{"status":"ok","devices":[],"rooms":[]}`))
		case "/v1.0/devices/lamp-1":
			w.Write([]byte(`{"status":"ok","id":"lamp-1","name":"Lamp","capabilities":[{"type":"devices.capabilities.on_off","state":{"instance":"on","value":false}}]}`))
		case "/v1.0/devices/actions":
			calls++
			w.Write([]byte(`{"status":"ok","devices":[{"capabilities":[{"state":{"action_result":{"status":"` + actionStatus + `","error_code":"TEST_FAILURE"}}}]}]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	homeOrigin = server.URL
	t.Cleanup(func() { server.Close(); base = oldBase; homeOrigin = oldOrigin })
	return &calls, "test-oauth-secret-for-home"
}
func TestHomeCredentialRequiresTLSAndNeverReturnsToken(t *testing.T) {
	_, key := homeFixture(t, "DONE")
	body, _ := json.Marshal(map[string]string{"token": key})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	out := httptest.NewRecorder()
	homeAccountAPI(out, req)
	if out.Code != 400 || homeToken() != "" {
		t.Fatal("accepted credential over HTTP")
	}
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.TLS = &tls.ConnectionState{}
	out = httptest.NewRecorder()
	homeAccountAPI(out, req)
	if out.Code != 200 || homeToken() != key {
		t.Fatal(out.Body.String())
	}
	info, e := os.Stat(filepath.Join(base, "home-oauth"))
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("private storage required")
	}
	out = httptest.NewRecorder()
	homeAccountAPI(out, httptest.NewRequest("GET", "/", nil))
	if strings.Contains(out.Body.String(), key) || !strings.Contains(out.Body.String(), `"connected":true`) {
		t.Fatal("credential disclosure")
	}
	out = httptest.NewRecorder()
	homeAccountAPI(out, httptest.NewRequest("DELETE", "/", nil))
	if out.Code != 200 || homeToken() != "" {
		t.Fatal("unlink failed")
	}
}
func TestHomeActionsUseAdvertisedCapabilities(t *testing.T) {
	calls, key := homeFixture(t, "DONE")
	_ = homeSave("home-oauth", []byte(key))
	var q homeAction
	_ = json.Unmarshal([]byte(`{"id":"lamp-1","actions":[{"type":"devices.capabilities.on_off","state":{"instance":"on","value":true}}]}`), &q)
	if _, e := homeExecute(q); e != nil || *calls != 1 {
		t.Fatal(e, *calls)
	}
	q.Actions[0].Type = "devices.capabilities.range"
	if _, e := homeExecute(q); e == nil || *calls != 1 {
		t.Fatal("unadvertised action sent")
	}
	q.ID = "../secret"
	if _, e := homeExecute(q); e == nil || *calls != 1 {
		t.Fatal("unsafe id")
	}
}
func TestHomePartialFailureIsNotSuccessOrRetried(t *testing.T) {
	calls, key := homeFixture(t, "ERROR")
	_ = homeSave("home-oauth", []byte(key))
	var q homeAction
	_ = json.Unmarshal([]byte(`{"id":"lamp-1","actions":[{"type":"devices.capabilities.on_off","state":{"instance":"on","value":true}}]}`), &q)
	if _, e := homeExecute(q); e == nil || *calls != 1 {
		t.Fatal("failure ignored or action repeated", *calls)
	}
}
func TestHomePluginGrantBoundToManifest(t *testing.T) {
	old := base
	base = t.TempDir()
	defer func() { base = old }()
	m := PluginManifest{Name: "test/lamp", Version: "1.0.0"}
	p := pluginHomePath(m.Name)
	_ = os.MkdirAll(filepath.Dir(p), 0700)
	b, _ := json.Marshal(pluginHomeGrant{Digest: pluginManifestDigest(m), Read: true, Control: true})
	_ = os.WriteFile(p, b, 0600)
	if !pluginHomeAllowed(m).Control {
		t.Fatal("grant missing")
	}
	m.Version = "1.0.1"
	if pluginHomeAllowed(m).Read {
		t.Fatal("grant survived version change")
	}
}

func TestHomeUnlinkClearsOnlyHomeGrants(t *testing.T) {
	_, key := homeFixture(t, "DONE")
	_ = homeSave("home-oauth", []byte(key))
	directory := filepath.Dir(pluginHomePath("test/lamp"))
	_ = os.MkdirAll(directory, 0700)
	grant := pluginHomePath("test/lamp")
	rootGrant := pluginTrustPath("test/lamp")
	_ = os.WriteFile(grant, []byte("home access"), 0600)
	_ = os.WriteFile(rootGrant, []byte("root consent"), 0600)
	out := httptest.NewRecorder()
	homeAccountAPI(out, httptest.NewRequest("DELETE", "/", nil))
	if out.Code != 200 {
		t.Fatal(out.Body.String())
	}
	if _, e := os.Stat(grant); !os.IsNotExist(e) {
		t.Fatal("home grant survived unlink")
	}
	if _, e := os.Stat(rootGrant); e != nil {
		t.Fatal("unrelated root grant removed")
	}
}
