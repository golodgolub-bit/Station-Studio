package main

import (
	"bytes"
	"context"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/gif"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Remote artwork is retrieved without Yandex cookies or station credentials.
// Same-origin authenticated blobs keep the panel CSP closed to remote images.
func homeArtURL(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Fragment != "" || len(raw) > 2048 {
		return false
	}
	h := strings.ToLower(u.Hostname())
	for _, s := range []string{"avatars.mds.yandex.net", "avatars.mdst.yandex.net", "yastatic.net", "storage.yandexcloud.net"} {
		if h == s || strings.HasSuffix(h, "."+s) {
			return true
		}
	}
	return false
}

type homeArtEntry struct {
	data  []byte
	mime  string
	until time.Time
}

var homeArtMu sync.Mutex
var homeArtCache = map[string]homeArtEntry{}

func homeArtAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		fail(w, "Нужен GET", 405)
		return
	}
	raw := r.URL.Query().Get("url")
	if !homeArtURL(raw) {
		fail(w, "Адрес изображения устройства не поддерживается", 400)
		return
	}
	homeArtMu.Lock()
	cached, ok := homeArtCache[raw]
	homeArtMu.Unlock()
	if ok && time.Now().Before(cached.until) {
		w.Header().Set("Content-Type", cached.mime)
		w.Header().Set("Cache-Control", "private,max-age=300")
		w.Write(cached.data)
		return
	}
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		h, p, e := net.SplitHostPort(address)
		if e != nil {
			return nil, e
		}
		ips, e := net.DefaultResolver.LookupIPAddr(ctx, h)
		if e != nil {
			return nil, e
		}
		for _, ip := range ips {
			if ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() || ip.IP.IsLinkLocalMulticast() || ip.IP.IsUnspecified() || ip.IP.IsMulticast() {
				return nil, fmt.Errorf("Нелокальный адрес CDN не подтверждён")
			}
		}
		for _, ip := range ips {
			c, e := (&net.Dialer{Timeout: 4 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), p))
			if e == nil {
				return c, nil
			}
		}
		return nil, fmt.Errorf("CDN не отвечает")
	}
	tr := &http.Transport{DialContext: dial}
	defer tr.CloseIdleConnections()
	client := http.Client{Transport: tr, Timeout: 9 * time.Second, CheckRedirect: func(q *http.Request, via []*http.Request) error {
		if len(via) > 3 || !homeArtURL(q.URL.String()) {
			return fmt.Errorf("Переход изображения запрещён")
		}
		return nil
	}}
	req, e := http.NewRequestWithContext(r.Context(), "GET", raw, nil)
	if e != nil {
		fail(w, "Неверный адрес", 400)
		return
	}
	req.Header.Set("User-Agent", "AliceKit-Art/2.9")
	res, e := client.Do(req)
	if e != nil {
		fail(w, "Не удалось загрузить изображение устройства", 502)
		return
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		fail(w, "Сервис изображений ответил ошибкой", 502)
		return
	}
	data, e := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if e != nil || len(data) > 2<<20 {
		fail(w, "Изображение слишком большое", 413)
		return
	}
	cfg, _, e := image.DecodeConfig(bytes.NewReader(data))
	mime := http.DetectContentType(data)
	if e != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 || cfg.Width*cfg.Height > 12_000_000 || !strings.HasPrefix(mime, "image/") {
		fail(w, "Формат изображения не поддерживается", 415)
		return
	}
	homeArtMu.Lock()
	if len(homeArtCache) >= 12 {
		for key := range homeArtCache {
			delete(homeArtCache, key)
			break
		}
	}
	homeArtCache[raw] = homeArtEntry{data, mime, time.Now().Add(30 * time.Minute)}
	homeArtMu.Unlock()
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private,max-age=300")
	w.Write(data)
}
