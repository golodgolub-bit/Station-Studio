package main

import (
	"image"
	"image/color"
	"reflect"
	"testing"
)

func TestCoverPaletteUsesImageAndIsDeterministic(t *testing.T) {
	picture := image.NewNRGBA(image.Rect(0, 0, 96, 96))
	for y := 0; y < 96; y++ {
		for x := 0; x < 96; x++ {
			c := color.NRGBA{255, 0, 0, 255}
			if x >= 48 {
				c = color.NRGBA{0, 0, 255, 255}
			}
			picture.SetNRGBA(x, y, c)
		}
	}
	want := []string{"#0000ff", "#ff0000"}
	for i := 0; i < 20; i++ {
		if got := imagePalette(picture); !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected cover palette: %v", got)
		}
	}
	transparent := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	if got := imagePalette(transparent); len(got) != 0 {
		t.Fatal("transparent artwork fabricated colors", got)
	}
}
func TestCoverURLRejectsLocalAndLookalikeAddresses(t *testing.T) {
	for _, raw := range []string{"http://i.scdn.co/image/a", "https://127.0.0.1/a", "https://scdn.co.evil.test/a", "https://evilscdn.co/a", "https://i.scdn.co:8888/a", "https://user:pass@i.scdn.co/a", "file:///data/local/tmp/a"} {
		if publicCoverURL(raw) {
			t.Fatal("accepted", raw)
		}
	}
	if !publicCoverURL("https://i.scdn.co/image/real-cover") {
		t.Fatal("rejected public artwork CDN")
	}
}
func TestMusicSourceExcludesHistoricalAndPausedPlayers(t *testing.T) {
	dump := "PlaybackActivityMonitor dump time: now\n u/pid:10010/44 state:paused usage=USAGE_MEDIA content=music\n u/pid:10011/45 state:started usage=USAGE_MEDIA content=music\n u/pid:10012/46 state:started usage=USAGE_ASSISTANT content=speech\nAudio event log:\n u/pid:10011/99 state:started usage=USAGE_MEDIA content=music"
	if got := activeMediaPIDs(dump); !reflect.DeepEqual(got, []string{"45"}) {
		t.Fatal("selected stale or non-media player", got)
	}
}
func TestMusicFramesAndMotionAreDistinct(t *testing.T) {
	s := musicLight{Level: .15, Gain: 6, Phase: .2}
	for i := range s.Bands {
		s.Bands[i] = .05
	}
	for i := range s.Wave {
		s.Wave[i] = .2
	}
	for _, mode := range []string{"spectrum", "wave", "pulse", "vu", "mirror", "orbit", "ribbon"} {
		if !validMusicMode(mode) {
			t.Fatal(mode)
		}
		f := musicMatrixFrame(s, mode)
		if len(f) != 400 {
			t.Fatal(mode, len(f))
		}
		nonzero := false
		for _, v := range f {
			nonzero = nonzero || v > 0
		}
		if !nonzero {
			t.Fatal("blank visual", mode)
		}
	}
	for _, mode := range []string{"orbit", "ribbon"} {
		a := musicMatrixFrame(s, mode)
		s.Phase += 1
		b := musicMatrixFrame(s, mode)
		if reflect.DeepEqual(a, b) {
			t.Fatal("speed has no effect", mode)
		}
	}
}
func TestPrivacyPreservesCoreAndRejectsTraversal(t *testing.T) {
	third := map[string]bool{"com.yandex.io.sdk": true, "com.spocky.projengmenu": true, "com.android.vending": true, "local.station.studio": true, "example.optional.player": true}
	for _, pkg := range []string{"com.yandex.io.sdk", "com.spocky.projengmenu", "com.android.vending", "local.station.studio", "com.android.systemui", "com.android.settings", "bad;reboot"} {
		if privacyCanDisable(pkg, third) {
			t.Fatal("can disable critical or invalid package", pkg)
		}
	}
	if !privacyCanDisable("example.optional.player", third) || !privacyCanDisable("com.yandex.tv.home", third) {
		t.Fatal("optional app cannot be disabled")
	}
	if _, _, err := scanAnalytics([]string{"/system/../../private.apk"}); err == nil {
		t.Fatal("accepted traversal")
	}
}
