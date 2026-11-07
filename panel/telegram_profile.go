package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/skip2/go-qrcode"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Public verification key only. Bot and signing secrets remain on the project server.
const telegramProfilePublicKey = "EbMQe/K8whmjOmi/tGfSladhd3t83Wp81arn+GR+CQA="

var telegramProfileKey, _ = base64.StdEncoding.DecodeString(telegramProfilePublicKey)

const profileProofDomain = "AliceKit-Telegram-Profile-v1\x00"

type telegramAttempt struct {
	Subject, ID string
	Expires     int64
}

var telegramMu sync.Mutex
var telegramAttempts = map[string]*telegramAttempt{}

func telegramConfigured() bool { return len(telegramProfileKey) == ed25519.PublicKeySize }
func decodeTelegramProof(proof string, now time.Time) (telegramIdentity, string, error) {
	var q telegramIdentity
	invalid := fmt.Errorf("Подтверждение Telegram не подошло или истекло")
	parts := strings.Split(proof, ".")
	if len(proof) > 256 || len(parts) != 3 || parts[0] != "AK1" {
		return q, "", invalid
	}
	raw, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil || len(raw) < 35 || len(raw) > 115 {
		return q, "", invalid
	}
	signature, e := base64.RawURLEncoding.DecodeString(parts[2])
	message := append([]byte(profileProofDomain), raw...)
	if e != nil || len(telegramProfileKey) != ed25519.PublicKeySize || !ed25519.Verify(telegramProfileKey, message, signature) || raw[0] != 1 {
		return q, "", invalid
	}
	id := binary.BigEndian.Uint64(raw[17:25])
	issued := int64(binary.BigEndian.Uint32(raw[25:29]))
	expires := int64(binary.BigEndian.Uint32(raw[29:33]))
	if id == 0 || expires <= now.Unix() || issued > now.Unix()+60 || expires-issued > 300 || expires <= issued {
		return q, "", invalid
	}
	nameLen := int(raw[33])
	if nameLen > 48 || 34+nameLen >= len(raw) {
		return q, "", invalid
	}
	name := string(raw[34 : 34+nameLen])
	offset := 34 + nameLen
	usernameLen := int(raw[offset])
	if usernameLen > 32 || offset+1+usernameLen != len(raw) {
		return q, "", invalid
	}
	username := string(raw[offset+1:])
	if !utf8.ValidString(name) || !validProfileName(name) || !regexp.MustCompile(`^[A-Za-z0-9_]{0,32}$`).MatchString(username) {
		return q, "", invalid
	}
	q = telegramIdentity{ID: fmt.Sprint(id), Name: name, Username: username}
	return q, hex.EncodeToString(raw[1:17]), nil
}
func telegramProfileAPI(w http.ResponseWriter, r *http.Request) {
	subject := profileSubject(r)
	if subject == "" {
		fail(w, "Сначала подключите устройство к станции", 401)
		return
	}
	if r.Method == "POST" {
		var in struct {
			Action string `json:"action"`
			Proof  string `json:"proof"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 2048)
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			fail(w, "Неверный запрос входа", 400)
			return
		}
		if in.Action == "confirm" {
			identity, id, e := decodeTelegramProof(strings.TrimSpace(in.Proof), time.Now())
			if e != nil {
				fail(w, e.Error(), 400)
				return
			}
			telegramMu.Lock()
			a := telegramAttempts[id]
			if a == nil || a.Subject != subject || a.Expires < time.Now().UnixMilli() {
				telegramMu.Unlock()
				fail(w, "Этот вход не запрашивался на вашем устройстве или уже использован", 410)
				return
			}
			delete(telegramAttempts, id)
			telegramMu.Unlock()
			profileMu.Lock()
			q := readProfile(subject)
			q.Telegram = &identity
			if q.Name == "Мой профиль" {
				q.Name = identity.Name
			}
			e = storeProfile(subject, q)
			profileMu.Unlock()
			if e != nil {
				fail(w, "Не удалось сохранить профиль. Создайте новую ссылку входа", 500)
				return
			}
			response(w, map[string]any{"ok": true, "state": "confirmed", "profile": q})
			return
		}
		if in.Action != "" && in.Action != "start" {
			fail(w, "Неизвестное действие", 400)
			return
		}
		nonce := make([]byte, 16)
		if _, e := rand.Read(nonce); e != nil {
			fail(w, "Не удалось создать запрос входа", 500)
			return
		}
		id := hex.EncodeToString(nonce)
		expires := time.Now().Add(5 * time.Minute).UnixMilli()
		telegramMu.Lock()
		for key, a := range telegramAttempts {
			if a.Expires < time.Now().UnixMilli() {
				delete(telegramAttempts, key)
			}
		}
		if len(telegramAttempts) >= 64 {
			telegramMu.Unlock()
			fail(w, "Слишком много запросов входа", 429)
			return
		}
		telegramAttempts[id] = &telegramAttempt{Subject: subject, ID: id, Expires: expires}
		telegramMu.Unlock()
		link := "https://t.me/AliceKitBot?start=al_" + id
		qr, _ := qrcode.Encode(link, qrcode.Medium, 288)
		response(w, map[string]any{"ok": true, "id": id, "url": link, "expires": expires, "qr": qr, "method": "signed-return"})
		return
	}
	if r.Method != "GET" && r.Method != "DELETE" {
		fail(w, "Нужен GET, POST или DELETE", 405)
		return
	}
	id := r.URL.Query().Get("id")
	telegramMu.Lock()
	a := telegramAttempts[id]
	if a == nil || a.Subject != subject || a.Expires < time.Now().UnixMilli() {
		telegramMu.Unlock()
		fail(w, "Запрос входа истёк. Создайте новую ссылку", 410)
		return
	}
	if r.Method == "DELETE" {
		delete(telegramAttempts, id)
		telegramMu.Unlock()
		response(w, map[string]bool{"ok": true})
		return
	}
	telegramMu.Unlock()
	response(w, map[string]string{"state": "pending"})
}
