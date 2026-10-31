package main

// Native remoting framing and early-final ASR timing were verified against
// klockky/spotiya src/voice.rs (MIT). No audio capture is requested.
import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

var nativeIPCConnected atomic.Bool
var nativeIPCFrames atomic.Int64
var nativeIPCFinals atomic.Int64
var nativeIPCCancels atomic.Int64

func nativeIPCStatus() map[string]any {
	return map[string]any{"connected": nativeIPCConnected.Load(), "frames": nativeIPCFrames.Load(), "finals": nativeIPCFinals.Load(), "cancels": nativeIPCCancels.Load()}
}

func ipcVar(b []byte, v uint64) []byte {
	for v >= 128 {
		b = append(b, byte(v)|128)
		v >>= 7
	}
	return append(b, byte(v))
}
func ipcNumber(b []byte, field, value uint64) []byte {
	b = ipcVar(b, field<<3)
	return ipcVar(b, value)
}
func ipcBytes(b []byte, field uint64, value []byte) []byte {
	b = ipcVar(b, field<<3|2)
	b = ipcVar(b, uint64(len(value)))
	return append(b, value...)
}

type ipcField struct {
	number uint64
	data   []byte
	value  uint64
	wire   uint64
}

func ipcFields(data []byte) ([]ipcField, error) {
	out := []ipcField{}
	for len(data) > 0 {
		tag, n := binary.Uvarint(data)
		if n <= 0 || tag>>3 == 0 {
			return nil, fmt.Errorf("invalid native tag")
		}
		data = data[n:]
		f := ipcField{number: tag >> 3, wire: tag & 7}
		switch f.wire {
		case 0:
			v, n := binary.Uvarint(data)
			if n <= 0 {
				return nil, fmt.Errorf("invalid native integer")
			}
			f.value = v
			data = data[n:]
		case 2:
			size, n := binary.Uvarint(data)
			if n <= 0 || size > uint64(len(data)-n) {
				return nil, fmt.Errorf("invalid native bytes")
			}
			data = data[n:]
			f.data = data[:size]
			data = data[size:]
		case 1:
			if len(data) < 8 {
				return nil, io.ErrUnexpectedEOF
			}
			data = data[8:]
		case 5:
			if len(data) < 4 {
				return nil, io.ErrUnexpectedEOF
			}
			data = data[4:]
		default:
			return nil, fmt.Errorf("unsupported native wire")
		}
		out = append(out, f)
	}
	return out, nil
}
func ipcFind(b []byte, number uint64) ipcField {
	fields, e := ipcFields(b)
	if e != nil {
		return ipcField{}
	}
	var result ipcField
	for _, f := range fields {
		if f.number == number {
			if result.number != 0 {
				return ipcField{}
			}
			result = f
		}
	}
	return result
}
func nativeCancelFrame() []byte {
	method := []byte{8, 3}
	remoting := ipcBytes(nil, 7, []byte("AliceCapability"))
	remoting = ipcBytes(remoting, 8, method)
	envelope := ipcBytes(nil, 252, remoting)
	envelope = ipcNumber(envelope, 253, 210)
	out := binary.LittleEndian.AppendUint32(nil, uint32(len(envelope)))
	return append(out, envelope...)
}
func cancelNativeConversation() bool {
	c, e := net.DialTimeout("tcp4", "127.0.0.1:9875", 350*time.Millisecond)
	if e != nil {
		return false
	}
	defer c.Close()
	c.SetWriteDeadline(time.Now().Add(350 * time.Millisecond))
	b := nativeCancelFrame()
	for len(b) > 0 {
		n, e := c.Write(b)
		if e != nil || n <= 0 {
			return false
		}
		b = b[n:]
	}
	nativeIPCCancels.Add(1)
	return true
}
func handledVoice(w http.ResponseWriter) {
	response(w, map[string]bool{"ok": true, "handled": true, "nativeCancelled": false})
}
func earlyNativeRequest(data []byte) (string, string, string, bool) {
	if ipcFind(data, 253).value != 3 {
		return "", "", "", false
	}
	state := ipcFind(data, 252).data
	kind := ipcFind(state, 1).value
	if kind != 1 && kind != 3 {
		return "", "", "", false
	}
	result := ipcFind(state, 15).data
	if ipcFind(result, 2).value != 1 || ipcFind(result, 4).value != 1 {
		return "", "", "", false
	}
	text := ipcFind(result, 1).data
	if len(text) == 0 || len(text) > 1000 || !bytes.Equal(text, ipcFind(state, 2).data) {
		return "", "", "", false
	}
	id := string(ipcFind(state, 7).data)
	if len(id) != 36 {
		return "", "", "", false
	}
	status := "listening"
	if kind == 3 {
		status = "thinking"
	}
	return id, string(text), status, true
}
func nativeVoiceLoop() {
	for {
		func() {
			c, e := net.DialTimeout("tcp4", "127.0.0.1:9875", time.Second)
			if e != nil {
				return
			}
			defer c.Close()
			nativeIPCConnected.Store(true)
			defer nativeIPCConnected.Store(false)
			for {
				var header [4]byte
				if _, e = io.ReadFull(c, header[:]); e != nil {
					return
				}
				size := binary.LittleEndian.Uint32(header[:])
				if size == 0 || size > 10<<20 {
					return
				}
				data := make([]byte, size)
				if _, e = io.ReadFull(c, data); e != nil {
					return
				}
				nativeIPCFrames.Add(1)
				id, text, status, ok := earlyNativeRequest(data)
				if !ok {
					continue
				}
				nativeIPCFinals.Add(1)
				b, _ := json.Marshal(map[string]string{"requestId": id, "text": text, "state": status})
				r, _ := http.NewRequest("POST", "http://local/event", bytes.NewReader(b))
				out := &captureResponse{header: make(http.Header)}
				voiceEvent(out, r)
			}
		}()
		time.Sleep(3 * time.Second)
	}
}
func selectedMusicHasContext() bool {
	scenarioMu.Lock()
	alarm := nativeAlarmActive
	scenarioMu.Unlock()
	if alarm {
		return false
	}

	dump, _ := run(3*time.Second, "/system/bin/dumpsys", "media_session")
	pkg := selectedProvider()
	title, _, playing := sessionMetadata(dump, pkg)
	if title == "" {
		return false
	}
	if playing {
		return true
	}
	other := strings.Replace(dump, mediaSession(dump, pkg), "", 1)
	return !strings.Contains(other, "state=PlaybackState {state=3,")
}
