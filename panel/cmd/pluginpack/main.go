// pluginpack builds a deterministic YAM tar.zst from plugin.json and files/.
package main

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/klauspost/compress/zstd"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

func main() {
	if e := pack(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func pack() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("Usage: pluginpack source-directory output.yampkg")
	}
	source, e := filepath.Abs(os.Args[1])
	if e != nil {
		return e
	}
	output, e := filepath.Abs(os.Args[2])
	if e != nil {
		return e
	}
	if !strings.HasSuffix(output, ".yampkg") {
		return fmt.Errorf("Output must end in .yampkg")
	}
	raw, e := os.ReadFile(filepath.Join(source, "plugin.json"))
	if e != nil {
		return e
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return fmt.Errorf("Invalid plugin.json")
	}
	name, _ := m["name"].(string)
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}/[a-z0-9][a-z0-9_-]{0,63}$`).MatchString(name) {
		return fmt.Errorf("Invalid namespace/name")
	}
	if m["target"] != "yandexstation_2" || m["arch"] != "aarch64" {
		return fmt.Errorf("This SDK profile targets yandexstation_2 / aarch64")
	}
	root := filepath.Join(source, "files")
	var paths []string
	hashes := map[string]string{}
	var total int64
	e = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("Links and special files are forbidden: %s", p)
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, "bin/") && !strings.HasPrefix(rel, "share/") {
			return fmt.Errorf("Only bin/ and share/ are supported")
		}
		if strings.ContainsAny(rel, "\\\x00") {
			return fmt.Errorf("Invalid path")
		}
		if strings.HasPrefix(rel, "share/") {
			prefix := strings.Split(name, "/")[0] + "."
			scoped := false
			for _, part := range strings.Split(rel, "/") {
				if strings.HasPrefix(part, prefix) {
					scoped = true
				}
			}
			if !scoped {
				return fmt.Errorf("Use share/%s/", strings.ReplaceAll(name, "/", "."))
			}
		}
		if info.Size() > 64<<20 {
			return fmt.Errorf("File exceeds 64 MiB")
		}
		total += info.Size()
		if total > 256<<20 || len(paths) >= 9998 {
			return fmt.Errorf("Package exceeds unpacked limits")
		}
		f, e := os.Open(p)
		if e != nil {
			return e
		}
		h := sha256.New()
		_, e = io.Copy(h, f)
		f.Close()
		if e != nil {
			return e
		}
		hashes[rel] = fmt.Sprintf("%x", h.Sum(nil))
		paths = append(paths, rel)
		return nil
	})
	if e != nil {
		return e
	}
	if len(paths) == 0 {
		return fmt.Errorf("No payload")
	}
	sort.Strings(paths)
	m["files_sha256"] = hashes
	// JSON is a YAML 1.2 subset accepted by the YAM YAML parser.
	manifest, e := json.MarshalIndent(m, "", "  ")
	if e != nil {
		return e
	}
	temp, e := os.CreateTemp(filepath.Dir(output), ".yampkg-*")
	if e != nil {
		return e
	}
	defer os.Remove(temp.Name())
	z, e := zstd.NewWriter(temp, zstd.WithEncoderConcurrency(1))
	if e != nil {
		temp.Close()
		return e
	}
	a := tar.NewWriter(z)
	write := func(name string, mode int64, data io.Reader, size int64) error {
		if e := a.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: size, ModTime: time.Unix(0, 0), Format: tar.FormatUSTAR}); e != nil {
			return e
		}
		_, e := io.Copy(a, data)
		return e
	}
	e = write("MANIFEST.yaml", 0644, strings.NewReader(string(manifest)), int64(len(manifest)))
	for _, rel := range paths {
		if e != nil {
			break
		}
		f, err := os.Open(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			e = err
			break
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			e = err
			break
		}
		mode := int64(0644)
		if strings.HasPrefix(rel, "bin/") {
			mode = 0755
		}
		e = write("files/"+rel, mode, f, info.Size())
		f.Close()
	}
	for _, closeFn := range []func() error{a.Close, z.Close, temp.Sync, temp.Close} {
		if err := closeFn(); e == nil {
			e = err
		}
	}
	if e != nil {
		return e
	}
	stat, e := os.Stat(temp.Name())
	if e != nil {
		return e
	}
	if stat.Size() > 64<<20 {
		return fmt.Errorf("Compressed package exceeds 64 MiB")
	}
	if _, e = os.Stat(output); e == nil {
		return fmt.Errorf("Output already exists; choose a new version filename")
	}
	if e = os.Rename(temp.Name(), output); e != nil {
		return e
	}
	fmt.Println(output)
	return nil
}
