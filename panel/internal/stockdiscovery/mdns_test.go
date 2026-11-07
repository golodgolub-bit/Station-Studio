package stockdiscovery

import (
	"encoding/binary"
	"testing"
)

func encodedName(name string) []byte {
	out := []byte{}
	start := 0
	for i := 0; i < len(name); i++ {
		if name[i] == '.' {
			out = append(out, byte(i-start))
			out = append(out, []byte(name[start:i])...)
			start = i + 1
		}
	}
	return append(out, 0)
}
func TestStockStationDoesNotNeedRootOrAliceKit(t *testing.T) {
	packet := make([]byte, 12)
	packet[2] = 0x84
	packet[7] = 3
	add := func(name string, kind uint16, data []byte) {
		packet = append(packet, encodedName(name)...)
		meta := make([]byte, 10)
		binary.BigEndian.PutUint16(meta, kind)
		binary.BigEndian.PutUint16(meta[2:4], 1)
		binary.BigEndian.PutUint16(meta[8:10], uint16(len(data)))
		packet = append(packet, meta...)
		packet = append(packet, data...)
	}
	service := "station._yandexio._tcp.local."
	target := "station.local."
	srv := make([]byte, 6)
	binary.BigEndian.PutUint16(srv[4:6], 1961)
	srv = append(srv, encodedName(target)...)
	add(service, 33, srv)
	txt := []byte{}
	for _, s := range []string{"platform=yandexstation_2", "deviceId=fixture"} {
		txt = append(txt, byte(len(s)))
		txt = append(txt, []byte(s)...)
	}
	add(service, 16, txt)
	add(target, 1, []byte{192, 168, 50, 42})
	inv := newInventory()
	inv.ingest(packet)
	stations := inv.result()
	if len(stations) != 1 || stations[0].Host != "192.168.50.42" || stations[0].Platform != "yandexstation_2" || stations[0].Panel {
		t.Fatal(stations)
	}
}
func TestDNSMalformedCompressionAndPublicTargetsAreRejected(t *testing.T) {
	if _, _, e := nameAt([]byte{0xc0, 0}, 0); e == nil {
		t.Fatal("pointer loop accepted")
	}
	if _, e := records([]byte{0, 1, 2}); e == nil {
		t.Fatal("short packet accepted")
	}
	packet := make([]byte, 12)
	packet[2] = 0x80
	packet[6] = 255
	packet[7] = 255
	if _, e := records(packet); e == nil {
		t.Fatal("unbounded record count accepted")
	}
	inv := newInventory()
	inv.ingest(packet)
	if len(inv.result()) != 0 {
		t.Fatal("malformed packet created a device")
	}
}
