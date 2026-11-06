package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLiveTwinPreservesHardwareIntensityAndRejectsOtherGeometry(t *testing.T) {
	raw := make([]byte, 1200)
	for i := 0; i < 400; i++ {
		v := byte(i % 256)
		raw[i*3] = v
		raw[i*3+1] = v
		raw[i*3+2] = v
	}
	mono, e := twinMonoFrame(raw, 25, 16, 24)
	if e != nil {
		t.Fatal(e)
	}
	for i, v := range mono {
		if v != byte(i%256) {
			t.Fatal("hardware brightness changed", i, v)
		}
	}
	if _, e = twinMonoFrame(raw, 25, 16, 32); e == nil {
		t.Fatal("unverified geometry accepted")
	}
	if _, e = twinMonoFrame(raw[:100], 25, 16, 24); e == nil {
		t.Fatal("short hardware frame accepted")
	}
}
func TestLiveTwinRingUsesDriverChannelsAndFailsClosed(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 24; i++ {
		for c, name := range []string{"red", "green", "blue"} {
			dir := filepath.Join(root, fmt.Sprintf("station2:%s:ring%d", name, i+1))
			os.MkdirAll(dir, 0700)
			os.WriteFile(filepath.Join(dir, "brightness"), []byte(fmt.Sprint(i*10+c)), 0600)
		}
	}
	ring, ok := readTwinRing(root)
	if !ok || len(ring) != 24 || ring[12] != [3]int{120, 121, 122} {
		t.Fatal("channel order changed", ring)
	}
	os.WriteFile(filepath.Join(root, "station2:green:ring3/brightness"), []byte("256"), 0600)
	if _, ok = readTwinRing(root); ok {
		t.Fatal("invalid channel advertised as live")
	}
}
func TestPluginSceneAssetsAreManifestBoundAndNamespaced(t *testing.T) {
	root := t.TempDir()
	m := PluginManifest{Name: "test/space", Version: "1.0.0", Files: map[string]string{}}
	prefix := "share/test.space/"
	directory := filepath.Join(root, "pkgs", m.Name, m.Version, prefix)
	os.MkdirAll(filepath.Join(directory, "assets"), 0700)
	write := func(rel string, data []byte) {
		sum := sha256.Sum256(data)
		m.Files[prefix+rel] = hex.EncodeToString(sum[:])
		os.WriteFile(filepath.Join(directory, rel), data, 0600)
	}
	write("station-studio.json", []byte(`{"version":1,"title":"Space","ui":"station-ui.html","uiMode":"scene","uiTV":true,"fields":[]}`))
	write("assets/texture.bin", []byte{1, 2, 3, 4})
	data, e := readPluginAssetAt(root, m, "assets/texture.bin")
	if e != nil || !bytes.Equal(data, []byte{1, 2, 3, 4}) {
		t.Fatal(e)
	}
	for _, rel := range []string{"../secret", "/etc/passwd", "assets/../station-studio.json", "assets\\texture.bin", "assets/not-in-manifest.bin"} {
		if _, e = readPluginAssetAt(root, m, rel); e == nil {
			t.Fatal("unsafe asset accepted", rel)
		}
	}
	os.WriteFile(filepath.Join(directory, "assets/texture.bin"), []byte("changed"), 0600)
	if _, e = readPluginAssetAt(root, m, "assets/texture.bin"); e == nil {
		t.Fatal("changed asset accepted")
	}
}
func TestPluginSceneOptionsAreExplicitAndBackwardCompatible(t *testing.T) {
	for _, raw := range []string{`{"version":1,"title":"Panel","fields":[]}`, `{"version":1,"title":"Scene","ui":"station-ui.html","uiMode":"scene","uiTV":true,"fields":[]}`} {
		if _, e := decodePluginPanel([]byte(raw)); e != nil {
			t.Fatal(e)
		}
	}
	for _, raw := range []string{`{"version":1,"title":"Bad","uiMode":"scene","fields":[]}`, `{"version":1,"title":"Bad","ui":"station-ui.html","uiMode":"system","fields":[]}`, `{"version":1,"title":"Bad","uiTV":true,"fields":[]}`} {
		if _, e := decodePluginPanel([]byte(raw)); e == nil {
			t.Fatal("invalid scene mode accepted")
		}
	}
}

func TestLiveTwinNativeRingDecodesDriverBitfields(t *testing.T) {
	info := make([]byte, 160)
	binary.LittleEndian.PutUint32(info[0:4], 24)
	binary.LittleEndian.PutUint32(info[4:8], 1)
	binary.LittleEndian.PutUint32(info[24:28], 24)
	for c, at := range []int{32, 44, 56} {
		binary.LittleEndian.PutUint32(info[at:at+4], uint32((2-c)*8))
		binary.LittleEndian.PutUint32(info[at+4:at+8], 8)
	}
	raw := make([]byte, 72)
	raw[0] = 3
	raw[1] = 2
	raw[2] = 1
	ring, ok := twinRingFrame(raw, info)
	if !ok || ring[0] != [3]int{1, 2, 3} {
		t.Fatal("native ring channel layout lost", ring)
	}
	binary.LittleEndian.PutUint32(info[44:48], 16)
	if _, ok = twinRingFrame(raw, info); ok {
		t.Fatal("duplicate channel layout accepted")
	}
}
