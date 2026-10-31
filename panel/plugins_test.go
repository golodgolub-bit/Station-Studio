package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"fmt"
	"github.com/klauspost/compress/zstd"
	"testing"
)

func fixturePlugin(t *testing.T, target, filename string, corrupt bool, typ byte) []byte {
	t.Helper()
	payload := []byte("#!/system/bin/sh\necho hello\n")
	sum := fmt.Sprintf("%x", sha256.Sum256(payload))
	if corrupt {
		sum = "wrong"
	}
	manifest := fmt.Sprintf("name: test/hello\nversion: 1.0.0\nlicense: MIT\ndescription: Hello\ntarget: %s\narch: aarch64\nfiles_sha256:\n  bin/hello: %s\nservices:\n  hello:\n    exec: [bin/hello]\n    oneshot: true\n", target, sum)
	var out bytes.Buffer
	z, _ := zstd.NewWriter(&out)
	a := tar.NewWriter(z)
	for _, entry := range []struct {
		name string
		data []byte
		typ  byte
	}{{"MANIFEST.yaml", []byte(manifest), tar.TypeReg}, {filename, payload, typ}} {
		h := &tar.Header{Name: entry.name, Size: int64(len(entry.data)), Typeflag: entry.typ, Mode: 0700}
		if entry.typ == tar.TypeSymlink {
			h.Size = 0
			h.Linkname = "/system/bin/sh"
		}
		if e := a.WriteHeader(h); e != nil {
			t.Fatal(e)
		}
		if h.Size > 0 {
			a.Write(entry.data)
		}
	}
	a.Close()
	z.Close()
	return out.Bytes()
}
func TestPluginValid(t *testing.T) {
	r, e := inspectPlugin(bytes.NewReader(fixturePlugin(t, "yandexstation_2", "files/bin/hello", false, tar.TypeReg)))
	if e != nil || !r.Compatible || r.Manifest.Name != "test/hello" {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestPluginRejectsTraversal(t *testing.T) {
	_, e := inspectPlugin(bytes.NewReader(fixturePlugin(t, "yandexstation_2", "files/../../escape", false, tar.TypeReg)))
	if e == nil {
		t.Fatal("path traversal accepted")
	}
}
func TestPluginRejectsSymlink(t *testing.T) {
	_, e := inspectPlugin(bytes.NewReader(fixturePlugin(t, "yandexstation_2", "files/bin/hello", false, tar.TypeSymlink)))
	if e == nil {
		t.Fatal("symlink accepted")
	}
}
func TestPluginRejectsHashMismatch(t *testing.T) {
	_, e := inspectPlugin(bytes.NewReader(fixturePlugin(t, "yandexstation_2", "files/bin/hello", true, tar.TypeReg)))
	if e == nil {
		t.Fatal("corrupt payload accepted")
	}
}
func TestPluginOtherModelIsNotCompatible(t *testing.T) {
	r, e := inspectPlugin(bytes.NewReader(fixturePlugin(t, "yandexmini_3", "files/bin/hello", false, tar.TypeReg)))
	if e != nil || r.Compatible {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestPluginArchiveNames(t *testing.T) {
	for _, p := range []string{"/data/escape", "../escape", "files/../../escape", "a\\b", "a//b", "a/./b"} {
		if validArchivePath(p) {
			t.Fatal(p)
		}
	}
}
