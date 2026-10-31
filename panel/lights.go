package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type lamp struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Host string `json:"host"`
}
type lampShow struct {
	Action      string   `json:"action"`
	IDs         []string `json:"ids"`
	Mode        string   `json:"mode"`
	Palette     []string `json:"palette"`
	Min         int      `json:"min"`
	Max         int      `json:"max"`
	Gain        float64  `json:"gain"`
	Interval    int      `json:"interval"`
	Duration    int      `json:"duration"`
	ColorSource string   `json:"colorSource"`
	Motion      string   `json:"motion"`
}

var lampsMu sync.Mutex
var lampsControl sync.Mutex
var lampsCancel context.CancelFunc
var lampsDone chan struct{}
var lampsError string
var lampList = []lamp{}
var lampsConfig lampShow

func privateLampHost(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil || !ip.IsPrivate() {
		return false
	}
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		addresses, _ := iface.Addrs()
		for _, a := range addresses {
			if n, _, e := net.ParseCIDR(a.String()); e == nil && n.Equal(ip) {
				return false
			}
		}
	}
	return true
}

var lampHTTP = &http.Client{Timeout: 1500 * time.Millisecond, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}

func wledRequest(l lamp, value any) (json.RawMessage, error) {
	method := "GET"
	var body io.Reader
	if value != nil {
		b, _ := json.Marshal(value)
		body = bytes.NewReader(b)
		method = "POST"
	}
	req, _ := http.NewRequest(method, "http://"+l.Host+"/json/state", body)
	req.Header.Set("Content-Type", "application/json")
	r, e := lampHTTP.Do(req)
	if e != nil {
		return nil, e
	}
	defer r.Body.Close()
	b, e := io.ReadAll(io.LimitReader(r.Body, 128<<10))
	if r.StatusCode != 200 || !json.Valid(b) {
		return nil, fmt.Errorf("WLED: неверный ответ")
	}
	return b, e
}
func yeelightRequest(l lamp, method string, params []any) ([]string, error) {
	c, e := net.DialTimeout("tcp", net.JoinHostPort(l.Host, "55443"), time.Second)
	if e != nil {
		return nil, e
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(1500 * time.Millisecond))
	b, _ := json.Marshal(map[string]any{"id": 1, "method": method, "params": params})
	if _, e = c.Write(append(b, '\r', '\n')); e != nil {
		return nil, e
	}
	reader := bufio.NewReader(io.LimitReader(c, 16384))
	for n := 0; n < 12; n++ {
		line, e := reader.ReadBytes('\n')
		if e != nil {
			return nil, e
		}
		var result struct {
			ID     int      `json:"id"`
			Result []string `json:"result"`
			Error  any      `json:"error"`
		}
		if json.Unmarshal(line, &result) == nil && result.ID == 1 {
			if result.Error != nil {
				return nil, fmt.Errorf("Yeelight: команда не поддерживается")
			}
			return result.Result, nil
		}
	}
	return nil, fmt.Errorf("Yeelight не ответил")
}
func lampsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		lampsMu.Lock()
		defer lampsMu.Unlock()
		response(w, map[string]any{"ok": true, "lamps": lampList, "active": lampsCancel != nil, "error": lampsError, "config": lampsConfig})
		return
	}
	if r.Method != "POST" {
		fail(w, "Нужен GET или POST", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var q struct {
		Lamps []lamp `json:"lamps"`
	}
	if json.NewDecoder(r.Body).Decode(&q) != nil || len(q.Lamps) > 16 {
		fail(w, "До 16 устройств", 400)
		return
	}
	seen := map[string]bool{}
	for _, l := range q.Lamps {
		if !libraryID.MatchString(l.ID) || seen[l.ID] || len(l.Name) < 1 || len(l.Name) > 100 || !privateLampHost(l.Host) || (l.Kind != "wled" && l.Kind != "yeelight") {
			fail(w, "Укажите WLED или Yeelight с локальным IPv4-адресом", 400)
			return
		}
		seen[l.ID] = true
	}
	lampsControl.Lock()
	defer lampsControl.Unlock()
	stopLampsLocked()
	b, _ := json.Marshal(q.Lamps)
	if e := os.WriteFile(filepath.Join(base, "lamps.json"), b, 0600); e != nil {
		fail(w, e.Error(), 500)
		return
	}
	lampsMu.Lock()
	lampList = q.Lamps
	lampsMu.Unlock()
	response(w, map[string]bool{"ok": true})
}
func stopLampsLocked() {
	lampsMu.Lock()
	c, d := lampsCancel, lampsDone
	lampsMu.Unlock()
	if c != nil {
		c()
		<-d
	}
}
func lightsActionAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		fail(w, "Нужен POST", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var q lampShow
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		fail(w, "Неверный запрос", 400)
		return
	}
	lampsControl.Lock()
	defer lampsControl.Unlock()
	if q.Action == "stop" {
		stopLampsLocked()
		response(w, map[string]bool{"ok": true})
		return
	}
	if q.Action != "start" || (q.Mode != "music" && q.Mode != "cover" && q.Mode != "palette") || q.Min < 1 || q.Max > 100 || q.Min > q.Max || q.Gain < .25 || q.Gain > 20 || q.Interval < 2 || q.Interval > 20 || (q.Duration != 0 && q.Duration < 5) || q.Duration > 86400 || len(q.Palette) < 2 || len(q.Palette) > 24 || len(q.IDs) == 0 {
		fail(w, "Проверьте яркость, длительность и палитру. Интервал: от 2 секунд", 400)
		return
	}
	if (q.ColorSource != "" && q.ColorSource != "palette" && q.ColorSource != "cover" && q.ColorSource != "flag") || (q.Motion != "" && q.Motion != "static" && q.Motion != "rotate" && q.Motion != "breathe") {
		fail(w, "Выберите источник цвета и анимацию ламп", 400)
		return
	}
	for _, c := range q.Palette {
		if _, e := parseLampColor(c); e != nil {
			fail(w, "Неверный цвет", 400)
			return
		}
	}
	lampsMu.Lock()
	selected := []lamp{}
	for _, id := range q.IDs {
		found := false
		for _, l := range lampList {
			if l.ID == id {
				selected = append(selected, l)
				found = true
				break
			}
		}
		if !found {
			lampsMu.Unlock()
			fail(w, "Лампа не найдена", 404)
			return
		}
	}
	lampsMu.Unlock()
	stopLampsLocked()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	lampsMu.Lock()
	lampsCancel = cancel
	lampsDone = done
	lampsConfig = q
	lampsError = ""
	lampsMu.Unlock()
	go lampWorker(ctx, done, q, selected)
	response(w, map[string]bool{"ok": true})
}
func parseLampColor(s string) ([3]int, error) {
	var c [3]int
	if len(s) != 7 || s[0] != '#' {
		return c, fmt.Errorf("цвет")
	}
	n, e := strconv.ParseUint(s[1:], 16, 24)
	c = [3]int{int(n >> 16), int(n >> 8 & 255), int(n & 255)}
	return c, e
}
func lampWorker(ctx context.Context, done chan struct{}, q lampShow, selected []lamp) {
	defer close(done)
	original := map[string]json.RawMessage{}
	restoreY := map[string][]string{}
	ready := []lamp{}
	setError := func(e error) { lampsMu.Lock(); lampsError = e.Error(); lampsMu.Unlock() }
	for _, l := range selected {
		if l.Kind == "wled" {
			b, e := wledRequest(l, nil)
			if e != nil {
				setError(e)
				continue
			}
			original[l.ID] = b
		} else {
			v, e := yeelightRequest(l, "get_prop", []any{"power", "bright", "rgb", "color_mode", "ct", "hue", "sat"})
			if e != nil || len(v) < 7 {
				setError(fmt.Errorf("%s: не удалось сохранить исходный свет", l.Name))
				continue
			}
			restoreY[l.ID] = v
		}
		ready = append(ready, l)
	}
	defer func() {
		for _, l := range ready {
			if l.Kind == "wled" {
				if _, e := wledRequest(l, original[l.ID]); e != nil {
					setError(e)
				}
			} else {
				v := restoreY[l.ID]
				rgb, _ := strconv.Atoi(v[2])
				ct, _ := strconv.Atoi(v[4])
				hue, _ := strconv.Atoi(v[5])
				sat, _ := strconv.Atoi(v[6])
				bright, _ := strconv.Atoi(v[1])
				switch v[3] {
				case "1":
					yeelightRequest(l, "set_rgb", []any{rgb, "smooth", 300})
				case "2":
					yeelightRequest(l, "set_ct_abx", []any{ct, "smooth", 300})
				case "3":
					yeelightRequest(l, "set_hsv", []any{hue, sat, "smooth", 300})
				}
				yeelightRequest(l, "set_bright", []any{bright, "smooth", 300})
				yeelightRequest(l, "set_power", []any{v[0], "smooth", 300})
			}
		}
		lampsMu.Lock()
		lampsCancel = nil
		lampsMu.Unlock()
	}()
	if len(ready) == 0 {
		return
	}
	palette := append([]string{}, q.Palette...)
	ticker := time.NewTicker(time.Duration(q.Interval) * time.Second)
	defer ticker.Stop()
	deadline := time.Now().Add(time.Duration(q.Duration) * time.Second)
	index := 0
	var nextCover time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if q.Duration != 0 && now.After(deadline) {
				return
			}
			if (q.Mode == "cover" || q.ColorSource == "cover") && now.After(nextCover) {
				cover, e := refreshCover()
				palette = cover.Palette
				if !cover.Available {
					setError(fmt.Errorf("%s", cover.Error))
				} else {
					setError(e)
				}
				nextCover = now.Add(time.Second)
			}
			brightness := q.Max
			if q.Motion == "breathe" {
				brightness = q.Min + int((1+math.Sin(float64(index)*.6))/2*float64(q.Max-q.Min))
			}
			if q.Mode == "music" {
				musicMu.Lock()
				active := musicCancel != nil
				level := musicLevels.Level
				musicMu.Unlock()
				if !active {
					setError(fmt.Errorf("Включите музыкальную визуализацию: нужен источник звука плеера"))
					continue
				}
				brightness = q.Min + int(math.Min(1, math.Sqrt(level*q.Gain))*float64(q.Max-q.Min))
			}
			for i, l := range ready {
				position := float64(i) / float64(len(ready))
				if q.Motion != "static" {
					position += float64(index) * .08
				}
				r, g, b := paletteBlend(palette, position)
				color := []int{int(r), int(g), int(b)}
				var e error
				if l.Kind == "wled" {
					_, e = wledRequest(l, map[string]any{"on": true, "bri": brightness * 255 / 100, "transition": 5, "seg": []any{map[string]any{"id": 0, "fx": 0, "col": []any{color}}}})
				} else {
					_, e = yeelightRequest(l, "set_scene", []any{"color", color[0]<<16 | color[1]<<8 | color[2], brightness})
				}
				if e != nil {
					setError(fmt.Errorf("%s: %v", l.Name, e))
				}
			}
			index++
		}
	}
}
func loadLamps() {
	b, _ := os.ReadFile(filepath.Join(base, "lamps.json"))
	json.Unmarshal(b, &lampList)
}
func discoverLamps(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		fail(w, "GET", 405)
		return
	}
	c, e := net.ListenPacket("udp4", ":0")
	if e != nil {
		fail(w, e.Error(), 500)
		return
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(1800 * time.Millisecond))
	target, _ := net.ResolveUDPAddr("udp4", "239.255.255.250:1982")
	c.WriteTo([]byte("M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1982\r\nMAN: \"ssdp:discover\"\r\nST: wifi_bulb\r\n\r\n"), target)
	list := []lamp{}
	seen := map[string]bool{}
	for {
		b := make([]byte, 4096)
		n, _, e := c.ReadFrom(b)
		if e != nil {
			break
		}
		headers := map[string]string{}
		for _, line := range strings.Split(string(b[:n]), "\r\n") {
			p := strings.SplitN(line, ":", 2)
			if len(p) == 2 {
				headers[strings.ToLower(p[0])] = strings.TrimSpace(p[1])
			}
		}
		u, e := url.Parse(headers["location"])
		if e != nil || !privateLampHost(u.Hostname()) || u.Port() != "55443" || seen[u.Hostname()] || !strings.Contains(headers["support"], "set_scene") {
			continue
		}
		seen[u.Hostname()] = true
		list = append(list, lamp{randomID(), "Yeelight · " + headers["model"], "yeelight", u.Hostname()})
	}
	response(w, map[string]any{"ok": true, "lamps": list})
}
