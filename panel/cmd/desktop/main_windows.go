package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/sys/windows"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"stationpanel/internal/stockdiscovery"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

//go:embed assets/*
var assets embed.FS

const version = "2.8.0"
const channel = "https://github.com/golodgolub-bit/Station-Studio/releases/latest/download/channel.json"
const fakeKey = "000000000000000000000000000000000000000000000000"

// Public release key, shared with the Android updater. No private key ships.
const publicKey = "MIIBojANBgkqhkiG9w0BAQEFAAOCAY8AMIIBigKCAYEA9LbvZNwkL22jM48Imj4ANKNIPdcyAwAvqCki+9dLJByS5WSHkdAkA/hNpKZgALktkuWl9rfow3npYQtz4U0hqJESnO5eVjZWCAC3kx9/zBfR/26iBJ+u/76eitYaxuQLPcNP8TfyVMAORGTxaha7+9KGEyqgfstCvXBTTQ+hUH3J+jwJTgYTpbUQc+ScBEizYnHOlhTbz82xWlZjy+BHVy2zPnlqSXX7F+zZb1o2yJk8H/SFruj1CT6eyVhz1PPryY0uz50KsRKHJ4aWtVfjHpWF+R4vFy8YqD5b9OEOwyx/I+VldcFyZ4hNKqKHrqeWeGHljw24eBE/kvnCmKlD6FsvYZfW9S2zHF7V3SFUfl4yWlXbYwlhHBlIICb5rA1FG5cJIbAc1fKOF4hxUmnS/iQraYoV/07j09m+XeThoIW7YFwib++4m9osz+WUyEe5o96rCUUUeGVl06QS74aGf2kIYtcP4eP/qE21u1TlYrKx3Jcv611QvRktwspKYpIZAgMBAAE="

type record struct{ Host, Pin, Token, ClientID string }
type app struct {
	mu                  sync.RWMutex
	config              record
	cookie, origin, dir string
	last                time.Time
}

func random(n int) string {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func mac(key, value string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(value))
	return hex.EncodeToString(h.Sum(nil))
}
func seal(b []byte, decode bool) ([]byte, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("empty data")
	}
	in := windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
	var out windows.DataBlob
	var e error
	if decode {
		e = windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		e = windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if e != nil {
		return nil, e
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), nil
}
func (a *app) save(c record) error {
	b, e := json.Marshal(c)
	if e != nil {
		return e
	}
	b, e = seal(b, false)
	if e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(a.dir, "connection.next"), b, 0600); e != nil {
		return e
	}
	if e = os.Rename(filepath.Join(a.dir, "connection.next"), filepath.Join(a.dir, "connection.dat")); e != nil {
		return e
	}
	a.mu.Lock()
	a.config = c
	a.mu.Unlock()
	return nil
}
func privateHost(host string) (string, error) {
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil || !ip.IsPrivate() {
		return "", fmt.Errorf("Укажите локальный IPv4-адрес станции")
	}
	return ip.String(), nil
}
func transport(pin string, observed *string) *http.Transport {
	return &http.Transport{Proxy: nil, DialContext: stockdiscovery.DialContext, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true, VerifyConnection: func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return fmt.Errorf("Нет сертификата станции")
		}
		cert := cs.PeerCertificates[0]
		now := time.Now()
		if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
			return fmt.Errorf("Срок сертификата станции истёк")
		}
		sum := sha256.Sum256(cert.Raw)
		actual := hex.EncodeToString(sum[:])
		if pin != "" && !hmac.Equal([]byte(actual), []byte(pin)) {
			return fmt.Errorf("Сертификат станции изменился. Подключитесь по новому коду")
		}
		if observed != nil {
			*observed = actual
		}
		return nil
	}}}
}
func (a *app) snapshot() record { a.mu.RLock(); defer a.mu.RUnlock(); return a.config }
func jsonOut(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
func errOut(w http.ResponseWriter, e error, code int) {
	w.WriteHeader(code)
	jsonOut(w, map[string]string{"error": e.Error()})
}
func discover() []string {
	found := []string{}
	conn, e := net.ListenUDP("udp4", &net.UDPAddr{})
	if e != nil {
		return found
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	query := []byte("STATION_STUDIO_DISCOVER_V1:" + random(16))
	dest := map[string]bool{"255.255.255.255": true}
	interfaces, _ := net.Interfaces()
	for _, i := range interfaces {
		addrs, _ := i.Addrs()
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil || !ipnet.IP.IsPrivate() {
				continue
			}
			ip := ipnet.IP.To4()
			mask := ipnet.Mask
			if len(mask) != 4 {
				continue
			}
			b := net.IPv4(ip[0]|^mask[0], ip[1]|^mask[1], ip[2]|^mask[2], ip[3]|^mask[3])
			dest[b.String()] = true
		}
	}
	for ip := range dest {
		conn.WriteToUDP(query, &net.UDPAddr{IP: net.ParseIP(ip), Port: 8788})
	}
	seen := map[string]bool{}
	for {
		b := make([]byte, 4096)
		n, peer, e := conn.ReadFromUDP(b)
		if e != nil {
			break
		}
		var d struct {
			TLSPort int    `json:"tlsPort"`
			Model   string `json:"model"`
		}
		if json.Unmarshal(b[:n], &d) == nil && d.TLSPort == 8789 && peer.IP.IsPrivate() && !seen[peer.IP.String()] {
			seen[peer.IP.String()] = true
			found = append(found, peer.IP.String())
		}
	}
	return found
}
func (a *app) pair(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST", 405)
		return
	}
	var q struct{ Host, Code string }
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		http.Error(w, "JSON", 400)
		return
	}
	host, e := privateHost(q.Host)
	if e != nil {
		errOut(w, e, 400)
		return
	}
	q.Code = strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(q.Code), " ", ""), "-", ""))
	if len(q.Code) != 16 {
		errOut(w, fmt.Errorf("Введите код из 16 символов из панели станции"), 400)
		return
	}
	observed := ""
	tr := transport("", &observed)
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	// Complete TLS first, then bind the pairing proof to that exact certificate.
	probe, e := client.Get("https://" + host + ":8789/api/discovery")
	if e != nil {
		errOut(w, e, 502)
		return
	}
	probe.Body.Close()
	tr.CloseIdleConnections()
	pinned := transport(observed, nil)
	defer pinned.CloseIdleConnections()
	client.Transport = pinned
	nonce := random(16)
	body, _ := json.Marshal(map[string]string{"nonce": nonce, "proof": mac(q.Code, nonce+"|"+observed), "name": "Station Studio · Windows"})
	reply, e := client.Post("https://"+host+":8789/api/pair", "application/json", bytes.NewReader(body))
	if e != nil {
		errOut(w, e, 502)
		return
	}
	defer reply.Body.Close()
	var result struct{ Token, ClientID, Proof, Error string }
	if json.NewDecoder(io.LimitReader(reply.Body, 8192)).Decode(&result) != nil {
		errOut(w, fmt.Errorf("Неверный ответ станции"), 502)
		return
	}
	if reply.StatusCode != 200 {
		errOut(w, fmt.Errorf("%s", result.Error), reply.StatusCode)
		return
	}
	if len(result.Token) != 48 || !hmac.Equal([]byte(result.Proof), []byte(mac(q.Code, nonce+"|"+observed+"|"+result.Token))) {
		errOut(w, fmt.Errorf("Подтверждение станции не совпало"), 403)
		return
	}
	if e = a.save(record{host, observed, result.Token, result.ClientID}); e != nil {
		errOut(w, e, 500)
		return
	}
	jsonOut(w, map[string]bool{"ok": true})
}
func (a *app) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Host != strings.TrimPrefix(a.origin, "http://") {
		http.Error(w, "Local host required", 403)
		return
	}
	if r.URL.Path == "/desktop/start" && hmac.Equal([]byte(r.URL.Query().Get("code")), []byte(a.cookie)) {
		http.SetCookie(w, &http.Cookie{Name: "studio-session", Value: a.cookie, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		where := "/desktop/"
		if a.snapshot().Token != "" {
			where = "/"
		}
		http.Redirect(w, r, where, 303)
		return
	}
	c, e := r.Cookie("studio-session")
	if e != nil || !hmac.Equal([]byte(c.Value), []byte(a.cookie)) {
		http.Error(w, "Open Station Studio from its application", 403)
		return
	}
	if o := r.Header.Get("Origin"); o != "" && o != a.origin {
		http.Error(w, "Origin mismatch", 403)
		return
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		http.Error(w, "Cross-site request", 403)
		return
	}
	a.mu.Lock()
	a.last = time.Now()
	a.mu.Unlock()
	switch r.URL.Path {
	case "/desktop/":
		b, _ := assets.ReadFile("assets/setup.html")
		w.Header().Set("Content-Type", "text/html;charset=utf-8")
		w.Write(b)
		return
	case "/desktop/manrope.ttf":
		b, _ := assets.ReadFile("assets/manrope.ttf")
		w.Header().Set("Content-Type", "font/ttf")
		w.Write(b)
	case "/desktop/kontur.png":
		b, _ := assets.ReadFile("assets/kontur.png")
		w.Header().Set("Content-Type", "image/png")
		w.Write(b)
	case "/desktop/station.png":
		b, _ := assets.ReadFile("assets/station.png")
		w.Header().Set("Content-Type", "image/png")
		w.Write(b)
		return
	case "/desktop/onboarding.html", "/desktop/onboarding.css", "/desktop/onboarding.js", "/desktop/station-max.png", "/desktop/launch-star.png":
		name := strings.TrimPrefix(r.URL.Path, "/desktop/")
		b, e := assets.ReadFile("assets/" + name)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		mime := "image/png"
		if strings.HasSuffix(name, ".html") {
			mime = "text/html;charset=utf-8"
		}
		if strings.HasSuffix(name, ".css") {
			mime = "text/css;charset=utf-8"
		}
		if strings.HasSuffix(name, ".js") {
			mime = "text/javascript;charset=utf-8"
		}
		w.Header().Set("Content-Type", mime)
		w.Write(b)
		return
	case "/desktop/state":
		cfg := a.snapshot()
		jsonOut(w, map[string]any{"version": version, "host": cfg.Host, "connected": cfg.Token != ""})
		return
	case "/desktop/discover":
		if r.Method != "POST" {
			http.Error(w, "POST", 405)
			return
		}
		stockChannel := make(chan []stockdiscovery.Station, 1)
		go func() {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			stockChannel <- stockdiscovery.Discover(ctx)
		}()
		panels := discover()
		stations := <-stockChannel
		for _, host := range panels {
			found := false
			for i := range stations {
				if stations[i].Host == host {
					stations[i].Panel = true
					found = true
					break
				}
			}
			if !found {
				stations = append(stations, stockdiscovery.Station{Host: host, Name: "Станция Макс", Panel: true})
			}
		}
		jsonOut(w, map[string]any{"stations": panels, "devices": stations})
		return
	case "/desktop/pair":
		a.pair(w, r)
		return
	case "/desktop/forget":
		if r.Method != "POST" {
			http.Error(w, "POST", 405)
			return
		}
		if e := a.save(record{}); e != nil {
			errOut(w, e, 500)
			return
		}
		jsonOut(w, map[string]bool{"ok": true})
		return
	case "/desktop/publish":
		if r.Method != "POST" {
			http.Error(w, "POST", 405)
			return
		}
		if e := publisherWindow(); e != nil {
			errOut(w, e, 500)
			return
		}
		jsonOut(w, map[string]bool{"ok": true})
		return
	case "/desktop/update":
		a.update(w, r)
		return
	case "/desktop/alive":
		jsonOut(w, map[string]bool{"ok": true})
		return
	case "/desktop/session.js":
		w.Header().Set("Content-Type", "application/javascript")
		io.WriteString(w, `window.StationDesktop={publish:async()=>{const r=await fetch('/desktop/publish',{method:'POST'});if(!r.ok)throw Error('Не удалось открыть мастерскую');}};addEventListener('DOMContentLoaded',()=>{if(window.stationNativeConnect)window.stationNativeConnect('`+fakeKey+`');const p=document.querySelector('.aside-bottom');if(p){const a=document.createElement('a');a.href='/desktop/';a.textContent='Подключение и обновления ПК';a.style.cssText='font-size:11px;line-height:1.6;color:#6d7c64;text-decoration:none;margin-top:12px';p.append(a)}});setInterval(()=>fetch('/desktop/alive').catch(()=>{}),15000);`)
		return
	}
	cfg := a.snapshot()
	if cfg.Token == "" {
		http.Redirect(w, r, "/desktop/", 303)
		return
	}
	target, _ := url.Parse("https://" + cfg.Host + ":8789")
	tr := transport(cfg.Pin, nil)
	defer tr.CloseIdleConnections()
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = tr
	original := proxy.Director
	proxy.Director = func(q *http.Request) {
		original(q)
		q.Host = target.Host
		q.Header.Del("Cookie")
		q.Header.Del("Origin")
		q.Header.Del("Referer")
		q.Header.Del("Forwarded")
		q.Header.Del("X-Forwarded-For")
		q.Header.Set("Authorization", "Bearer "+cfg.Token)
		q.Header.Del("Accept-Encoding")
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) {
		errOut(w, fmt.Errorf("Станция недоступна. Проверьте домашнюю сеть и подключение в меню ПК"), 502)
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		if response.StatusCode == 401 {
			return fmt.Errorf("Подключение отозвано. Подключите ПК по новому коду")
		}
		if strings.HasPrefix(response.Header.Get("Content-Type"), "text/html") && r.URL.Path == "/" {
			b, e := io.ReadAll(io.LimitReader(response.Body, 4<<20))
			response.Body.Close()
			if e != nil {
				return e
			}
			b = bytes.Replace(b, []byte("</body>"), []byte(`<script src="/desktop/session.js"></script></body>`), 1)
			response.Body = io.NopCloser(bytes.NewReader(b))
			response.ContentLength = int64(len(b))
			response.Header.Del("Content-Length")
			response.Header.Del("Content-Encoding")
		}
		return nil
	}
	proxy.ServeHTTP(w, r)
}
func main() {
	dir := filepath.Join(os.Getenv("LOCALAPPDATA"), "Station Studio")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--self-test" {
		b, e := seal([]byte("station-studio-self-test"), false)
		if e == nil {
			b, e = seal(b, true)
		}
		if e != nil || string(b) != "station-studio-self-test" {
			fmt.Println("FAIL", e)
			os.Exit(1)
		}
		fmt.Println("PASS DPAPI roundtrip")
		return
	}
	if launchVerifiedUpdate(dir) {
		return
	}
	a := &app{dir: dir, cookie: random(24), last: time.Now()}
	if b, e := os.ReadFile(filepath.Join(dir, "connection.dat")); e == nil {
		if plain, e := seal(b, true); e == nil {
			json.Unmarshal(plain, &a.config)
		}
	}
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		return
	}
	a.origin = "http://" + listener.Addr().String()
	url := a.origin + "/desktop/start?code=" + a.cookie
	go func() {
		previousTick := time.Now()
		for {
			time.Sleep(30 * time.Second)
			now := time.Now()
			if now.Sub(previousTick) > time.Minute {
				a.mu.Lock()
				a.last = now
				a.mu.Unlock()
			}
			previousTick = now
			a.mu.RLock()
			last := a.last
			a.mu.RUnlock()
			if time.Since(last) > 5*time.Minute {
				os.Exit(0)
			}
		}
	}()
	edge := ""
	for _, root := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA")} {
		p := filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe")
		if _, e := os.Stat(p); e == nil {
			edge = p
			break
		}
	}
	if len(os.Args) > 1 && os.Args[1] == "--headless-test" {
		fmt.Println(url)
	} else if edge != "" {
		cmd := exec.Command(edge, "--app="+url, "--window-size=1320,900")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		cmd.Start()
	} else {
		cmd := exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		cmd.Start()
	}
	(&http.Server{Handler: a, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 45 * time.Second}).Serve(listener)
}

type artifact struct {
	URL, SHA256 string
	Size        int64
}
type release struct {
	Version string   `json:"versionName"`
	Desktop artifact `json:"desktop"`
}

func fetchPublic(address string, max int64) ([]byte, error) {
	check := func(u *url.URL) bool {
		return u.Scheme == "https" && (u.Hostname() == "github.com" || strings.HasSuffix(u.Hostname(), ".githubusercontent.com"))
	}
	u, e := url.Parse(address)
	if e != nil || !check(u) {
		return nil, fmt.Errorf("Неподдерживаемый источник обновления")
	}
	client := http.Client{Timeout: 90 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if !check(r.URL) || len(via) > 5 {
			return fmt.Errorf("Неверное перенаправление")
		}
		return nil
	}}
	res, e := client.Get(address)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("Канал обновлений ответил %d", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, max+1))
	if int64(len(b)) > max {
		return nil, fmt.Errorf("Слишком большой файл")
	}
	return b, e
}
func signedRelease(raw []byte) (release, error) {
	var envelope struct{ Payload, Signature string }
	var info release
	if json.Unmarshal(raw, &envelope) != nil {
		return info, fmt.Errorf("Неверный каталог")
	}
	payload, e := base64.StdEncoding.DecodeString(envelope.Payload)
	if e != nil {
		return info, e
	}
	sig, e := base64.StdEncoding.DecodeString(envelope.Signature)
	if e != nil {
		return info, e
	}
	der, e := base64.StdEncoding.DecodeString(publicKey)
	if e != nil {
		return info, e
	}
	key, e := x509.ParsePKIXPublicKey(der)
	if e != nil {
		return info, e
	}
	rsaKey, ok := key.(*rsa.PublicKey)
	if !ok {
		return info, fmt.Errorf("Неверный ключ")
	}
	sum := sha256.Sum256(payload)
	if e = rsa.VerifyPKCS1v15(rsaKey, crypto.SHA256, sum[:], sig); e != nil {
		return info, fmt.Errorf("Подпись обновления не совпала")
	}
	e = json.Unmarshal(payload, &info)
	return info, e
}
func newer(v string) bool {
	if !regexp.MustCompile(`^[0-9]{1,5}\.[0-9]{1,5}\.[0-9]{1,5}$`).MatchString(v) {
		return false
	}
	var a, b, c, x, y, z int
	if _, e := fmt.Sscanf(v, "%d.%d.%d", &a, &b, &c); e != nil {
		return false
	}
	fmt.Sscanf(version, "%d.%d.%d", &x, &y, &z)
	return a > x || (a == x && b > y) || (a == x && b == y && c > z)
}
func (a *app) update(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST", 405)
		return
	}
	raw, e := fetchPublic(channel, 256<<10)
	if e != nil {
		errOut(w, e, 502)
		return
	}
	info, e := signedRelease(raw)
	if e != nil {
		errOut(w, e, 502)
		return
	}
	if !newer(info.Version) || info.Desktop.URL == "" {
		jsonOut(w, map[string]any{"ok": true, "current": true, "version": version})
		return
	}
	d := info.Desktop
	if d.Size < 1 || d.Size > 64<<20 {
		errOut(w, fmt.Errorf("Неверный размер обновления"), 502)
		return
	}
	b, e := fetchPublic(d.URL, 64<<20)
	if e != nil {
		errOut(w, e, 502)
		return
	}
	sum := sha256.Sum256(b)
	if int64(len(b)) != d.Size || hex.EncodeToString(sum[:]) != d.SHA256 {
		errOut(w, fmt.Errorf("SHA-256 обновления не совпала"), 502)
		return
	}
	target := filepath.Join(a.dir, "update-"+info.Version+".exe")
	if e = os.WriteFile(target, b, 0700); e == nil {
		e = os.WriteFile(filepath.Join(a.dir, "update-channel.json"), raw, 0600)
	}
	if e != nil {
		errOut(w, e, 500)
		return
	}
	jsonOut(w, map[string]any{"ok": true, "version": info.Version, "restart": true})
	go func() { time.Sleep(time.Second); exec.Command(target).Start(); os.Exit(0) }()
}
func launchVerifiedUpdate(dir string) bool {
	raw, e := os.ReadFile(filepath.Join(dir, "update-channel.json"))
	if e != nil {
		return false
	}
	info, e := signedRelease(raw)
	if e != nil || !newer(info.Version) {
		return false
	}
	path := filepath.Join(dir, "update-"+info.Version+".exe")
	b, e := os.ReadFile(path)
	if e != nil || int64(len(b)) != info.Desktop.Size {
		return false
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != info.Desktop.SHA256 {
		return false
	}
	return exec.Command(path).Start() == nil
}

// The author window has its own remote origin and receives no station credentials.
func publisherWindow() error {
	const address = "https://station-studio-community.shaaaape.chatgpt.site/app"
	for _, root := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA")} {
		p := filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe")
		if _, e := os.Stat(p); e == nil {
			cmd := exec.Command(p, "--app="+address, "--window-size=620,860")
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			return cmd.Start()
		}
	}
	return fmt.Errorf("Для встроенной мастерской требуется Microsoft Edge. Можно опубликовать плагин из приложения телефона")
}
