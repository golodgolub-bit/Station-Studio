package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestCommunityCatalogSearchAndPaginationFromCache(t *testing.T) {
	old := base
	base = t.TempDir()
	defer func() { base = old }()
	index := marketIndex{SyncedAt: "2026-11-06T00:00:00Z", Items: []json.RawMessage{}}
	for i := 0; i < 205; i++ {
		index.Items = append(index.Items, json.RawMessage(fmt.Sprintf(`{"title":"Aurora %d","author":"Creator","description":"Свет"}`, i)))
	}
	raw, _ := json.Marshal(index)
	if e := os.WriteFile(filepath.Join(base, "community-catalog.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	for _, q := range []struct {
		Path         string
		Count, Total int
	}{
		{"?page=0", 100, 205}, {"?page=2", 5, 205}, {"?q=" + url.QueryEscape("СВЕТ"), 100, 205}, {"?q=missing", 0, 0},
	} {
		w := httptest.NewRecorder()
		marketplaceAPI(w, httptest.NewRequest("GET", "/api/plugins/market"+q.Path, nil))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var j struct {
			Items []json.RawMessage
			Total int
		}
		if e := json.Unmarshal(w.Body.Bytes(), &j); e != nil || len(j.Items) != q.Count || j.Total != q.Total {
			t.Fatal(q, j, e)
		}
	}
	w := httptest.NewRecorder()
	marketplaceAPI(w, httptest.NewRequest("GET", "/api/plugins/market?page=-1", nil))
	if w.Code != 400 {
		t.Fatal("negative page accepted")
	}
}
func TestCommunityDownloadRedirectsStayOnReleaseHosts(t *testing.T) {
	c := marketClient()
	for _, s := range []string{"http://github.com/a", "https://evil.example/a", "https://github.com.evil.example/a"} {
		u, _ := url.Parse(s)
		if c.CheckRedirect(&http.Request{URL: u}, nil) == nil {
			t.Fatal("untrusted redirect accepted", s)
		}
	}
	for _, s := range []string{"https://github.com/a", "https://release-assets.githubusercontent.com/a"} {
		u, _ := url.Parse(s)
		if e := c.CheckRedirect(&http.Request{URL: u}, nil); e != nil {
			t.Fatal(e)
		}
	}
}
