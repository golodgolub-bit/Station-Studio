package main

import (
	"net/http/httptest"
	"testing"
)

func TestHomeArtworkTrustBoundary(t *testing.T) {
	for _, u := range []string{"http://avatars.mds.yandex.net/x", "https://127.0.0.1/x", "https://avatars.mds.yandex.net.evil.example/x", "https://user:pass@avatars.mds.yandex.net/x", "https://avatars.mds.yandex.net:8443/x", "file:///data/local/tmp/key"} {
		if homeArtURL(u) {
			t.Fatalf("untrusted source accepted: %s", u)
		}
		r := httptest.NewRequest("GET", "/api/home/art?url="+u, nil)
		w := httptest.NewRecorder()
		homeArtAPI(w, r)
		if w.Code != 400 {
			t.Fatalf("invalid source reached network: %d", w.Code)
		}
	}
	if !homeArtURL("https://avatars.mds.yandex.net/get-iot/example/orig") {
		t.Fatal("real Yandex CDN rejected")
	}
}
