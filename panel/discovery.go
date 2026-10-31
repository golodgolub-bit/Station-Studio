package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"
)

// Broadcast only an irreversible identifier. The bearer key never leaves storage.
func discoveryInfo(nonce string) map[string]any {
	digest := sha256.Sum256([]byte(token))
	value := map[string]any{"id": hex.EncodeToString(digest[:16]), "port": 8787, "model": "Station Max", "version": 2, "tlsPort": 8789, "pairing": true}
	if b, e := hex.DecodeString(nonce); e == nil && len(b) == 16 {
		mac := hmac.New(sha256.New, []byte(token))
		mac.Write([]byte(nonce + "|8787"))
		value["proof"] = hex.EncodeToString(mac.Sum(nil))
	}
	return value
}
func discovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		fail(w, "Метод не поддерживается", 405)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response(w, discoveryInfo(r.URL.Query().Get("nonce")))
}
func discoveryLoop() {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: 8788})
	if err != nil {
		return
	}
	defer conn.Close()
	buf := make([]byte, 128)
	seen := map[string]time.Time{}
	for {
		n, peer, err := conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if !peer.IP.IsPrivate() && !peer.IP.IsLoopback() {
			continue
		}
		parts := strings.Split(string(buf[:n]), ":")
		if len(parts) != 2 || parts[0] != "STATION_STUDIO_DISCOVER_V1" {
			continue
		}
		nonce := parts[1]
		if b, e := hex.DecodeString(nonce); e != nil || len(b) != 16 {
			continue
		}
		now := time.Now()
		ip := peer.IP.String()
		if now.Sub(seen[ip]) < time.Second {
			continue
		}
		if len(seen) > 128 {
			seen = map[string]time.Time{}
		}
		seen[ip] = now
		reply, _ := json.Marshal(discoveryInfo(nonce))
		conn.WriteToUDP(reply, peer)
	}
}
