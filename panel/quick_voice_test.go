package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestQuickSpotterAcceptsOnlyFreshSDKMediaEvents(t *testing.T) {
	now := time.Unix(1793476201, 500000000)
	line := "1793476201.240 4415 5146 I quark-native: [4415:5146] [sdk] [info] [command_spotter_capability.cpp:309] Command phrase spotted: назад"
	phrase, id, ok := parseQuickLog(line, "4415", now.Add(-time.Second), now)
	if !ok || phrase != "назад" || id != "quick:4415:1793476201.240" {
		t.Fatal(phrase, id, ok)
	}
	for _, bad := range []string{strings.Replace(line, " 4415 ", " 9999 ", 1), strings.Replace(line, "quark-native:", "other:", 1), strings.Replace(line, "command_spotter_capability.cpp:", "another.cpp:", 1), strings.Replace(line, "назад", "включи свет", 1), strings.Replace(line, "1793476201.240", "1793476101.240", 1), strings.Replace(line, "1793476201.240", "1793476301.240", 1)} {
		if _, _, ok := parseQuickLog(bad, "4415", now.Add(-time.Second), now); ok {
			t.Fatal("Non SDK/fresh media event accepted:", bad)
		}
	}
	if _, _, ok := parseQuickLog(line, "4415", now, now); ok {
		t.Fatal("Old log replayed after worker restart")
	}
}
func TestQuickASRPairExecutesOnceInEitherOrder(t *testing.T) {
	defer func() { quickPairs = map[string][]quickPair{} }()
	for _, order := range [][2]string{{"quick", "asr"}, {"asr", "quick"}} {
		quickPairs = map[string][]quickPair{}
		now := time.Now()
		if pairedQuickDuplicate("следующий", order[0], now) {
			t.Fatal("First event skipped")
		}
		if !pairedQuickDuplicate("следующий", order[1], now.Add(100*time.Millisecond)) {
			t.Fatal("Pair executed twice")
		}
		if pairedQuickDuplicate("следующий", order[0], now.Add(200*time.Millisecond)) {
			t.Fatal("Intentional repeat skipped")
		}
	}
	quickPairs = map[string][]quickPair{}
	now := time.Now()
	for i := 0; i < 2; i++ {
		if pairedQuickDuplicate("назад", "quick", now.Add(time.Duration(i)*time.Millisecond)) {
			t.Fatal("Repeat skipped")
		}
	}
	for i := 0; i < 2; i++ {
		if !pairedQuickDuplicate("назад", "asr", now.Add(time.Duration(i+10)*time.Millisecond)) {
			t.Fatal("Repeated pair executed twice")
		}
	}
	if pairedQuickDuplicate("назад", "asr", now.Add(3*time.Second)) {
		t.Fatal("Stale pair suppressed new command")
	}
	if pairedQuickDuplicate("выключи свет", "quick", now) {
		t.Fatal("Household intent entered quick-media pairing")
	}
}
func TestCancelledSearchDoesNotLaunchPlayer(t *testing.T) {
	before := musicSearchGeneration.Load()
	defer musicSearchGeneration.Store(before)
	musicSearchGeneration.Store(42)
	err := playMusicQueryGeneration("a song", 41)
	if err == nil || !strings.Contains(err.Error(), "отменён") {
		t.Fatal("Cancelled search was allowed to launch:", err)
	}
}
func TestNativeQuickPriorityAndOwnScenarios(t *testing.T) {
	settings := voiceSettings{BuiltinCommands: true, QuickCommands: true, Rules: []voiceRule{{Enabled: true, Phrases: []string{"Алиса, пауза"}}}}
	if _, ok := quickRule("пауза", settings); ok {
		t.Fatal("Quick media overrode own scenario")
	}
	for _, state := range []string{"speaking", "idle"} {
		oldState, oldAlarm := lastVoiceState, nativeAlarmActive
		lastVoiceState = state
		nativeAlarmActive = true
		_, ok := quickRule("пауза", voiceSettings{BuiltinCommands: true, QuickCommands: true})
		lastVoiceState, nativeAlarmActive = oldState, oldAlarm
		if ok {
			t.Fatal(fmt.Sprintf("Quick media stole alarm stop in %s", state))
		}
	}
}
