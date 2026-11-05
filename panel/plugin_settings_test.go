package main

import (
	"encoding/json"
	"testing"
)

func TestPluginPanelRejectsExecutableAndAmbiguousFields(t *testing.T) {
	valid := `{"version":1,"title":"Свет","fields":[{"id":"level","label":"Яркость","type":"range","default":25,"min":0,"max":100,"step":5}]}`
	p, e := decodePluginPanel([]byte(valid))
	if e != nil {
		t.Fatal(e)
	}
	if validatePluginValue(p.Fields[0], float64(26)) == nil {
		t.Fatal("off-step value accepted")
	}
	if validatePluginValue(p.Fields[0], float64(105)) == nil {
		t.Fatal("out-of-range value accepted")
	}
	if validatePluginValue(p.Fields[0], "25") == nil {
		t.Fatal("wrong value type accepted")
	}
	for _, bad := range []string{
		`{"version":1,"title":"UI","html":"<script>","fields":[]}`,
		`{"version":1,"title":"UI","fields":[{"id":"../x","label":"x","type":"text","default":""}]}`,
		`{"version":1,"title":"UI","fields":[{"id":"x","label":"x","type":"text","default":""},{"id":"x","label":"x","type":"text","default":""}]}`,
		`{"version":1,"title":"UI","fields":[{"id":"x","label":"x","type":"range","default":0,"min":0,"max":10,"step":0}]}`,
		valid + ` {"extra":true}`,
	} {
		if _, e := decodePluginPanel([]byte(bad)); e == nil {
			t.Fatalf("invalid schema accepted: %s", bad)
		}
	}
}
func TestPluginSettingsDefaultsMigrationAndAtomicPersistence(t *testing.T) {
	oldBase := base
	base = t.TempDir()
	defer func() { base = oldBase }()
	var p pluginPanel
	if e := json.Unmarshal([]byte(`{"version":1,"title":"Test","fields":[{"id":"enabled","label":"Enabled","type":"toggle","default":true},{"id":"palette","label":"Palette","type":"select","options":["ocean","fire"],"default":"ocean"}]}`), &p); e != nil {
		t.Fatal(e)
	}
	if e := writePluginSettings("test/sample", map[string]any{"enabled": false, "palette": "removed-option", "old": "drop"}); e != nil {
		t.Fatal(e)
	}
	v := pluginSettingsValues("test/sample", p)
	if v["enabled"] != false || v["palette"] != "ocean" || len(v) != 2 {
		t.Fatalf("invalid migration: %v", v)
	}
	if e := writePluginSettings("test/sample", v); e != nil {
		t.Fatal(e)
	}
	v = pluginSettingsValues("test/sample", p)
	if v["enabled"] != false {
		t.Fatal("valid setting lost")
	}
}
