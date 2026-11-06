package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestSignedUpdateCannotBeModified(t *testing.T) {
	b, e := os.ReadFile("testdata/channel.json")
	if e != nil {
		t.Fatal(e)
	}
	info, e := signedRelease(b)
	if e != nil || info.Version != "2.3.3" {
		t.Fatal(info, e)
	}
	var envelope map[string]string
	json.Unmarshal(b, &envelope)
	envelope["payload"] = "e30="
	b, _ = json.Marshal(envelope)
	if _, e = signedRelease(b); e == nil {
		t.Fatal("tampered release accepted")
	}
	for _, v := range []string{"../2.5.0", "2.5.0/evil", "2.3.0", "2.4.0"} {
		if newer(v) {
			t.Fatal("unsafe or old version", v)
		}
	}
	if !newer("2.6.1") {
		t.Fatal("update not found")
	}
}
func TestDesktopGatewayRejectsCrossSiteAccess(t *testing.T) {
	a := &app{origin: "http://127.0.0.1:31415", cookie: strings.Repeat("a", 48)}
	send := func(host, origin, cookie string) int {
		r := httptest.NewRequest("GET", "http://127.0.0.1:31415/desktop/state", nil)
		r.Host = host
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "studio-session", Value: cookie})
		}
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		return w.Code
	}
	if send("127.0.0.1:31415", "", "") != 403 {
		t.Fatal("missing session accepted")
	}
	if send("attacker.example", "", a.cookie) != 403 {
		t.Fatal("DNS rebinding accepted")
	}
	if send("127.0.0.1:31415", "https://attacker.example", a.cookie) != 403 {
		t.Fatal("cross-site origin accepted")
	}
	if send("127.0.0.1:31415", a.origin, a.cookie) != 200 {
		t.Fatal("local session rejected")
	}
}
func TestPinnedTLSRefusesDifferentCertificate(t *testing.T) {
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	defer s.Close()
	bad := &http.Client{Transport: transport(strings.Repeat("0", 64), nil)}
	if _, e := bad.Get(s.URL); e == nil {
		t.Fatal("mismatching certificate accepted")
	}
	sum := sha256.Sum256(s.Certificate().Raw)
	good := &http.Client{Transport: transport(fmt.Sprintf("%x", sum), nil)}
	res, e := good.Get(s.URL)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
}
func TestDPAPIStorage(t *testing.T) {
	plain := []byte("private-station-test")
	cipher, e := seal(plain, false)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(cipher), string(plain)) {
		t.Fatal("plaintext storage")
	}
	decoded, e := seal(cipher, true)
	if e != nil || string(decoded) != string(plain) {
		t.Fatal("DPAPI roundtrip failed", e)
	}
}
