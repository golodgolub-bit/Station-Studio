package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

type musicBufferCandidate struct {
	Offset, Capacity, Rate int
	Score                  float64
}

// Android 9 stereo PCM16 AudioTrack only. Every pointer is bounded by the
// named AudioFlinger mapping and must demonstrably advance between snapshots.
func musicBufferCandidates(a, b []byte) []musicBufferCandidate {
	out := []musicBufferCandidate{}
	if len(a) != len(b) || len(a) > 4<<20 {
		return out
	}
	for i := 0; i+0xd0 < len(a); i += 16 {
		u := func(data []byte, o int) uint32 { return binary.LittleEndian.Uint32(data[i+o:]) }
		rate, frames := int(u(b, 0x14)), int(u(b, 0xa8))
		if (rate != 44100 && rate != 48000) || frames < 128 || frames > 65536 {
			continue
		}
		capacity := 1
		for capacity < frames {
			capacity *= 2
		}
		if i+0xd0+capacity*4 > len(a) {
			continue
		}
		delta := u(b, 0) - u(a, 0)
		front, rear := u(b, 0xb0), u(b, 0xb4)
		if delta < 1000 || delta > 24000 || rear-front > uint32(capacity) || front-u(b, 0) > 4096 {
			continue
		}
		if front == u(a, 0xb0) {
			continue
		}
		count := 256
		if capacity < count {
			count = capacity
		}
		sum := 0.0
		for j := 0; j < count; j++ {
			at := i + 0xd0 + int((front-uint32(j+1))&uint32(capacity-1))*4
			v := float64(int16(binary.LittleEndian.Uint16(b[at:]))) / 32768
			sum += v * v
		}
		out = append(out, musicBufferCandidate{i, capacity, rate, math.Sqrt(sum / float64(count))})
	}
	return out
}

func discoverMusicBuffers(pid, maps string) (*musicTap, error) {
	type mapping struct {
		start int64
		a, b  []byte
	}
	blocks := []mapping{}
	for _, line := range strings.Split(maps, "\n") {
		if !strings.Contains(line, "/dev/ashmem/AudioFlinger::Client("+pid+")") || !strings.Contains(line, "rw-s") {
			continue
		}
		parts := strings.Split(strings.Fields(line)[0], "-")
		if len(parts) != 2 {
			continue
		}
		start, e := strconv.ParseInt(parts[0], 16, 64)
		end, e2 := strconv.ParseInt(parts[1], 16, 64)
		if e != nil || e2 != nil || end <= start || end-start > 4<<20 || len(blocks) >= 4 {
			continue
		}
		blocks = append(blocks, mapping{start: start, a: make([]byte, end-start), b: make([]byte, end-start)})
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("У плеера пока нет поддерживаемого звукового буфера")
	}
	f, e := os.Open("/proc/" + pid + "/mem")
	if e != nil {
		return nil, e
	}
	for i := range blocks {
		m := &blocks[i]
		if n, _ := f.ReadAt(m.a, m.start); n != len(m.a) {
			m.a = nil
		}
	}
	time.Sleep(160 * time.Millisecond)
	var best *musicTap
	score := -1.0
	for i := range blocks {
		m := &blocks[i]
		if len(m.a) == 0 {
			continue
		}
		if n, _ := f.ReadAt(m.b, m.start); n != len(m.b) {
			continue
		}
		for _, c := range musicBufferCandidates(m.a, m.b) {
			// Crossfades can expose two valid streams. Use the strongest moving PCM
			// track belonging to this selected app instead of refusing both forever.
			if c.Score > score {
				score = c.Score
				best = &musicTap{f: f, address: m.start + int64(c.Offset), buffer: m.start + int64(c.Offset) + 0xd0, capacity: c.Capacity, rate: c.Rate, progress: time.Now(), pid: pid}
			}
		}
	}
	if best == nil {
		f.Close()
		return nil, fmt.Errorf("Ожидаем воспроизведение в выбранном плеере")
	}
	return best, nil
}
