package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	xdraw "golang.org/x/image/draw"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"io"
	"net"
	"net/http"
	"time"
)

var matrixClient = &http.Client{Timeout: 45 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
	return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", "/data/local/tmp/station-panel/matrix.sock")
}}}

func matrixAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		fail(w, "Нужен GET или POST", 405)
		return
	}
	var data []byte
	var e error
	if r.Method == "POST" {
		displayFeedbackCancel.Store(true)
		musicControlMu.Lock()
		defer musicControlMu.Unlock()
		stopMusicVisualLocked()
		stopAmbientLocked()
		r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
		data, e = io.ReadAll(r.Body)
		if e != nil {
			fail(w, "Слишком много кадров", 400)
			return
		}
	}
	req, e := http.NewRequestWithContext(r.Context(), r.Method, "http://matrix/", bytes.NewReader(data))
	if e != nil {
		fail(w, e.Error(), 500)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	result, e := matrixClient.Do(req)
	if e != nil {
		fail(w, "Служба дисплея пока не готова: "+e.Error(), 503)
		return
	}
	defer result.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(result.StatusCode)
	io.Copy(w, io.LimitReader(result.Body, 8192))
}

// Validate GIF frame count and allocation sizes before DecodeAll allocates memory.
func checkGIF(data []byte) error {
	if len(data) < 13 || (string(data[:6]) != "GIF89a" && string(data[:6]) != "GIF87a") {
		return fmt.Errorf("Нужен GIF")
	}
	width, height := int(binary.LittleEndian.Uint16(data[6:8])), int(binary.LittleEndian.Uint16(data[8:10]))
	if width < 1 || height < 1 || width > 2048 || height > 2048 {
		return fmt.Errorf("GIF: размер до 2048×2048")
	}
	pos := 13
	if data[10]&0x80 != 0 {
		pos += 3 * (1 << ((data[10] & 7) + 1))
	}
	frames, pixels := 0, 0
	skipBlocks := func() bool {
		for pos < len(data) {
			n := int(data[pos])
			pos++
			if n == 0 {
				return true
			}
			pos += n
			if pos > len(data) {
				return false
			}
		}
		return false
	}
	for pos < len(data) {
		marker := data[pos]
		pos++
		switch marker {
		case 0x3b:
			if frames == 0 {
				return fmt.Errorf("GIF без кадров")
			}
			return nil
		case 0x21:
			pos++
			if pos > len(data) || !skipBlocks() {
				return fmt.Errorf("Повреждённый GIF")
			}
		case 0x2c:
			if pos+9 > len(data) {
				return fmt.Errorf("Повреждённый GIF")
			}
			w, h := int(binary.LittleEndian.Uint16(data[pos+4:pos+6])), int(binary.LittleEndian.Uint16(data[pos+6:pos+8]))
			frames++
			pixels += w * h
			if frames > 120 || pixels > 4<<20 {
				return fmt.Errorf("GIF: до 120 кадров и 4 млн пикселей суммарно")
			}
			packed := data[pos+8]
			pos += 9
			if packed&0x80 != 0 {
				pos += 3 * (1 << ((packed & 7) + 1))
			}
			pos++
			if pos > len(data) || !skipBlocks() {
				return fmt.Errorf("Повреждённый GIF")
			}
		default:
			return fmt.Errorf("Неизвестный блок GIF")
		}
	}
	return fmt.Errorf("Незавершённый GIF")
}
func grayscaleFrame(src image.Image) []byte {
	out := make([]byte, 400)
	b := src.Bounds()
	w, h := 25, 16
	if b.Dx()*16 > b.Dy()*25 {
		h = b.Dy() * 25 / b.Dx()
		if h < 1 {
			h = 1
		}
	} else {
		w = b.Dx() * 16 / b.Dy()
		if w < 1 {
			w = 1
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, 25, 16))
	xdraw.CatmullRom.Scale(dst, image.Rect((25-w)/2, (16-h)/2, (25-w)/2+w, (16-h)/2+h), src, b, draw.Src, nil)
	for y := 0; y < 16; y++ {
		for x := 0; x < 25; x++ {
			v := color.GrayModel.Convert(dst.At(x, y)).(color.Gray)
			out[y*25+x] = v.Y
		}
	}
	return out
}
func matrixMedia(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		fail(w, "Нужен POST", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	data, e := io.ReadAll(r.Body)
	if e != nil {
		fail(w, "GIF до 8 МБ", 400)
		return
	}
	if e = checkGIF(data); e != nil {
		fail(w, e.Error(), 400)
		return
	}
	decoded, e := gif.DecodeAll(bytes.NewReader(data))
	if e != nil {
		fail(w, "Не удалось прочитать GIF", 400)
		return
	}
	canvas := image.NewRGBA(image.Rect(0, 0, decoded.Config.Width, decoded.Config.Height))
	frames := [][]byte{}
	delays := []int{}
	for i, frame := range decoded.Image {
		var previous *image.RGBA
		disposal := byte(0)
		if i < len(decoded.Disposal) {
			disposal = decoded.Disposal[i]
		}
		if disposal == gif.DisposalPrevious {
			previous = image.NewRGBA(canvas.Bounds())
			copy(previous.Pix, canvas.Pix)
		}
		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
		frames = append(frames, grayscaleFrame(canvas))
		delay := decoded.Delay[i] * 10
		if delay < 66 {
			delay = 100
		}
		if delay > 10000 {
			delay = 10000
		}
		delays = append(delays, delay)
		switch disposal {
		case gif.DisposalBackground:
			draw.Draw(canvas, frame.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			if previous != nil {
				canvas = previous
			}
		}
	}
	response(w, map[string]any{"ok": true, "frames": frames, "delays": delays, "width": 25, "height": 16})
}
