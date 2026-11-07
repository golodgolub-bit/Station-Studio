package main

import (
	"bytes"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type coverInfo struct {
	Available bool     `json:"available"`
	Title     string   `json:"title"`
	Artist    string   `json:"artist"`
	Provider  string   `json:"provider"`
	Palette   []string `json:"palette"`
	Source    string   `json:"source"`
	Error     string   `json:"error"`
	uri       string
}

var coverMu sync.Mutex
var coverRefreshMu sync.Mutex
var currentCover coverInfo
var coverUpdated time.Time

func coverSnapshot() coverInfo {
	coverMu.Lock()
	defer coverMu.Unlock()
	c := currentCover
	c.Palette = append([]string(nil), c.Palette...)
	return c
}
func publicCoverURL(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, suffix := range []string{"scdn.co", "spotifycdn.com", "spotifycdn.net", "ytimg.com", "googleusercontent.com", "ggpht.com", "mzstatic.com", "tidal.com", "yandex.net"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}
func fetchCover(raw string) ([]string, error) {
	if !publicCoverURL(raw) {
		return nil, fmt.Errorf("Плеер не передал доступную обложку")
	}
	client := http.Client{Timeout: 3 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 3 || !publicCoverURL(r.URL.String()) {
			return fmt.Errorf("Недопустимый адрес обложки")
		}
		return nil
	}}
	r, e := client.Get(raw)
	if e != nil {
		return nil, e
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, fmt.Errorf("Сервис обложек временно недоступен")
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, 2<<20+1))
	if e != nil || len(b) > 2<<20 {
		return nil, fmt.Errorf("Обложка слишком большая")
	}
	config, _, e := image.DecodeConfig(bytes.NewReader(b))
	if e != nil || config.Width < 1 || config.Height < 1 || config.Width > 2048 || config.Height > 2048 {
		return nil, fmt.Errorf("Формат обложки не поддерживается")
	}
	picture, _, e := image.Decode(bytes.NewReader(b))
	if e != nil {
		return nil, e
	}
	palette := imagePalette(picture)
	var out bytes.Buffer
	if e = jpeg.Encode(&out, picture, &jpeg.Options{Quality: 85}); e != nil {
		return nil, e
	}
	temp := filepath.Join(base, "album.jpg.tmp")
	if e = os.WriteFile(temp, out.Bytes(), 0600); e != nil {
		return nil, e
	}
	if e = os.Rename(temp, filepath.Join(base, "album.jpg")); e != nil {
		return nil, e
	}
	return palette, nil
}
func imagePalette(picture image.Image) []string {
	type bin struct{ count, r, g, b int }
	hist := map[int]*bin{}
	bounds := picture.Bounds()
	for y := 0; y < 96; y++ {
		for x := 0; x < 96; x++ {
			r, g, b, a := picture.At(bounds.Min.X+x*bounds.Dx()/96, bounds.Min.Y+y*bounds.Dy()/96).RGBA()
			if a < 32768 {
				continue
			}
			ri, gi, bi := int(r>>8), int(g>>8), int(b>>8)
			key := (ri>>3)<<10 | (gi>>3)<<5 | (bi >> 3)
			v := hist[key]
			if v == nil {
				v = &bin{}
				hist[key] = v
			}
			v.count++
			v.r += ri
			v.g += gi
			v.b += bi
		}
	}
	keys := []int{}
	for key := range hist {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if hist[keys[i]].count == hist[keys[j]].count {
			return keys[i] < keys[j]
		}
		return hist[keys[i]].count > hist[keys[j]].count
	})
	result := []string{}
	centers := [][3]int{}
	for _, key := range keys {
		v := hist[key]
		c := [3]int{v.r / v.count, v.g / v.count, v.b / v.count}
		distinct := true
		for _, old := range centers {
			d := (c[0]-old[0])*(c[0]-old[0]) + (c[1]-old[1])*(c[1]-old[1]) + (c[2]-old[2])*(c[2]-old[2])
			if d < 2500 {
				distinct = false
			}
		}
		if distinct {
			centers = append(centers, c)
			result = append(result, fmt.Sprintf("#%02x%02x%02x", c[0], c[1], c[2]))
			if len(result) == 4 {
				break
			}
		}
	}
	if len(result) == 1 {
		result = append(result, result[0])
	}
	return result
}
func refreshCover() (coverInfo, error) {
	coverRefreshMu.Lock()
	defer coverRefreshMu.Unlock()
	if time.Since(coverUpdated) < 900*time.Millisecond {
		return coverSnapshot(), nil
	}
	defer func() { coverUpdated = time.Now() }()
	pkg := selectedProvider()
	m, e := helperMedia("metadata", "", pkg)
	c := coverInfo{Provider: pkg}
	if e != nil {
		c.Error = "Откройте выбранный плеер на станции"
	} else {
		c.Title, _ = m["title"].(string)
		c.Artist, _ = m["artist"].(string)
		c.Source, _ = m["coverSource"].(string)
		if values, ok := m["palette"].([]any); ok {
			for _, v := range values {
				if color, ok := v.(string); ok {
					c.Palette = append(c.Palette, color)
				}
			}
		}
		if len(c.Palette) < 2 {
			if uri, ok := m["coverUri"].(string); ok && uri != "" {
				c.uri = uri
				old := coverSnapshot()
				if old.Available && old.uri == uri && old.Provider == pkg && old.Title == c.Title && old.Artist == c.Artist && fileExists(filepath.Join(base, "album.jpg")) {
					c.Palette = old.Palette
					c.Source = old.Source
				} else {
					c.Palette, e = fetchCover(uri)
					if e == nil {
						c.Source = "cover-url"
					}
				}
			}
		}
		c.Available = len(c.Palette) >= 2 && fileExists(filepath.Join(base, "album.jpg"))
		if !c.Available {
			c.Error = "Плеер пока не передал обложку этого трека"
		}
	}
	coverMu.Lock()
	currentCover = c
	coverMu.Unlock()
	return c, e
}
func coverAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		fail(w, "Нужен GET", 405)
		return
	}
	c, _ := refreshCover()
	response(w, c)
}
