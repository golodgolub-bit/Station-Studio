// Package stockdiscovery reads the station's existing _yandexio announcement.
// A stock announcement is evidence of presence, never evidence of root/ADB.
package stockdiscovery

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

type Station struct {
	Host     string `json:"host"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	DeviceID string `json:"deviceId"`
	Port     int    `json:"glagolPort"`
	Panel    bool   `json:"panel"`
}
type record struct {
	name   string
	kind   uint16
	data   []byte
	offset int
}

func nameAt(packet []byte, offset int) (string, int, error) {
	labels := []string{}
	cursor, end, hops := offset, -1, 0
	for {
		if cursor < 0 || cursor >= len(packet) || hops > 32 {
			return "", 0, fmt.Errorf("invalid DNS name")
		}
		hops++
		n := int(packet[cursor])
		cursor++
		if n == 0 {
			if end < 0 {
				end = cursor
			}
			return strings.ToLower(strings.Join(labels, ".")) + ".", end, nil
		}
		if n&0xc0 == 0xc0 {
			if cursor >= len(packet) {
				return "", 0, fmt.Errorf("short DNS pointer")
			}
			if end < 0 {
				end = cursor + 1
			}
			cursor = ((n & 63) << 8) | int(packet[cursor])
			continue
		}
		if n > 63 || cursor+n > len(packet) || len(labels) > 32 {
			return "", 0, fmt.Errorf("invalid DNS label")
		}
		labels = append(labels, string(packet[cursor:cursor+n]))
		cursor += n
	}
}
func records(packet []byte) ([]record, error) {
	if len(packet) < 12 || len(packet) > 9000 || binary.BigEndian.Uint16(packet[2:4])&0x8000 == 0 {
		return nil, fmt.Errorf("not a DNS response")
	}
	q := int(binary.BigEndian.Uint16(packet[4:6]))
	count := 0
	for i := 6; i < 12; i += 2 {
		count += int(binary.BigEndian.Uint16(packet[i : i+2]))
	}
	if q > 64 || count > 256 {
		return nil, fmt.Errorf("too many DNS records")
	}
	p := 12
	for i := 0; i < q; i++ {
		_, next, e := nameAt(packet, p)
		if e != nil || next+4 > len(packet) {
			return nil, fmt.Errorf("short DNS question")
		}
		p = next + 4
	}
	out := []record{}
	for i := 0; i < count; i++ {
		name, next, e := nameAt(packet, p)
		if e != nil || next+10 > len(packet) {
			return nil, fmt.Errorf("short DNS record")
		}
		kind := binary.BigEndian.Uint16(packet[next : next+2])
		size := int(binary.BigEndian.Uint16(packet[next+8 : next+10]))
		p = next + 10
		if p+size > len(packet) {
			return nil, fmt.Errorf("short DNS data")
		}
		out = append(out, record{name, kind, packet[p : p+size], p})
		p += size
	}
	return out, nil
}

type inventory struct {
	services  map[string]*Station
	targets   map[string]string
	addresses map[string]string
}

func newInventory() *inventory {
	return &inventory{map[string]*Station{}, map[string]string{}, map[string]string{}}
}
func (inv *inventory) ingest(packet []byte) {
	rs, e := records(packet)
	if e != nil {
		return
	}
	for _, r := range rs {
		if r.kind == 1 && len(r.data) == 4 {
			ip := net.IP(r.data)
			if ip.IsPrivate() {
				inv.addresses[r.name] = ip.String()
			}
		}
		if !strings.HasSuffix(r.name, "._yandexio._tcp.local.") {
			continue
		}
		s := inv.services[r.name]
		if s == nil {
			s = &Station{Name: strings.TrimSuffix(r.name, "._yandexio._tcp.local.")}
			inv.services[r.name] = s
		}
		switch r.kind {
		case 33:
			if len(r.data) >= 7 {
				target, _, e := nameAt(packet, r.offset+6)
				if e == nil {
					s.Port = int(binary.BigEndian.Uint16(r.data[4:6]))
					inv.targets[r.name] = target
				}
			}
		case 16:
			for p := 0; p < len(r.data); {
				n := int(r.data[p])
				p++
				if p+n > len(r.data) {
					break
				}
				parts := strings.SplitN(string(r.data[p:p+n]), "=", 2)
				p += n
				if len(parts) != 2 || len(parts[1]) > 128 {
					continue
				}
				switch parts[0] {
				case "platform":
					s.Platform = parts[1]
				case "deviceId":
					s.DeviceID = parts[1]
				}
			}
		}
	}
}
func (inv *inventory) result() []Station {
	out := []Station{}
	seen := map[string]bool{}
	for key, s := range inv.services {
		ip := inv.addresses[inv.targets[key]]
		if ip == "" || s.Port < 1 || seen[ip] {
			continue
		}
		s.Host = ip
		seen[ip] = true
		out = append(out, *s)
	}
	return out
}
func query() []byte {
	b := make([]byte, 12)
	b[5] = 1
	for _, part := range strings.Split("_yandexio._tcp.local", ".") {
		b = append(b, byte(len(part)))
		b = append(b, []byte(part)...)
	}
	return append(b, 0, 0, 12, 0, 1)
}
func Discover(ctx context.Context) []Station {
	inv := newInventory()
	var mu sync.Mutex
	var wg sync.WaitGroup
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		private := false
		for _, a := range addrs {
			ip, _, _ := net.ParseCIDR(a.String())
			if ip.To4() != nil && ip.IsPrivate() {
				private = true
			}
		}
		if !private {
			continue
		}
		conn, e := net.ListenMulticastUDP("udp4", &iface, &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353})
		if e != nil {
			continue
		}
		wg.Add(1)
		go func(c *net.UDPConn) {
			defer wg.Done()
			defer c.Close()
			until := time.Now().Add(4500 * time.Millisecond)
			next := time.Time{}
			b := make([]byte, 9000)
			for ctx.Err() == nil && time.Now().Before(until) {
				now := time.Now()
				if !now.Before(next) {
					c.WriteToUDP(query(), &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353})
					next = now.Add(time.Second)
				}
				c.SetReadDeadline(time.Now().Add(350 * time.Millisecond))
				n, peer, e := c.ReadFromUDP(b)
				if e != nil {
					if t, ok := e.(net.Error); ok && t.Timeout() {
						continue
					}
					return
				}
				if !peer.IP.IsPrivate() {
					continue
				}
				mu.Lock()
				inv.ingest(b[:n])
				mu.Unlock()
			}
		}(conn)
	}
	wg.Wait()
	return inv.result()
}
