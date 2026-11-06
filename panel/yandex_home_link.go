package main

// Passwordless web-session flow informed by AlexxIT/YandexStation (MIT).
// Unlike its token conversion, this adapter never uses another application's
// client_id/client_secret. Only a newly and explicitly approved web session.
import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
)

var homePassport = "https://passport.yandex.ru"
var homeWebOrigin = "https://iot.quasar.yandex.ru"
var homePageOrigin = "https://yandex.ru"
var homeWebMu sync.Mutex
var homeLogin *homeLinkLogin
var homeCSRF = regexp.MustCompile(`__CSRF__\s*=\s*"([^"\r\n]+)"`)
var homePageCSRF = regexp.MustCompile(`"csrfToken2":"([^"\r\n]+)"`)

type homeCookie struct {
	Origin string      `json:"origin"`
	Cookie http.Cookie `json:"cookie"`
}
type homeCookieJar struct {
	jar     http.CookieJar
	Entries []homeCookie
}

func newHomeJar(entries []homeCookie) *homeCookieJar {
	j, _ := cookiejar.New(nil)
	out := &homeCookieJar{jar: j}
	for _, e := range entries {
		u, err := url.Parse(e.Origin)
		if err == nil && homeAllowedURL(u) {
			out.SetCookies(u, []*http.Cookie{&e.Cookie})
		}
	}
	return out
}
func homeAllowedURL(u *url.URL) bool {
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	for _, origin := range []string{homePassport, homeWebOrigin, homePageOrigin} {
		o, _ := url.Parse(origin)
		if o != nil && u.Scheme == o.Scheme && u.Host == o.Host {
			return true
		}
	}
	return false
}
func (j *homeCookieJar) Cookies(u *url.URL) []*http.Cookie { return j.jar.Cookies(u) }
func (j *homeCookieJar) SetCookies(u *url.URL, cs []*http.Cookie) {
	if !homeAllowedURL(u) {
		return
	}
	for _, c := range cs {
		if c.Name == "" || len(c.Value) > 16384 {
			continue
		}
		d := strings.TrimPrefix(c.Domain, ".")
		if d != "" && d != u.Hostname() && !(strings.HasSuffix(u.Hostname(), "."+d) && (d == "yandex.ru" || strings.HasSuffix(d, ".yandex.ru"))) {
			continue
		}
		j.jar.SetCookies(u, []*http.Cookie{c})
		// Remember enough attributes to restore host-only and path semantics.
		copyCookie := *c
		if copyCookie.Path == "" {
			p := u.EscapedPath()
			i := strings.LastIndex(p, "/")
			if i <= 0 {
				copyCookie.Path = "/"
			} else {
				copyCookie.Path = p[:i]
			}
		}
		if c.MaxAge > 0 {
			copyCookie.Expires = time.Now().Add(time.Duration(c.MaxAge) * time.Second)
			copyCookie.MaxAge = 0
		}
		domain := strings.TrimPrefix(copyCookie.Domain, ".")
		if domain == "" {
			domain = u.Hostname()
		}
		kept := j.Entries[:0]
		for _, e := range j.Entries {
			old := strings.TrimPrefix(e.Cookie.Domain, ".")
			if old == "" {
				origin, _ := url.Parse(e.Origin)
				old = origin.Hostname()
			}
			if old != domain || e.Cookie.Path != copyCookie.Path || e.Cookie.Name != c.Name {
				kept = append(kept, e)
			}
		}
		j.Entries = kept
		if c.MaxAge >= 0 && (copyCookie.Expires.IsZero() || copyCookie.Expires.After(time.Now())) && len(j.Entries) < 256 {
			j.Entries = append(j.Entries, homeCookie{u.Scheme + "://" + u.Host + copyCookie.Path, copyCookie})
		}
	}
}

type homeLinkLogin struct {
	Client   *http.Client
	Jar      *homeCookieJar
	CSRF     string
	Track    map[string]any
	Link     string
	QR       string
	Expires  time.Time
	LastPoll time.Time
}

func homeNewClient(j *homeCookieJar) *http.Client {
	return &http.Client{Jar: j, Timeout: 15 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
}
func homeWebRequest(c *http.Client, method, target, csrf string, data any, form bool) ([]byte, error) {
	u, e := url.Parse(target)
	if e != nil || !homeAllowedURL(u) {
		return nil, fmt.Errorf("Адрес Яндекса не разрешён")
	}
	var b []byte
	content := "application/json"
	if data != nil {
		if form {
			v, ok := data.(url.Values)
			if !ok {
				return nil, fmt.Errorf("Некорректный запрос")
			}
			b = []byte(v.Encode())
			content = "application/x-www-form-urlencoded"
		} else {
			b, e = json.Marshal(data)
			if e != nil {
				return nil, e
			}
		}
	}
	req, e := http.NewRequest(method, target, bytes.NewReader(b))
	if e != nil {
		return nil, e
	}
	req.Header.Set("User-Agent", "AliceKit/2.5")
	req.Header.Set("Accept", "application/json, text/html")
	if data != nil {
		req.Header.Set("Content-Type", content)
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	res, e := c.Do(req)
	if e != nil {
		return nil, fmt.Errorf("Нет связи с Яндексом")
	}
	defer res.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(res.Body, (4<<20)+1))
	if e != nil || len(raw) > 4<<20 {
		return nil, fmt.Errorf("Неполный ответ Яндекса")
	}
	if res.StatusCode == 401 || res.StatusCode == 403 {
		return nil, fmt.Errorf("Сессия Яндекса недействительна или нужна проверка входа. Подключитесь по новой ссылке")
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, fmt.Errorf("Яндекс ответил HTTP %d. Запрос автоматически не повторялся", res.StatusCode)
	}
	return raw, nil
}
func homeLinkStart() (*homeLinkLogin, error) {
	j := newHomeJar(nil)
	s := &homeLinkLogin{Jar: j, Client: homeNewClient(j), Expires: time.Now().Add(5 * time.Minute)}
	raw, e := homeWebRequest(s.Client, "GET", homePassport+"/pwl-yandex", "", nil, false)
	if e != nil {
		return nil, e
	}
	m := homeCSRF.FindSubmatch(raw)
	if len(m) != 2 {
		return nil, fmt.Errorf("Яндекс изменил страницу входа. Ссылка не создана")
	}
	s.CSRF = string(m[1])
	raw, e = homeWebRequest(s.Client, "POST", homePassport+"/pwl-yandex/api/passport/auth/password/submit", s.CSRF, map[string]string{"retpath": homePassport + "/"}, false)
	if e != nil {
		return nil, e
	}
	if json.Unmarshal(raw, &s.Track) != nil {
		return nil, fmt.Errorf("Не удалось начать вход Яндекса")
	}
	track, _ := s.Track["track_id"].(string)
	if track == "" {
		return nil, fmt.Errorf("Яндекс не выдал сеанс входа")
	}
	raw, e = homeWebRequest(s.Client, "POST", homePassport+"/pwl-yandex/api/passport/auth/magic/code", s.CSRF, url.Values{"location_id": {"0"}, "magic_track_id": {track}, "track_id": {""}}, true)
	if e != nil {
		return nil, e
	}
	var response struct {
		Link string `json:"link"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return nil, fmt.Errorf("Не удалось получить ссылку")
	}
	u, e := url.Parse(response.Link)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Port() != "" || !(u.Hostname() == "passport.yandex.ru" || u.Hostname() == "yandex.ru" || u.Hostname() == "ya.ru") {
		return nil, fmt.Errorf("Яндекс вернул неожиданный адрес входа")
	}
	s.Link = response.Link
	png, e := qrcode.Encode(s.Link, qrcode.Medium, 384)
	if e != nil {
		return nil, fmt.Errorf("Не удалось создать QR-код входа")
	}
	s.QR = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	return s, nil
}
func homeWebConnected() bool {
	s, e := os.Stat(filepath.Join(base, "home-web-session"))
	return e == nil && !s.IsDir() && s.Size() > 2
}
func homeConnected() bool { return homeWebConnected() || homeToken() != "" }
func homeLoadWeb() (*http.Client, *homeCookieJar, error) {
	raw, e := os.ReadFile(filepath.Join(base, "home-web-session"))
	var entries []homeCookie
	if e != nil || len(raw) > 256*1024 || json.Unmarshal(raw, &entries) != nil || len(entries) == 0 {
		return nil, nil, fmt.Errorf("Подключите Дом с Алисой по ссылке")
	}
	jar := newHomeJar(entries)
	return homeNewClient(jar), jar, nil
}
func homeWebSave(j *homeCookieJar) error {
	raw, e := json.Marshal(j.Entries)
	if e != nil {
		return e
	}
	return homeSave("home-web-session", raw)
}
func homeWebInfo(c *http.Client) (homeInfo, error) {
	var info homeInfo
	raw, e := homeWebRequest(c, "GET", homeWebOrigin+"/m/v3/user/devices", "", nil, false)
	if e != nil {
		return info, e
	}
	var payload struct {
		Status     string `json:"status"`
		Households []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			All  []struct {
				homeDevice
				ItemType string `json:"item_type"`
				RoomName string `json:"room_name"`
			} `json:"all"`
			Rooms []json.RawMessage `json:"rooms"`
		} `json:"households"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.Status != "ok" {
		return info, fmt.Errorf("Яндекс не подтвердил доступ к умному дому")
	}
	info.Status = "ok"
	seen := map[string]bool{}
	seenRooms := map[string]bool{}
	for _, house := range payload.Households {
		roomOfDevice := map[string]string{}
		roomByName := map[string]string{}
		h, _ := json.Marshal(map[string]string{"id": house.ID, "name": house.Name})
		info.Households = append(info.Households, h)
		for _, r := range house.Rooms {
			var room struct {
				ID    string `json:"id"`
				Name  string `json:"name"`
				Items []struct {
					ID string `json:"id"`
				} `json:"items"`
			}
			if json.Unmarshal(r, &room) == nil && room.ID != "" {
				roomByName[room.Name] = room.ID
				for _, item := range room.Items {
					roomOfDevice[item.ID] = room.ID
				}
				if seenRooms[room.ID] {
					continue
				}
				metadata, _ := json.Marshal(map[string]string{"id": room.ID, "name": room.Name})
				info.Rooms = append(info.Rooms, metadata)
				seenRooms[room.ID] = true
			}
		}
		for _, item := range house.All {
			if item.ItemType != "device" || !homeID.MatchString(item.ID) || seen[item.ID] {
				continue
			}
			seen[item.ID] = true
			if id := roomByName[item.Room]; id != "" {
				item.Room = id
			}
			if item.Room == "" {
				item.Room = roomOfDevice[item.ID]
			}
			if item.Room == "" {
				item.Room = roomByName[item.RoomName]
			}
			info.Devices = append(info.Devices, item.homeDevice)
			if item.Room != "" && !seenRooms[item.Room] {
				room, _ := json.Marshal(map[string]string{"id": item.Room, "name": item.RoomName})
				info.Rooms = append(info.Rooms, room)
				seenRooms[item.Room] = true
			}
		}
	}
	return info, nil
}
func homeWebCall(method, path string, body, out any) error {
	homeWebMu.Lock()
	defer homeWebMu.Unlock()
	c, jar, e := homeLoadWeb()
	if e != nil {
		return e
	}
	if method == "GET" && path == "/v1.0/user/info" {
		info, e := homeWebInfo(c)
		if e != nil {
			return e
		}
		if e = homeWebSave(jar); e != nil {
			return fmt.Errorf("Не удалось сохранить сессию")
		}
		raw, _ := json.Marshal(info)
		return json.Unmarshal(raw, out)
	}
	id := strings.TrimPrefix(path, "/v1.0/devices/")
	target := ""
	csrf := ""
	if method == "GET" && homeID.MatchString(id) {
		target = homeWebOrigin + "/m/user/devices/" + id
	} else if method == "POST" && path == "/v1.0/devices/actions" {
		raw, _ := json.Marshal(body)
		var payload struct {
			Devices []homeAction `json:"devices"`
		}
		if json.Unmarshal(raw, &payload) != nil || len(payload.Devices) != 1 || !homeID.MatchString(payload.Devices[0].ID) {
			return fmt.Errorf("Некорректное действие")
		}
		id = payload.Devices[0].ID
		target = homeWebOrigin + "/m/user/devices/" + id + "/actions"
		body = map[string]any{"actions": payload.Devices[0].Actions}
		raw, e = homeWebRequest(c, "GET", homePageOrigin+"/iot", "", nil, false)
		if e != nil {
			return e
		}
		m := homePageCSRF.FindSubmatch(raw)
		if len(m) != 2 {
			return fmt.Errorf("Не удалось проверить защиту команды Яндекса")
		}
		csrf = string(m[1])
	} else {
		return fmt.Errorf("Операция веб-доступа не поддерживается")
	}
	raw, e := homeWebRequest(c, method, target, csrf, body, false)
	if e != nil {
		return e
	}
	var status struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(raw, &status) != nil || status.Status != "ok" {
		return fmt.Errorf("Яндекс не подтвердил запрос")
	}
	// A physical action has already happened: persistence errors must not invite a retry.
	if e = homeWebSave(jar); e != nil {
		return fmt.Errorf("Яндекс получил запрос, но сессия не сохранена. Не повторяйте действие автоматически")
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// Free color/temperature uses the web application's dedicated endpoint,
// not the official OAuth capability endpoint. No fabricated device receipt.
func homeIsWebColor(q homeAction) bool {
	if len(q.Actions) != 1 || q.Actions[0].Type != "devices.capabilities.color_setting" {
		return false
	}
	i, _ := q.Actions[0].State["instance"].(string)
	return i == "hsv" || i == "rgb" || i == "temperature_k"
}
func homeColorPayload(q homeAction) (map[string]any, error) {
	if !homeIsWebColor(q) || !homeID.MatchString(q.ID) {
		return nil, fmt.Errorf("Некорректная команда цвета")
	}
	a := q.Actions[0]
	instance, _ := a.State["instance"].(string)
	value := a.State["value"]
	payload := map[string]any{"device_ids": []string{q.ID}}
	valid := func(v any, min, max float64) (float64, bool) {
		f, ok := v.(float64)
		return f, ok && !math.IsNaN(f) && !math.IsInf(f, 0) && f >= min && f <= max
	}
	switch instance {
	case "temperature_k":
		v, ok := valid(value, 1, 10000)
		if !ok {
			return nil, fmt.Errorf("Некорректная температура света")
		}
		payload[instance] = v
	case "hsv":
		v, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Нужен цвет HSV")
		}
		for k, max := range map[string]float64{"h": 360, "s": 100, "v": 100} {
			if _, ok := valid(v[k], 0, max); !ok {
				return nil, fmt.Errorf("Некорректный цвет HSV")
			}
		}
		payload[instance] = v
	case "rgb":
		v, ok := valid(value, 0, 0xffffff)
		if !ok || v != math.Trunc(v) {
			return nil, fmt.Errorf("Некорректный цвет RGB")
		}
		rgb := int(v)
		r, g, b := float64((rgb>>16)&255)/255, float64((rgb>>8)&255)/255, float64(rgb&255)/255
		max, min := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
		delta := max - min
		h, s := 0.0, 0.0
		if delta > 0 {
			s = delta / max * 100
			switch max {
			case r:
				h = 60 * math.Mod((g-b)/delta, 6)
			case g:
				h = 60 * ((b-r)/delta + 2)
			default:
				h = 60 * ((r-g)/delta + 4)
			}
			if h < 0 {
				h += 360
			}
		}
		payload["hsv"] = map[string]any{"h": math.Round(h), "s": math.Round(s), "v": math.Round(max * 100)}
	}
	return payload, nil
}
func homeWebColor(q homeAction) (json.RawMessage, error) {
	payload, e := homeColorPayload(q)
	if e != nil {
		return nil, e
	}
	homeWebMu.Lock()
	defer homeWebMu.Unlock()
	c, jar, e := homeLoadWeb()
	if e != nil {
		return nil, e
	}
	raw, e := homeWebRequest(c, "GET", homePageOrigin+"/iot", "", nil, false)
	if e != nil {
		return nil, e
	}
	m := homePageCSRF.FindSubmatch(raw)
	if len(m) != 2 {
		return nil, fmt.Errorf("Не удалось проверить защиту команды цвета")
	}
	raw, e = homeWebRequest(c, "POST", homeWebOrigin+"/m/v3/user/custom/group/color/apply", string(m[1]), payload, false)
	if e != nil {
		return nil, e
	}
	var result struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Status != "ok" {
		return nil, fmt.Errorf("Яндекс не принял изменение цвета")
	}
	if e = homeWebSave(jar); e != nil {
		return nil, fmt.Errorf("Яндекс получил цвет, но сессия не сохранена. Не повторяйте действие автоматически")
	}
	return json.RawMessage(`{"status":"ok","accepted":true,"confirmation":"cloud"}`), nil
}
func homeLinkAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	homeWebMu.Lock()
	defer homeWebMu.Unlock()
	if r.Method == "DELETE" {
		homeLogin = nil
		response(w, map[string]bool{"ok": true})
		return
	}
	if r.Method == "POST" {
		// Drain the bounded optional JSON body before replying. Some Android
		// connections otherwise reset when a body remains unread on close.
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			fail(w, "Слишком большой запрос входа", 400)
			return
		}
		var q struct {
			Renew bool `json:"renew"`
		}
		if len(bytes.TrimSpace(body)) > 0 && json.Unmarshal(body, &q) != nil {
			fail(w, "Некорректный запрос входа", 400)
			return
		}
		if q.Renew {
			homeLogin = nil
		}
		if homeLogin != nil && time.Now().Before(homeLogin.Expires) {
			response(w, map[string]any{"state": "waiting", "link": homeLogin.Link, "qr": homeLogin.QR, "expires": homeLogin.Expires.Unix()})
			return
		}
		s, e := homeLinkStart()
		if e != nil {
			fail(w, e.Error(), 502)
			return
		}
		homeLogin = s
		response(w, map[string]any{"state": "waiting", "link": s.Link, "qr": s.QR, "expires": s.Expires.Unix()})
		return
	}
	if r.Method != "GET" {
		fail(w, "Нужен GET, POST или DELETE", 405)
		return
	}
	if homeLogin == nil {
		if homeConnected() {
			response(w, map[string]string{"state": "connected"})
			return
		}
		response(w, map[string]string{"state": "idle"})
		return
	}
	s := homeLogin
	// A user may have approved before the UI deadline and returned later.
	// Check Yandex once more before discarding the pending browser session.
	if time.Now().After(s.Expires.Add(5 * time.Minute)) {
		homeLogin = nil
		response(w, map[string]string{"state": "expired"})
		return
	}
	if time.Since(s.LastPoll) < 3*time.Second {
		state := "waiting"
		if time.Now().After(s.Expires) {
			state = "expired"
		}
		response(w, map[string]string{"state": state})
		return
	}
	s.LastPoll = time.Now()
	raw, e := homeWebRequest(s.Client, "POST", homePassport+"/pwl-yandex/api/passport/auth/magic/code/status", s.CSRF, s.Track, false)
	if e != nil {
		fail(w, e.Error(), 502)
		return
	}
	var status struct {
		State  string   `json:"state"`
		Track  string   `json:"trackId"`
		Errors []string `json:"errors"`
	}
	if json.Unmarshal(raw, &status) != nil {
		fail(w, "Не удалось проверить подтверждение", 502)
		return
	}
	if status.State != "otp_auth_finished" {
		for _, state := range append(status.Errors, status.State) {
			state = strings.ToLower(state)
			if strings.Contains(state, "expired") || strings.Contains(state, "timeout") || strings.Contains(state, "invalid") {
				homeLogin = nil
				response(w, map[string]string{"state": "expired"})
				return
			}
		}
		if time.Now().After(s.Expires) {
			response(w, map[string]string{"state": "expired"})
			return
		}
		response(w, map[string]string{"state": "waiting"})
		return
	}
	if status.Track == "" {
		homeLogin = nil
		fail(w, "Яндекс не выдал подтверждение входа", 502)
		return
	}
	// Consume once; never replay a session-creation request after an ambiguous result.
	homeLogin = nil
	_, e = homeWebRequest(s.Client, "POST", homePassport+"/pwl-yandex/api/passport/sessions/get_session", s.CSRF, url.Values{"track_id": {status.Track}}, true)
	if e != nil {
		fail(w, e.Error(), 502)
		return
	}
	info, e := homeWebInfo(s.Client)
	if e != nil {
		fail(w, e.Error(), 502)
		return
	}
	if len(s.Jar.Entries) == 0 {
		fail(w, "Яндекс не сохранил сессию. Получите новую ссылку", 502)
		return
	}
	if e = homeWebSave(s.Jar); e != nil {
		fail(w, "Не удалось сохранить подключение", 500)
		return
	}
	_ = os.Remove(filepath.Join(base, "home-oauth"))
	clearPluginHomeGrants()
	response(w, map[string]any{"state": "connected", "devices": len(info.Devices)})
}
