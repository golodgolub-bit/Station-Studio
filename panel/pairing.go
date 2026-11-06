package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type pairedClient struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Hash    string `json:"hash"`
	Created int64  `json:"created"`
}

var pairMu sync.Mutex
var pairedClients = []pairedClient{}
var pairingCode string
var pairingUntil time.Time
var pairingAttempts int
var tlsFingerprint string

func accessValid(key string) bool {
	if len(key) != 48 {
		return false
	}
	if hmac.Equal([]byte(key), []byte(token)) {
		return true
	}
	digest := sha256.Sum256([]byte(key))
	hash := hex.EncodeToString(digest[:])
	pairMu.Lock()
	defer pairMu.Unlock()
	for _, client := range pairedClients {
		if hmac.Equal([]byte(hash), []byte(client.Hash)) {
			return true
		}
	}
	return false
}
func pairingMAC(key, value string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(value))
	return hex.EncodeToString(h.Sum(nil))
}
func loadPairing() error {
	b, _ := os.ReadFile(filepath.Join(base, "clients.json"))
	json.Unmarshal(b, &pairedClients)
	certPath, keyPath := filepath.Join(base, "tls.crt"), filepath.Join(base, "tls.key")
	if !fileExists(certPath) || !fileExists(keyPath) {
		key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			return e
		}
		serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		if e != nil {
			return e
		}
		now := time.Now()
		template := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Station Studio"}, NotBefore: now.Add(-24 * time.Hour), NotAfter: now.AddDate(10, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{"station-studio.local", "localhost"}}
		der, e := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
		if e != nil {
			return e
		}
		private, e := x509.MarshalECPrivateKey(key)
		if e != nil {
			return e
		}
		if e = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: private}), 0600); e != nil {
			return e
		}
		if e = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); e != nil {
			return e
		}
	}
	data, e := os.ReadFile(certPath)
	if e != nil {
		return e
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return fmt.Errorf("Invalid panel certificate")
	}
	digest := sha256.Sum256(block.Bytes)
	tlsFingerprint = hex.EncodeToString(digest[:])
	return nil
}
func saveClientsLocked() error {
	b, e := json.Marshal(pairedClients)
	if e == nil {
		e = os.WriteFile(filepath.Join(base, "clients.json.tmp"), b, 0600)
	}
	if e == nil {
		e = os.Rename(filepath.Join(base, "clients.json.tmp"), filepath.Join(base, "clients.json"))
	}
	return e
}
func pairingStartAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		fail(w, "Нужен POST", 405)
		return
	}
	pairMu.Lock()
	defer pairMu.Unlock()
	b := make([]byte, 10)
	if _, e := rand.Read(b); e != nil {
		fail(w, e.Error(), 500)
		return
	}
	pairingCode = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	pairingUntil = time.Now().Add(5 * time.Minute)
	pairingAttempts = 0
	response(w, map[string]any{"ok": true, "code": pairingCode, "expires": pairingUntil.Unix(), "tlsPort": 8789})
}
func pairingAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "POST" || r.TLS == nil {
		fail(w, "Подключение выполняется через защищённый порт", 400)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	var q struct {
		Nonce string `json:"nonce"`
		Proof string `json:"proof"`
		Name  string `json:"name"`
	}
	if json.NewDecoder(r.Body).Decode(&q) != nil || len(q.Nonce) != 32 || len(q.Name) < 1 || len([]rune(q.Name)) > 60 {
		fail(w, "Неверный запрос", 400)
		return
	}
	pairMu.Lock()
	defer pairMu.Unlock()
	if pairingCode == "" || time.Now().After(pairingUntil) || pairingAttempts >= 20 {
		fail(w, "Откройте новый код на станции", 409)
		return
	}
	pairingAttempts++
	expected := pairingMAC(pairingCode, q.Nonce+"|"+tlsFingerprint)
	if !hmac.Equal([]byte(expected), []byte(q.Proof)) {
		fail(w, "Код не подошёл", 403)
		return
	}
	if len(pairedClients) >= 30 {
		fail(w, "Сначала удалите старое подключение", 409)
		return
	}
	b := make([]byte, 24)
	rand.Read(b)
	key := hex.EncodeToString(b)
	hash := sha256.Sum256([]byte(key))
	client := pairedClient{randomID(), q.Name, hex.EncodeToString(hash[:]), time.Now().Unix()}
	pairedClients = append(pairedClients, client)
	if e := saveClientsLocked(); e != nil {
		pairedClients = pairedClients[:len(pairedClients)-1]
		fail(w, e.Error(), 500)
		return
	}
	proof := pairingMAC(pairingCode, q.Nonce+"|"+tlsFingerprint+"|"+key)
	pairingCode = ""
	response(w, map[string]any{"ok": true, "token": key, "clientId": client.ID, "proof": proof, "model": "Station Max", "version": 2})
	go happyConnectionFeedback()
}
func clientsAPI(w http.ResponseWriter, r *http.Request) {
	pairMu.Lock()
	defer pairMu.Unlock()
	if r.Method == "GET" {
		list := []map[string]any{}
		for _, c := range pairedClients {
			list = append(list, map[string]any{"id": c.ID, "name": c.Name, "created": c.Created})
		}
		response(w, map[string]any{"ok": true, "clients": list})
		return
	}
	if r.Method != "DELETE" {
		fail(w, "Метод не поддерживается", 405)
		return
	}
	id := r.URL.Query().Get("id")
	if !isMediaID(id) {
		fail(w, "Неверное подключение", 400)
		return
	}
	old := pairedClients
	next := []pairedClient{}
	for _, c := range old {
		if c.ID != id {
			next = append(next, c)
		}
	}
	pairedClients = next
	if e := saveClientsLocked(); e != nil {
		pairedClients = old
		fail(w, e.Error(), 500)
		return
	}
	response(w, map[string]bool{"ok": true})
}
func tlsAddress(listen string) string {
	if strings.HasPrefix(listen, ":") {
		return ":8789"
	}
	return "127.0.0.1:8789"
}
