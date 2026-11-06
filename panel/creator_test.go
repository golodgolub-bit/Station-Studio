package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAirplaySettingsStayWithinSupportedReceiverContract(t *testing.T) {
	for _, q := range [][2]string{{"server_name", "Станция Макс"}, {"require_pin", "true"}, {"resolution", "1280x720"}, {"audio_latency_ms", "-1"}} {
		if !airplaySettingValid(q[0], q[1]) {
			t.Fatal(q)
		}
	}
	for _, q := range [][2]string{{"server_port", "22"}, {"server_name", "x\nreboot"}, {"require_pin", "1"}, {"max_fps", "120"}, {"audio_latency_ms", "1001"}} {
		if airplaySettingValid(q[0], q[1]) {
			t.Fatal("unexpected accepted setting", q)
		}
	}
}
func TestPluginConsentIsVersionAndContentsBound(t *testing.T) {
	old := base
	base = t.TempDir()
	defer func() { base = old }()
	m := PluginManifest{Name: "test/plugin", Version: "1.0.0", Files: map[string]string{"bin/test": "abc"}}
	if pluginRootAllowed(m) {
		t.Fatal("implicit trust")
	}
	if e := setPluginTrust(m, true); e != nil {
		t.Fatal(e)
	}
	if !pluginRootAllowed(m) {
		t.Fatal("consent not stored")
	}
	m.Files["bin/test"] = "changed"
	if pluginRootAllowed(m) {
		t.Fatal("modified plugin inherited root")
	}
	m.Files["bin/test"] = "abc"
	m.Version = "1.0.1"
	if pluginRootAllowed(m) {
		t.Fatal("updated plugin inherited root")
	}
	m.Version = "1.0.0"
	if e := setPluginTrust(m, false); e != nil {
		t.Fatal(e)
	}
	if pluginRootAllowed(m) {
		t.Fatal("revocation ignored")
	}
	raw, _ := os.Stat(pluginTrustPath(m.Name))
	if raw.Mode().Perm() != 0600 {
		t.Fatal("trust state is not private")
	}
}
func TestPluginUIOnlyUsesFixedSiblingFile(t *testing.T) {
	for _, path := range []string{"../evil.html", "https://example.com", "/system/build.prop", "share/station-ui.html"} {
		raw := []byte(`{"version":1,"title":"Example","ui":"` + path + `","fields":[]}`)
		if _, e := decodePluginPanel(raw); e == nil {
			t.Fatal("unsafe ui path accepted", path)
		}
	}
	if _, e := decodePluginPanel([]byte(`{"version":1,"title":"Example","emoji":"🪐","ui":"station-ui.html","fields":[]}`)); e != nil {
		t.Fatal(e)
	}
	m := PluginManifest{Name: "test/plugin", Files: map[string]string{"share/test.plugin/station-studio.json": "hash"}}
	if pluginPanelPath(m) != filepath.ToSlash("share/test.plugin/station-studio.json") {
		t.Fatal("namespaced settings ignored")
	}
}
