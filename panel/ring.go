package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var ringFile *os.File
var ringBuffer []byte
var ringInfo [160]byte
var ringOffsets [3]int

func ringBegin() {
	if read("/sys/class/graphics/fb5/name") != "YNDX LED ring" {
		return
	}
	f, e := os.OpenFile("/dev/graphics/fb5", os.O_RDWR, 0)
	if e != nil {
		return
	}
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), 0x4600, uintptr(unsafe.Pointer(&ringInfo[0])))
	if err != 0 || binary.LittleEndian.Uint32(ringInfo[:4]) != 24 || binary.LittleEndian.Uint32(ringInfo[4:8]) != 1 || binary.LittleEndian.Uint32(ringInfo[24:28]) != 24 {
		f.Close()
		return
	}
	seen := map[int]bool{}
	for channel, at := range []int{32, 44, 56} {
		offset := int(binary.LittleEndian.Uint32(ringInfo[at : at+4]))
		bits := binary.LittleEndian.Uint32(ringInfo[at+4 : at+8])
		if (offset != 0 && offset != 8 && offset != 16) || bits != 8 || seen[offset] {
			f.Close()
			return
		}
		seen[offset] = true
		ringOffsets[channel] = offset / 8
	}
	data, e := syscall.Mmap(int(f.Fd()), 0, 4096, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if e != nil {
		f.Close()
		return
	}
	ringFile = f
	ringBuffer = data
	// Direct sysfs writes detach this trigger. Reattach it before using fb5,
	// otherwise framebuffer animation runs but no LED changes physically.
	for n := 1; n <= 24; n++ {
		for _, color := range []string{"red", "green", "blue"} {
			os.WriteFile(fmt.Sprintf("/sys/class/leds/station2:%s:ring%d/trigger", color, n), []byte("led_ring_fb"), 0644)
		}
	}
}
func ringPixel(n int, r, g, b float64) bool {
	if ringBuffer == nil || n < 1 || n > 24 {
		return false
	}
	offset := (n - 1) * 3
	ringBuffer[offset+ringOffsets[0]] = byte(r)
	ringBuffer[offset+ringOffsets[1]] = byte(g)
	ringBuffer[offset+ringOffsets[2]] = byte(b)
	return true
}
func ringFlush() {
	if ringFile != nil {
		syscall.Syscall(syscall.SYS_IOCTL, ringFile.Fd(), 0x4606, uintptr(unsafe.Pointer(&ringInfo[0])))
	}
}
func ringEnd() bool {
	if ringBuffer == nil {
		return false
	}
	for i := 0; i < 72; i++ {
		ringBuffer[i] = 0
	}
	ringFlush()
	syscall.Munmap(ringBuffer)
	ringFile.Close()
	ringBuffer = nil
	ringFile = nil
	return true
}
