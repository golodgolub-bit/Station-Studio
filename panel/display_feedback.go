package main

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

var displayFeedbackUntil atomic.Int64
var displayFeedbackCancel atomic.Bool
var displayFeedbackControl sync.Mutex
var displayFeedbackDone chan struct{}

func stopDisplayFeedback() {
	displayFeedbackControl.Lock()
	done := displayFeedbackDone
	displayFeedbackCancel.Store(true)
	displayFeedbackControl.Unlock()
	if done != nil {
		<-done
	}
}

func displayFeedbackActive() bool { return displayFeedbackUntil.Load() != 0 }
func startDisplayFeedback(frames [][]byte, delays []int, duration, brightness int) error {
	if len(frames) == 0 || len(frames) > 80 || len(delays) != len(frames) || duration < 1 || duration > 8 || brightness < 1 || brightness > 100 {
		return fmt.Errorf("Предпросмотр: 1–80 кадров, 1–8 секунд, яркость 1–100")
	}
	for i, f := range frames {
		if len(f) != 400 || delays[i] < 66 || delays[i] > 2000 {
			return fmt.Errorf("Кадр должен содержать 400 точек, задержка 66–2000 мс")
		}
	}
	if assistantBusy() {
		return fmt.Errorf("Алиса занята. Повторите после ответа")
	}
	now := time.Now().UnixMilli()
	displayFeedbackControl.Lock()
	defer displayFeedbackControl.Unlock()
	old := displayFeedbackUntil.Load()
	if old != 0 || !displayFeedbackUntil.CompareAndSwap(0, now+int64(duration)*1000+250) {
		return fmt.Errorf("На дисплее уже идёт предпросмотр")
	}
	displayFeedbackCancel.Store(false)
	done := make(chan struct{})
	displayFeedbackDone = done
	go func() {
		defer close(done)
		defer displayFeedbackUntil.Store(0)
		time.Sleep(150 * time.Millisecond) // existing clients yield before the first frame
		deadline := time.Now().Add(time.Duration(duration) * time.Second)
		for i := 0; time.Now().Before(deadline); i = (i + 1) % len(frames) {
			if assistantBusy() || displayFeedbackCancel.Load() {
				break
			}
			if matrixPost(map[string]any{"action": "play", "frames": [][]byte{frames[i]}, "delays": []int{delays[i]}, "duration": 1, "brightness": brightness}) != nil {
				break
			}
			wait := time.Duration(delays[i]) * time.Millisecond
			if remaining := time.Until(deadline); wait > remaining {
				wait = remaining
			}
			if wait > 0 {
				time.Sleep(wait)
			}
		}
		_ = matrixPost(map[string]string{"action": "stop"})
	}()
	return nil
}
func happyConnectionFeedback() {
	f := make([]byte, 400)
	put := func(x, y int) {
		if x >= 0 && x < 25 && y >= 0 && y < 16 {
			f[y*25+x] = 200
		}
	}
	for _, cx := range []int{7, 17} {
		for x := -2; x <= 2; x++ {
			put(cx+x, 5+int(math.Abs(float64(x))*.6))
		}
	}
	for x := 6; x <= 18; x++ {
		put(x, 10+int(2*math.Sin(float64(x-6)*math.Pi/12)))
	}
	_ = startDisplayFeedback([][]byte{f}, []int{120}, 2, 70)
}
