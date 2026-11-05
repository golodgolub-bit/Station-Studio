package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

type androidDNS struct {
	Servers  []string
	Private  bool
	Hostname string
}

var dnsMu sync.Mutex
var dnsCached androidDNS
var dnsUpdated time.Time
var activeNetworkPattern = regexp.MustCompile(`(?m)^Active default network:\s*(\d+)\s*$`)
var dnsAddressPattern = regexp.MustCompile(`DnsAddresses:\s*\[([^\]]*)\]`)
var privateDNSNamePattern = regexp.MustCompile(`PrivateDnsServerName:\s*([^\s}]+)`)
var dnsHostnamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,252}$`)

// Android's active LinkProperties are authoritative. Do not pick DNS from a
// disconnected network or the historical events appended to dumpsys output.
func parseAndroidDNS(raw string) (androidDNS, error) {
	var cfg androidDNS
	active := activeNetworkPattern.FindStringSubmatch(raw)
	if len(active) != 2 {
		return cfg, fmt.Errorf("Android не сообщил активную сеть")
	}
	for _, line := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "NetworkAgentInfo{") || !strings.Contains(line, "network{"+active[1]+"}") {
			continue
		}
		m := dnsAddressPattern.FindStringSubmatch(line)
		if len(m) != 2 {
			return cfg, fmt.Errorf("У активной сети нет DNS")
		}
		for _, candidate := range strings.FieldsFunc(m[1], func(r rune) bool { return r == ',' || r == ' ' }) {
			ip := net.ParseIP(strings.TrimPrefix(candidate, "/"))
			if ip != nil && !ip.IsUnspecified() {
				cfg.Servers = append(cfg.Servers, ip.String())
			}
		}
		if len(cfg.Servers) == 0 {
			return cfg, fmt.Errorf("Android не сообщил DNS-серверы")
		}
		cfg.Private = strings.Contains(line, "UsePrivateDns: true")
		if name := privateDNSNamePattern.FindStringSubmatch(line); len(name) == 2 && name[1] != "null" {
			if !dnsHostnamePattern.MatchString(name[1]) {
				return cfg, fmt.Errorf("Некорректное имя частного DNS")
			}
			cfg.Hostname = name[1]
		}
		return cfg, nil
	}
	return cfg, fmt.Errorf("Параметры активной сети Android недоступны")
}
func currentAndroidDNS() (androidDNS, error) {
	dnsMu.Lock()
	defer dnsMu.Unlock()
	if len(dnsCached.Servers) > 0 && time.Since(dnsUpdated) < 20*time.Second {
		return dnsCached, nil
	}
	out, e := run(5*time.Second, "/system/bin/dumpsys", "connectivity")
	if e != nil {
		return androidDNS{}, fmt.Errorf("Не удалось прочитать DNS активной сети")
	}
	cfg, e := parseAndroidDNS(out)
	if e != nil {
		return cfg, e
	}
	dnsCached = cfg
	dnsUpdated = time.Now()
	return cfg, nil
}
func initAndroidDNS() {
	if _, e := os.Stat("/system/bin/getprop"); e != nil {
		return
	}
	// GOOS=linux also omits Android's certificate directory. Use the device's
	// system trust store; certificate verification remains enabled everywhere.
	if os.Getenv("SSL_CERT_DIR") == "" {
		os.Setenv("SSL_CERT_DIR", "/system/etc/security/cacerts")
	}
	// This binary targets Linux without bionic/cgo. Its default resolver would
	// consult nonexistent /etc/resolv.conf and query ::1:53 on Android.
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		cfg, e := currentAndroidDNS()
		if e != nil {
			return nil, e
		}
		var last error
		for _, ip := range cfg.Servers {
			dialer := &net.Dialer{Timeout: 3 * time.Second}
			if cfg.Private {
				name := cfg.Hostname
				if name == "" {
					name = ip
				}
				tlsDialer := &tls.Dialer{NetDialer: dialer, Config: &tls.Config{ServerName: name, MinVersion: tls.VersionTLS12}}
				c, err := tlsDialer.DialContext(ctx, "tcp", net.JoinHostPort(ip, "853"))
				if err == nil {
					return c, nil
				}
				last = err
			} else {
				c, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip, "53"))
				if err == nil {
					return c, nil
				}
				last = err
			}
		}
		// A configured private DNS failure never falls back to plaintext/public DNS.
		return nil, fmt.Errorf("DNS активной сети не ответил: %v", last)
	}}
}
