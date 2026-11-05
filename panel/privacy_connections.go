package main

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func decodeProcEndpoint(raw string) (string, int, bool) {
	p := strings.Split(raw, ":")
	if len(p) != 2 {
		return "", 0, false
	}
	b, e := hex.DecodeString(p[0])
	port, pe := strconv.ParseUint(p[1], 16, 16)
	if e != nil || pe != nil || (len(b) != 4 && len(b) != 16) {
		return "", 0, false
	}
	// proc uses native-endian 32-bit words, including each IPv6 word.
	for i := 0; i < len(b); i += 4 {
		b[i], b[i+3] = b[i+3], b[i]
		b[i+1], b[i+2] = b[i+2], b[i+1]
	}
	ip := net.IP(b)
	if ip.IsUnspecified() || ip.IsLoopback() {
		return "", 0, false
	}
	return ip.String(), int(port), true
}
func privacyConnectionsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		fail(w, "Нужен GET", 405)
		return
	}
	packages := map[string][]string{}
	out, _ := run(5*time.Second, "/system/bin/pm", "list", "packages", "-U")
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.HasPrefix(f[0], "package:") && strings.HasPrefix(f[1], "uid:") {
			uid := strings.TrimPrefix(f[1], "uid:")
			packages[uid] = append(packages[uid], strings.TrimPrefix(f[0], "package:"))
		}
	}
	rows := []map[string]any{}
	seen := map[string]bool{}
	for _, file := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		for _, line := range strings.Split(read(file), "\n") {
			f := strings.Fields(line)
			if len(f) < 10 || f[3] != "01" {
				continue
			}
			address, port, ok := decodeProcEndpoint(f[2])
			if !ok {
				continue
			}
			key := fmt.Sprint(f[7], "/", address, "/", port)
			if seen[key] {
				continue
			}
			seen[key] = true
			names := packages[f[7]]
			if len(names) == 0 {
				names = []string{"Системный процесс · UID " + f[7]}
			}
			rows = append(rows, map[string]any{"uid": f[7], "packages": names, "address": address, "port": port})
			if len(rows) >= 120 {
				break
			}
		}
	}
	response(w, map[string]any{"connections": rows, "note": "Открытые TCP-соединения на момент проверки. Адрес и приложение не раскрывают содержимое зашифрованного соединения и не доказывают слежку. Процессы с общим UID нельзя надёжно разделить по приложениям.", "capturedAt": time.Now().Unix()})
}
