package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

type stationAppearance struct {
	Variant string `json:"variant"`
}
type localProfile struct {
	Name     string            `json:"name"`
	Accent   string            `json:"accent"`
	Telegram *telegramIdentity `json:"telegram,omitempty"`
}
type telegramIdentity struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username,omitempty"`
}

var profileMu sync.Mutex

func writePrivateJSON(name string, value any) error {
	raw, e := json.Marshal(value)
	if e != nil {
		return e
	}
	tmp, e := os.CreateTemp(base, ".setting-")
	if e != nil {
		return e
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, e = tmp.Write(raw); e != nil {
		return e
	}
	if e = tmp.Sync(); e != nil {
		return e
	}
	if e = tmp.Close(); e != nil {
		return e
	}
	return os.Rename(tmp.Name(), filepath.Join(base, name))
}
func getAppearance() stationAppearance {
	profileMu.Lock()
	defer profileMu.Unlock()
	q := stationAppearance{Variant: "dark"}
	b, _ := os.ReadFile(filepath.Join(base, "appearance.json"))
	json.Unmarshal(b, &q)
	if q.Variant != "light" {
		q.Variant = "dark"
	}
	return q
}
func stationAppearanceAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		response(w, getAppearance())
		return
	}
	if r.Method != "POST" {
		fail(w, "Нужен GET или POST", 405)
		return
	}
	var q stationAppearance
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if json.NewDecoder(r.Body).Decode(&q) != nil || (q.Variant != "dark" && q.Variant != "light") {
		fail(w, "Выберите светлую или тёмную Станцию Макс", 400)
		return
	}
	profileMu.Lock()
	e := writePrivateJSON("appearance.json", q)
	profileMu.Unlock()
	if e != nil {
		fail(w, "Не удалось сохранить цвет станции", 500)
		return
	}
	response(w, q)
}

// Profiles belong to an existing paired client. Telegram never grants station access.
func profileSubject(r *http.Request) string {
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if hmac.Equal([]byte(key), []byte(token)) {
		return "owner"
	}
	digest := sha256.Sum256([]byte(key))
	hash := hex.EncodeToString(digest[:])
	pairMu.Lock()
	defer pairMu.Unlock()
	for _, c := range pairedClients {
		if hmac.Equal([]byte(hash), []byte(c.Hash)) {
			return c.ID
		}
	}
	return ""
}
func validProfileName(s string) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) < 1 || utf8.RuneCountInString(s) > 48 {
		return false
	}
	for _, c := range s {
		if unicode.IsControl(c) || unicode.Is(unicode.Cf, c) {
			return false
		}
	}
	return true
}
func readProfile(subject string) localProfile {
	q := localProfile{Name: "Мой профиль", Accent: "violet"}
	b, _ := os.ReadFile(filepath.Join(base, "profiles.json"))
	all := map[string]localProfile{}
	json.Unmarshal(b, &all)
	if x, ok := all[subject]; ok {
		q = x
	}
	return q
}
func storeProfile(subject string, q localProfile) error {
	b, _ := os.ReadFile(filepath.Join(base, "profiles.json"))
	all := map[string]localProfile{}
	if len(b) > 0 && json.Unmarshal(b, &all) != nil {
		return fmt.Errorf("Повреждён файл профилей")
	}
	if len(all) >= 64 {
		if _, ok := all[subject]; !ok {
			return fmt.Errorf("Достигнут предел профилей")
		}
	}
	all[subject] = q
	return writePrivateJSON("profiles.json", all)
}
func profileAPI(w http.ResponseWriter, r *http.Request) {
	subject := profileSubject(r)
	if subject == "" {
		fail(w, "Подключите устройство по коду станции", 401)
		return
	}
	profileMu.Lock()
	defer profileMu.Unlock()
	if r.Method == "GET" {
		response(w, map[string]any{"profile": readProfile(subject), "local": true, "telegramAvailable": telegramConfigured()})
		return
	}
	if r.Method != "POST" {
		fail(w, "Нужен GET или POST", 405)
		return
	}
	var in struct {
		Name   string `json:"name"`
		Accent string `json:"accent"`
		Action string `json:"action"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		fail(w, "Неверный профиль", 400)
		return
	}
	q := readProfile(subject)
	if in.Action == "unlink-telegram" {
		q.Telegram = nil
	} else {
		in.Name = strings.TrimSpace(in.Name)
		if !validProfileName(in.Name) || (in.Accent != "violet" && in.Accent != "blue" && in.Accent != "rose" && in.Accent != "mint") {
			fail(w, "Имя — от 1 до 48 символов. Выберите цвет профиля", 400)
			return
		}
		q.Name = in.Name
		q.Accent = in.Accent
	}
	if e := storeProfile(subject, q); e != nil {
		fail(w, "Не удалось сохранить профиль", 500)
		return
	}
	response(w, map[string]any{"ok": true, "profile": q})
}
