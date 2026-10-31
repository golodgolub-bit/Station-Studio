package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Only fixed media words from the real SDK process are read. No microphone
// stream or arbitrary transcript is collected or saved by this worker.
var quickConnected atomic.Bool
var quickDetected, quickRouted, quickDuplicates atomic.Int64
var quickPairMu sync.Mutex

type quickPair struct {
	source string
	at     time.Time
}

var quickPairs = map[string][]quickPair{}

type musicVoiceJob struct {
	rule       voiceRule
	generation int64
}

var musicVoiceQueue = make(chan musicVoiceJob, 16)
var musicVoiceGeneration atomic.Int64
var musicVoiceOnce sync.Once

func dispatchMusicRule(rule voiceRule) {
	musicVoiceOnce.Do(func() {
		go func() {
			for job := range musicVoiceQueue {
				if job.generation == musicVoiceGeneration.Load() {
					runQuickRule(job.rule)
				}
			}
		}()
	})
	if rule.ID == "music-pause" {
		musicVoiceGeneration.Add(1)
		cancelMusicSearch()
		go runQuickRule(rule)
		return
	}
	select {
	case musicVoiceQueue <- musicVoiceJob{rule, musicVoiceGeneration.Load()}:
	default:
		scenarioMu.Lock()
		scenarioLast = "Слишком много команд подряд. Дождитесь ответа плеера"
		scenarioMu.Unlock()
	}
}

func quickVoiceStatus() map[string]any {
	return map[string]any{"connected": quickConnected.Load(), "detected": quickDetected.Load(), "routed": quickRouted.Load(), "duplicates": quickDuplicates.Load()}
}
func quickMediaPhrase(text string) (string, bool) {
	switch text {
	case "играй", "продолжай", "продолжи":
		return "продолжи", true
	case "пауза", "стоп", "хватит":
		return "пауза", true
	case "следующий", "следующая", "дальше", "вперед":
		return "следующий", true
	case "назад", "предыдущий", "предыдущая":
		return "назад", true
	}
	return "", false
}
func parseQuickLog(line, pid string, after, now time.Time) (string, string, bool) {
	fields := strings.Fields(line)
	if len(fields) < 7 || fields[1] != pid || fields[3] != "I" || fields[4] != "quark-native:" {
		return "", "", false
	}
	stamp, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return "", "", false
	}
	when := time.UnixMilli(int64(stamp * 1000))
	if !when.After(after) || now.Sub(when) > 2*time.Second || when.After(now.Add(500*time.Millisecond)) {
		return "", "", false
	}
	const marker = "] Command phrase spotted: "
	index := strings.Index(line, marker)
	if index < 0 || !strings.Contains(line[:index], "[command_spotter_capability.cpp:") {
		return "", "", false
	}
	phrase, ok := quickMediaPhrase(strings.TrimSpace(line[index+len(marker):]))
	if !ok {
		return "", "", false
	}
	return phrase, "quick:" + pid + ":" + fields[0], true
}

// Pair a native spotter event with its later ASR delivery. Repeated spotter
// events remain separate commands; neither channel can execute the pair twice.
func pairedQuickDuplicate(text, source string, now time.Time) bool {
	canonical, ok := quickMediaPhrase(text)
	if !ok {
		return false
	}
	quickPairMu.Lock()
	defer quickPairMu.Unlock()
	for key, list := range quickPairs {
		fresh := list[:0]
		for _, p := range list {
			if now.Sub(p.at) <= 2*time.Second {
				fresh = append(fresh, p)
			}
		}
		if len(fresh) == 0 {
			delete(quickPairs, key)
		} else {
			quickPairs[key] = fresh
		}
	}
	list := quickPairs[canonical]
	for i, p := range list {
		if p.source != source {
			quickPairs[canonical] = append(list[:i], list[i+1:]...)
			return true
		}
	}
	if len(list) >= 8 {
		list = list[1:]
	}
	quickPairs[canonical] = append(list, quickPair{source, now})
	return false
}
func quickVoiceWorker() {
	for {
		func() {
			out, err := run(3*time.Second, "/system/bin/pidof", "com.yandex.io.sdk")
			pid := strings.TrimSpace(out)
			if err != nil || pid == "" || strings.ContainsAny(pid, " \n\r") {
				return
			}
			if _, err = strconv.Atoi(pid); err != nil {
				return
			}
			after := time.Now()
			cmd := exec.Command("/system/bin/sh", "-c", "exec \"$@\"", "station-quick-commands", "/system/bin/logcat", "--pid="+pid, "-v", "epoch", "-T", "1", "-s", "quark-native:I")
			cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
			stream, err := cmd.StdoutPipe()
			if err != nil {
				return
			}
			if cmd.Start() != nil {
				return
			}
			quickConnected.Store(true)
			defer quickConnected.Store(false)
			done := make(chan struct{})
			defer close(done)
			go func() {
				ticker := time.NewTicker(3 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-done:
						return
					case <-ticker.C:
						b, e := os.ReadFile("/proc/" + pid + "/cmdline")
						if e != nil || !bytes.HasPrefix(b, []byte("com.yandex.io.sdk\x00")) {
							cmd.Process.Kill()
							return
						}
					}
				}
			}()
			scanner := bufio.NewScanner(stream)
			scanner.Buffer(make([]byte, 2048), 65536)
			for scanner.Scan() {
				phrase, id, ok := parseQuickLog(scanner.Text(), pid, after, time.Now())
				if !ok {
					continue
				}
				quickDetected.Add(1)
				b, _ := json.Marshal(map[string]string{"kind": "quick", "text": phrase, "requestId": id})
				request, _ := http.NewRequest("POST", "http://local/event", bytes.NewReader(b))
				reply := &captureResponse{header: make(http.Header)}
				voiceEvent(reply, request)
			}
			cmd.Process.Kill()
			cmd.Wait()
		}()
		time.Sleep(3 * time.Second)
	}
}

func quickRule(query string, settings voiceSettings) (voiceRule, bool) {
	canonical, ok := quickMediaPhrase(query)
	if !ok || !settings.QuickCommands || !settings.BuiltinCommands {
		return voiceRule{}, false
	}
	for _, rule := range settings.Rules {
		if rule.Enabled {
			for _, phrase := range rule.Phrases {
				if normalizeVoicePhrase(phrase) == normalizeVoicePhrase(query) {
					return voiceRule{}, false
				}
			}
		}
	}
	// Let native alarm and speech handling retain the stop word. The normal
	// short-command guard also checks for another active media application.
	scenarioMu.Lock()
	speaking := lastVoiceState == "speaking"
	alarm := nativeAlarmActive
	scenarioMu.Unlock()
	if speaking || alarm {
		return voiceRule{}, false
	}
	rule, ok := builtInVoice(canonical, settings)
	return rule, ok && strings.HasPrefix(rule.ID, "music-")
}
func runQuickRule(rule voiceRule) {
	err := executeSteps(rule.Steps)
	scenarioMu.Lock()
	defer scenarioMu.Unlock()
	scenarioLast = "Команда «" + rule.Name + "» выполнена"
	if err != nil {
		scenarioLast = fmt.Sprintf("Команда «%s»: %v", rule.Name, err)
	}
}
