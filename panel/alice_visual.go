package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const aliceVisualDir = "/data/user/0/com.yandex.io.sdk/files/system/frontal_animation/25x16/oknix"

var aliceVisualSlots = map[string]string{"activation": "01-activation.gif", "listening": "03-listening-loop.gif", "thinking": "05-thinking-loop.gif", "speaking": "06-answer-loop.gif", "out": "00-out.gif"}

func replaceAliceAsset(path string, data []byte) error {
	info, e := os.Stat(path)
	if e != nil {
		return e
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("Не удалось определить владельца")
	}
	label := make([]byte, 512)
	n, e := syscall.Getxattr(path, "security.selinux", label)
	if e != nil {
		return e
	}
	temp := path + ".studio-new"
	f, e := os.OpenFile(temp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if e != nil {
		return e
	}
	defer os.Remove(temp)
	_, e = f.Write(data)
	if e == nil {
		e = f.Sync()
	}
	f.Close()
	if e != nil {
		return e
	}
	if e = os.Chown(temp, int(st.Uid), int(st.Gid)); e != nil {
		return e
	}
	if e = syscall.Setxattr(temp, "security.selinux", label[:n], 0); e != nil {
		return e
	}
	return os.Rename(temp, path)
}
func aliceVisualAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		slots := map[string]bool{}
		for key, file := range aliceVisualSlots {
			_, e := os.Stat(filepath.Join(base, "alice-visual-backup", file))
			slots[key] = e == nil
		}
		response(w, map[string]any{"slots": slots, "restartRequired": true})
		return
	}
	if r.Method != "POST" {
		fail(w, "Нужен POST", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	var q struct {
		Action string   `json:"action"`
		Slot   string   `json:"slot"`
		Frames [][]byte `json:"frames"`
		Delays []int    `json:"delays"`
	}
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		fail(w, "Некорректный запрос", 400)
		return
	}
	if q.Action == "restart" {
		if result, e := matrixClient.Post("http://matrix/", "application/json", bytes.NewBufferString(`{"action":"stop"}`)); e == nil {
			result.Body.Close()
		}
		out, e := run(12*time.Second, "/system/bin/am", "startservice", "-n", "com.yandex.io.sdk/com.yandex.tv.alice.app.KeepAliveService")
		if e != nil {
			fail(w, out, 500)
			return
		}
		// Kill only the exact SDK process; ActivityManager/KeepAlive restarts it.
		entries, _ := os.ReadDir("/proc")
		for _, entry := range entries {
			cmd, _ := os.ReadFile("/proc/" + entry.Name() + "/cmdline")
			if bytes.Equal(bytes.SplitN(cmd, []byte{0}, 2)[0], []byte("com.yandex.io.sdk")) {
				var pid int
				fmt.Sscan(entry.Name(), &pid)
				if pid > 1 {
					syscall.Kill(pid, syscall.SIGTERM)
				}
			}
		}
		response(w, map[string]bool{"ok": true})
		return
	}
	file, ok := aliceVisualSlots[q.Slot]
	if !ok {
		fail(w, "Выберите состояние Алисы", 400)
		return
	}
	path := filepath.Join(aliceVisualDir, file)
	backup := filepath.Join(base, "alice-visual-backup", file)
	libraryMu.Lock()
	defer libraryMu.Unlock()
	var data []byte
	var e error
	if q.Action == "restore" {
		data, e = os.ReadFile(backup)
		if e != nil {
			fail(w, "Для этого состояния нет сохранённой замены", 400)
			return
		}
	} else if q.Action == "set" {
		if len(q.Frames) < 1 || len(q.Frames) > 120 || len(q.Frames) != len(q.Delays) {
			fail(w, "До 120 кадров", 400)
			return
		}
		palette := make(color.Palette, 256)
		for i := range palette {
			palette[i] = color.Gray{uint8(i)}
		}
		animation := &gif.GIF{LoopCount: 0}
		for i, frame := range q.Frames {
			if len(frame) != 400 || q.Delays[i] < 66 || q.Delays[i] > 10000 {
				fail(w, "Нужны кадры 25×16", 400)
				return
			}
			im := image.NewPaletted(image.Rect(0, 0, 25, 16), palette)
			copy(im.Pix, frame)
			animation.Image = append(animation.Image, im)
			animation.Delay = append(animation.Delay, (q.Delays[i]+5)/10)
		}
		var b bytes.Buffer
		if e = gif.EncodeAll(&b, animation); e != nil {
			fail(w, e.Error(), 500)
			return
		}
		data = b.Bytes()
		if _, e = os.Stat(backup); os.IsNotExist(e) {
			original, err := os.ReadFile(path)
			if err != nil {
				fail(w, err.Error(), 500)
				return
			}
			os.MkdirAll(filepath.Dir(backup), 0700)
			if err = os.WriteFile(backup, original, 0600); err != nil {
				fail(w, err.Error(), 500)
				return
			}
		}
	} else {
		fail(w, "Неизвестное действие", 400)
		return
	}
	if e = replaceAliceAsset(path, data); e != nil {
		fail(w, e.Error(), 500)
		return
	}
	event("LED-анимация Алисы: " + q.Slot)
	response(w, map[string]any{"ok": true, "restartRequired": true})
}
