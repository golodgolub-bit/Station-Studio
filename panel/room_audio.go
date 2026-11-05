package main

// Read-only observer of the existing AudioRecord shared buffer. Android 9
// AudioTrackShared.h/Tracks.cpp define the checked cblk layout (Apache-2.0).
// No AudioRecord/ALSA device is opened; no consumer cursor is advanced.
import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

type roomAudioTap struct {
	file     *os.File
	address  int64
	buffer   int64
	last     uint32
	progress time.Time
	levels   [7]float64
	wave     [512]float64
	bands    [16]float64
}

func openRoomAudioTapForPID(pid string) (*roomAudioTap, error) {
	b, _ := os.ReadFile("/proc/" + pid + "/maps")
	var start, end uint64
	for _, line := range strings.Split(string(b), "\n") {
		if strings.Contains(line, "/dev/ashmem/AudioFlinger::Client("+pid+")") && strings.Contains(line, "rw-s") {
			if start != 0 {
				return nil, fmt.Errorf("Неоднозначный буфер микрофонов")
			}
			p := strings.Split(strings.Fields(line)[0], "-")
			start, _ = strconv.ParseUint(p[0], 16, 64)
			end, _ = strconv.ParseUint(p[1], 16, 64)
		}
	}
	if start == 0 || end-start > 4<<20 {
		return nil, fmt.Errorf("Буфер микрофонов не найден")
	}
	f, e := os.Open("/proc/" + pid + "/mem")
	if e != nil {
		return nil, e
	}
	fail := func() (*roomAudioTap, error) {
		f.Close()
		return nil, fmt.Errorf("Формат звукового буфера не подтверждён")
	}
	a := make([]byte, end-start)
	z := make([]byte, end-start)
	if n, _ := f.ReadAt(a, int64(start)); n != len(a) {
		return fail()
	}
	time.Sleep(160 * time.Millisecond)
	if n, _ := f.ReadAt(z, int64(start)); n != len(z) {
		return fail()
	}
	found := -1
	for i := 0; i+0xd0+16384*32 <= len(a); i += 16 {
		u := func(b []byte, o int) uint32 { return binary.LittleEndian.Uint32(b[i+o:]) }
		delta := u(z, 0) - u(a, 0)
		if u(z, 0xa8) != 9216 || u(z, 0xc) != 3072 || u(z, 0x10) != 0xe000e000 || u(z, 0x14) != 0 || delta < 3072 || delta > 24576 {
			continue
		}
		rear, front := u(z, 0xb4), u(z, 0xb0)
		if rear-front > 9216 || rear-u(z, 0) > 3072 || u(z, 0xb8) != 0 || u(z, 0xc8) != 0 || u(z, 0xcc) != 0 {
			continue
		}
		if found >= 0 {
			return fail()
		}
		found = i
	}
	if found < 0 {
		return fail()
	}
	return &roomAudioTap{file: f, address: int64(start) + int64(found), buffer: int64(start) + int64(found) + 0xd0, progress: time.Now()}, nil
}
func (a *roomAudioTap) close() {
	if a != nil && a.file != nil {
		a.file.Close()
		a.file = nil
	}
	if a != nil {
		a.wave = [512]float64{}
		a.bands = [16]float64{}
		a.levels = [7]float64{}
	}
}
func (a *roomAudioTap) sample() error {
	h := make([]byte, 0xd0)
	if n, _ := a.file.ReadAt(h, a.address); n != len(h) || binary.LittleEndian.Uint32(h[0xa8:]) != 9216 || binary.LittleEndian.Uint32(h[0xc:]) != 3072 {
		return fmt.Errorf("Звуковой буфер изменился")
	}
	rear := binary.LittleEndian.Uint32(h[0xb4:])
	if rear != a.last {
		a.progress = time.Now()
		a.last = rear
	}
	if time.Since(a.progress) > 2*time.Second {
		return fmt.Errorf("Микрофонный поток остановлен")
	}
	// Read 1536 completed frames, decimate to 16 kHz for visual analysis only.
	const count = 1536
	data := make([]byte, count*32)
	position := (rear - count) & 16383
	first := count
	if int(position)+count > 16384 {
		first = 16384 - int(position)
	}
	if n, _ := a.file.ReadAt(data[:first*32], a.buffer+int64(position)*32); n != first*32 {
		return fmt.Errorf("Нет доступа к микрофонному буферу")
	}
	if first < count {
		if n, _ := a.file.ReadAt(data[first*32:], a.buffer); n != (count-first)*32 {
			return fmt.Errorf("Нет доступа к звуку")
		}
	}
	sums := [7]float64{}
	for i := 0; i < count; i += 3 {
		for ch := 0; ch < 7; ch++ {
			v := float64(int16(binary.LittleEndian.Uint16(data[i*32+ch*2:]))) / 32768
			sums[ch] += v * v
		}
	}
	best := 0
	for ch := 0; ch < 7; ch++ {
		a.levels[ch] = math.Sqrt(sums[ch] / 512)
		if sums[ch] > sums[best] {
			best = ch
		}
	}
	mean := 0.0
	for i := 0; i < 512; i++ {
		a.wave[i] = float64(int16(binary.LittleEndian.Uint16(data[i*3*32+best*2:]))) / 32768
		mean += a.wave[i]
	}
	mean /= 512
	frequencies := []float64{80, 120, 180, 270, 400, 600, 850, 1200, 1700, 2300, 3000, 3800, 4600, 5400, 6300, 7200}
	for band, freq := range frequencies {
		coefficient := 2 * math.Cos(2*math.Pi*freq/16000)
		s1, s2 := 0.0, 0.0
		for i, v := range a.wave {
			v = (v - mean) * (.5 - .5*math.Cos(2*math.Pi*float64(i)/511))
			s := v + coefficient*s1 - s2
			s2 = s1
			s1 = s
		}
		power := math.Sqrt(math.Max(0, s1*s1+s2*s2-coefficient*s1*s2)) / 128
		a.bands[band] = power
	}
	// Discard PCM after deriving local levels and a short in-memory waveform.
	clear(data)
	return nil
}

func openRoomAudioTap() (*roomAudioTap, error) {
	out, e := run(2*time.Second, "/system/bin/pidof", "com.yandex.io.sdk")
	pid := strings.TrimSpace(out)
	if e != nil || pid == "" || strings.ContainsAny(pid, " \r\n") {
		return nil, fmt.Errorf("Ожидаем службу микрофонов станции")
	}
	if _, e = strconv.Atoi(pid); e != nil {
		return nil, fmt.Errorf("Неверный процесс микрофонов")
	}
	return openRoomAudioTapForPID(pid)
}
func (a *roomAudioTap) musicSample() (musicLight, error) {
	s := musicLight{}
	if e := a.sample(); e != nil {
		return s, e
	}
	for _, v := range a.levels {
		if v > s.Level {
			s.Level = v
		}
	}
	s.Level *= 10
	for i, v := range a.bands {
		s.Bands[i] = v * 4
	}
	for x := 0; x < 25; x++ {
		for k := x * 512 / 25; k < (x+1)*512/25; k++ {
			v := a.wave[k] * 3
			if math.Abs(v) > math.Abs(s.Wave[x]) {
				s.Wave[x] = v
			}
		}
	}
	return s, nil
}
