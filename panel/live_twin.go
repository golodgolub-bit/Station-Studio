package main

import (
	"encoding/binary"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Read-only telemetry: no controller flags, audio recording, or sysfs writes.
type twinSnapshot struct {
	Variant          string   `json:"variant"`
	Timestamp        int64    `json:"timestamp"`
	Frame            []byte   `json:"frame,omitempty"`
	Ring             [][3]int `json:"ring,omitempty"`
	DisplayAvailable bool     `json:"displayAvailable"`
	RingAvailable    bool     `json:"ringAvailable"`
	Width            int      `json:"width"`
	Height           int      `json:"height"`
	Brightness       int      `json:"brightness"`
	Assistant        string   `json:"assistant"`
	DisplayError     string   `json:"displayError,omitempty"`
}

var twinMu sync.Mutex
var twinCached twinSnapshot
var twinProfileOnce sync.Once
var twinProfileSupported bool

func twinMonoFrame(raw []byte, w, h, bpp int) ([]byte, error) {
	if w != 25 || h != 16 || bpp != 24 || len(raw) != 1200 {
		return nil, fmt.Errorf("Матрица 25×16 RGB888 не подтверждена")
	}
	out := make([]byte, 400)
	for i := range out {
		out[i] = byte((int(raw[i*3])*299 + int(raw[i*3+1])*587 + int(raw[i*3+2])*114 + 500) / 1000)
	}
	return out, nil
}
func readTwinDisplay() ([]byte, error) {
	f, e := os.Open("/dev/graphics/fb4")
	if e != nil {
		return nil, fmt.Errorf("Буфер дисплея недоступен")
	}
	defer f.Close()
	var info [160]byte
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), 0x4600, uintptr(unsafe.Pointer(&info[0])))
	if errno != 0 || binary.LittleEndian.Uint32(info[:4]) != 25 || binary.LittleEndian.Uint32(info[4:8]) != 16 || binary.LittleEndian.Uint32(info[24:28]) != 24 {
		return nil, fmt.Errorf("Матрица этой прошивки не подтверждена")
	}
	raw := make([]byte, 1200)
	if n, _ := f.ReadAt(raw, 0); n != len(raw) {
		mapped, e := syscall.Mmap(int(f.Fd()), 0, 4096, syscall.PROT_READ, syscall.MAP_SHARED)
		if e != nil {
			return nil, fmt.Errorf("Чтение дисплея не поддерживается")
		}
		copy(raw, mapped[:1200])
		syscall.Munmap(mapped)
	}
	return twinMonoFrame(raw, 25, 16, 24)
}
func readTwinRing(root string) ([][3]int, bool) {
	ring := make([][3]int, 24)
	for i := range ring {
		for c, name := range []string{"red", "green", "blue"} {
			value, e := os.ReadFile(fmt.Sprintf("%s/station2:%s:ring%d/brightness", root, name, i+1))
			if e != nil {
				return nil, false
			}
			v, e := strconv.Atoi(strings.TrimSpace(string(value)))
			if e != nil || v < 0 || v > 255 {
				return nil, false
			}
			ring[i][c] = v
		}
	}
	return ring, true
}
func twinRingFrame(raw []byte, info []byte) ([][3]int, bool) {
	if len(raw) != 72 || len(info) < 64 || binary.LittleEndian.Uint32(info[:4]) != 24 || binary.LittleEndian.Uint32(info[4:8]) != 1 || binary.LittleEndian.Uint32(info[24:28]) != 24 {
		return nil, false
	}
	offsets := [3]int{}
	seen := map[int]bool{}
	for c, at := range []int{32, 44, 56} {
		offset := int(binary.LittleEndian.Uint32(info[at : at+4]))
		if (offset != 0 && offset != 8 && offset != 16) || binary.LittleEndian.Uint32(info[at+4:at+8]) != 8 || seen[offset] {
			return nil, false
		}
		seen[offset] = true
		offsets[c] = offset / 8
	}
	ring := make([][3]int, 24)
	for i := range ring {
		for c, offset := range offsets {
			ring[i][c] = int(raw[i*3+offset])
		}
	}
	return ring, true
}
func readLiveTwinRing() ([][3]int, bool) {
	// Sysfs brightness is stale while the native framebuffer trigger is active.
	if !strings.Contains(read("/sys/class/leds/station2:red:ring1/trigger"), "[led_ring_fb]") {
		return readTwinRing("/sys/class/leds")
	}
	if read("/sys/class/graphics/fb5/name") != "YNDX LED ring" {
		return nil, false
	}
	f, e := os.Open("/dev/graphics/fb5")
	if e != nil {
		return nil, false
	}
	defer f.Close()
	var info [160]byte
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), 0x4600, uintptr(unsafe.Pointer(&info[0])))
	if errno != 0 {
		return nil, false
	}
	raw := make([]byte, 72)
	if n, _ := f.ReadAt(raw, 0); n != 72 {
		mapped, e := syscall.Mmap(int(f.Fd()), 0, 4096, syscall.PROT_READ, syscall.MAP_SHARED)
		if e != nil {
			return nil, false
		}
		copy(raw, mapped[:72])
		syscall.Munmap(mapped)
	}
	return twinRingFrame(raw, info[:])
}
func liveTwinAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		fail(w, "Нужен GET", 405)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	twinMu.Lock()
	defer twinMu.Unlock()
	if time.Now().UnixMilli()-twinCached.Timestamp < 200 {
		response(w, twinCached)
		return
	}
	twinProfileOnce.Do(func() { twinProfileSupported = buildID() == "5000000921" })
	s := twinSnapshot{Variant: getAppearance().Variant, Timestamp: time.Now().UnixMilli(), Width: 25, Height: 16}
	if twinProfileSupported {
		var e error
		s.Frame, e = readTwinDisplay()
		s.DisplayAvailable = e == nil
		if e != nil {
			s.DisplayError = e.Error()
		}
		s.Ring, s.RingAvailable = readLiveTwinRing()
		s.Brightness, _ = strconv.Atoi(read("/sys/class/backlight/fb4/brightness"))
	} else {
		s.DisplayError = "Аппаратный предпросмотр этой прошивки пока не подтверждён"
	}
	if assistantBusy() {
		s.Assistant = "active"
	} else {
		s.Assistant = "idle"
	}
	twinCached = s
	response(w, s)
}
