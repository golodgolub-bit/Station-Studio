package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const communityMirror = "https://github.com/golodgolub-bit/Station-Studio/releases/download/"
const communityOrigin = "https://station-studio-community.shaaaape.chatgpt.site"

var marketUUID = regexp.MustCompile(`^[a-f0-9-]{36}$`)

func marketClient() *http.Client {
	return &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if r.URL.Scheme != "https" || (r.URL.Host != "github.com" && !strings.HasSuffix(r.URL.Hostname(), ".githubusercontent.com")) || len(via) > 2 {
			return fmt.Errorf("Каталог перенаправил загрузку на другой адрес")
		}
		return nil
	}}
}
func marketGet(path string) (*http.Response, error) {
	req, e := http.NewRequest("GET", communityMirror+path, nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("User-Agent", "StationStudio/2.4 (+https://github.com/golodgolub-bit/Station-Studio)")
	req.Header.Set("Accept", "application/json, application/octet-stream")
	return marketClient().Do(req)
}

type marketIndex struct {
	Items    []json.RawMessage `json:"items"`
	SyncedAt string            `json:"syncedAt"`
}

func readMarketIndex(fresh bool) (marketIndex, error) {
	var index marketIndex
	cache := filepath.Join(base, "community-catalog.json")
	if st, e := os.Stat(cache); e == nil && !fresh && time.Since(st.ModTime()) < 3*time.Minute {
		if raw, e := os.ReadFile(cache); e == nil && json.Unmarshal(raw, &index) == nil {
			return index, nil
		}
	}
	res, e := marketGet("community-index/catalog.json")
	if e != nil {
		return index, e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return index, fmt.Errorf("Каталог загрузок пока недоступен (HTTP %d). Первая синхронизация может занять до 15 минут", res.StatusCode)
	}
	raw, e := io.ReadAll(io.LimitReader(res.Body, (16<<20)+1))
	if e != nil || len(raw) > 16<<20 || json.Unmarshal(raw, &index) != nil {
		return index, fmt.Errorf("Не удалось проверить каталог")
	}
	f, e := os.CreateTemp(base, ".catalog-*")
	if e == nil {
		defer os.Remove(f.Name())
		if _, e = f.Write(raw); e == nil {
			e = f.Sync()
		}
		f.Close()
		if e == nil {
			os.Rename(f.Name(), cache)
		}
	}
	return index, nil
}
func marketplaceAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		page := 0
		if p := r.URL.Query().Get("page"); p != "" {
			var e error
			page, e = strconv.Atoi(p)
			if e != nil || page < 0 || page > 10000 {
				fail(w, "Неверная страница каталога", 400)
				return
			}
		}
		query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
		if len(query) > 300 {
			fail(w, "Слишком длинный поиск", 400)
			return
		}
		index, e := readMarketIndex(r.URL.Query().Get("fresh") == "1")
		if e != nil {
			fail(w, e.Error(), 502)
			return
		}
		items := index.Items
		if query != "" {
			items = []json.RawMessage{}
			for _, raw := range index.Items {
				var p struct{ Title, Author, Description string }
				if json.Unmarshal(raw, &p) == nil && strings.Contains(strings.ToLower(p.Title+" "+p.Author+" "+p.Description), query) {
					items = append(items, raw)
				}
			}
		}
		start := page * 100
		if start > len(items) {
			start = len(items)
		}
		end := start + 100
		if end > len(items) {
			end = len(items)
		}
		var next any
		if end < len(items) {
			next = page + 1
		}
		response(w, map[string]any{"items": items[start:end], "next": next, "syncedAt": index.SyncedAt, "intervalMinutes": 15, "total": len(items), "publishURL": communityOrigin})
		return
	}

	if r.Method != "POST" {
		fail(w, "Нужен GET или POST", 405)
		return
	}
	var q struct {
		ID  string `json:"id"`
		SHA string `json:"sha256"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if json.NewDecoder(r.Body).Decode(&q) != nil || !marketUUID.MatchString(q.ID) || !pluginID.MatchString(q.SHA) {
		fail(w, "Некорректная публикация", 400)
		return
	}
	pluginMu.Lock()
	defer pluginMu.Unlock()
	res, e := marketGet("community-" + q.ID[:2] + "/" + url.PathEscape(q.ID+"-"+q.SHA+".yampkg"))
	if e != nil {
		fail(w, e.Error(), 502)
		return
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		fail(w, "Публикация обновлена или удалена. Обновите каталог", 409)
		return
	}
	if e = os.MkdirAll(pluginDir(), 0700); e != nil {
		fail(w, e.Error(), 500)
		return
	}
	f, e := os.CreateTemp(pluginDir(), ".market-*")
	if e != nil {
		fail(w, e.Error(), 500)
		return
	}
	defer os.Remove(f.Name())
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(res.Body, (32<<20)+1))
	syncErr := f.Sync()
	f.Close()
	if e != nil || syncErr != nil || n > 32<<20 || hex.EncodeToString(h.Sum(nil)) != q.SHA {
		fail(w, "Пакет не прошёл проверку размера и SHA-256", 409)
		return
	}
	input, e := os.Open(f.Name())
	if e != nil {
		fail(w, e.Error(), 500)
		return
	}
	record, e := inspectPlugin(input)
	input.Close()
	if e != nil {
		fail(w, e.Error(), 409)
		return
	}
	record.ID = q.SHA
	record.Size = n
	record.Filename = q.ID + ".yampkg"
	record.Uploaded = time.Now().Unix()
	if e = os.Rename(f.Name(), filepath.Join(pluginDir(), q.SHA+".yampkg")); e != nil {
		fail(w, e.Error(), 500)
		return
	}
	raw, _ := json.Marshal(record)
	if e = os.WriteFile(filepath.Join(pluginDir(), q.SHA+".json"), raw, 0600); e != nil {
		fail(w, e.Error(), 500)
		return
	}
	response(w, record)
}
