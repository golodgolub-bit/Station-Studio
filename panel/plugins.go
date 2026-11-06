package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/klauspost/compress/zstd"
	"go.yaml.in/yaml/v3"
)

const yamRoot = "/data/local/station-yam"
const maxPlugin = 64 << 20

var pluginMu sync.Mutex
var pluginName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}/[a-z0-9][a-z0-9_-]{0,63}$`)
var pluginVersion = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,79}$`)
var pluginID = regexp.MustCompile(`^[a-f0-9]{64}$`)

type PluginService struct {
	Exec    []string          `yaml:"exec" json:"exec"`
	Restart string            `yaml:"restart" json:"restart"`
	After   []string          `yaml:"after" json:"after"`
	Env     map[string]string `yaml:"env" json:"env"`
	Oneshot bool              `yaml:"oneshot" json:"oneshot"`
}
type PluginManifest struct {
	Name        string   `yaml:"name" json:"name"`
	Version     string   `yaml:"version" json:"version"`
	License     string   `yaml:"license" json:"license"`
	Description string   `yaml:"description" json:"description"`
	Homepage    string   `yaml:"homepage" json:"homepage"`
	Target      string   `yaml:"target" json:"target"`
	Arch        string   `yaml:"arch" json:"arch"`
	Depends     []string `yaml:"depends" json:"depends"`
	Recommends  []string `yaml:"recommends" json:"recommends"`
	Enhances    []string `yaml:"enhances" json:"enhances"`
	Provides    []string `yaml:"provides" json:"provides"`
	Conflicts   []string `yaml:"conflicts" json:"conflicts"`
	Replaces    []string `yaml:"replaces" json:"replaces"`
	Permissions []string `yaml:"permissions" json:"permissions"`
	Entware     *struct {
		Packages []string `yaml:"packages" json:"packages"`
	} `yaml:"entware" json:"entware"`
	Services    map[string]PluginService `yaml:"services" json:"services"`
	Postinstall []string                 `yaml:"postinstall" json:"postinstall"`
	Files       map[string]string        `yaml:"files_sha256" json:"files"`
}
type PluginRecord struct {
	Panel      *pluginPanel   `json:"panel,omitempty"`
	ID         string         `json:"id"`
	Filename   string         `json:"filename"`
	Size       int64          `json:"size"`
	Unpacked   int64          `json:"unpacked"`
	Manifest   PluginManifest `json:"manifest"`
	Compatible bool           `json:"compatible"`
	Notes      []string       `json:"notes"`
	Uploaded   int64          `json:"uploaded"`
}

func pluginDir() string { return filepath.Join(base, "plugins") }
func validArchivePath(p string) bool {
	return p != "" && !strings.ContainsAny(p, "\\\x00") && !strings.HasPrefix(p, "/") && path.Clean(p) == p && p != ".." && !strings.HasPrefix(p, "../")
}
func inspectPlugin(reader io.Reader) (PluginRecord, error) {
	record := PluginRecord{Compatible: true, Notes: []string{}}
	decoder, e := zstd.NewReader(reader, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(64<<20), zstd.WithDecoderMaxWindow(32<<20))
	if e != nil {
		return record, e
	}
	defer decoder.Close()
	archive := tar.NewReader(io.LimitReader(decoder, (256<<20)+(16<<20)))
	seen := map[string]bool{}
	hashes := map[string]string{}
	entries := 0
	for {
		h, e := archive.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return record, e
		}
		entries++
		name := strings.TrimSuffix(h.Name, "/")
		if entries > 10000 || !validArchivePath(name) || seen[name] {
			return record, fmt.Errorf("Недопустимый или повторный путь в архиве")
		}
		seen[name] = true
		if entries == 1 && name != "MANIFEST.yaml" {
			return record, fmt.Errorf("Первым файлом должен быть MANIFEST.yaml")
		}
		if h.Typeflag == tar.TypeDir {
			continue
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			return record, fmt.Errorf("Ссылки и специальные файлы запрещены")
		}
		if h.Size < 0 || h.Size > 64<<20 {
			return record, fmt.Errorf("Слишком большой файл внутри архива")
		}
		record.Unpacked += h.Size
		if record.Unpacked > 256<<20 {
			return record, fmt.Errorf("Распакованный пакет превышает 256 МБ")
		}
		if name == "MANIFEST.yaml" {
			if h.Size > 128<<10 {
				return record, fmt.Errorf("Слишком большое описание пакета")
			}
			data, e := io.ReadAll(archive)
			if e != nil {
				return record, e
			}
			dec := yaml.NewDecoder(bytes.NewReader(data))
			dec.KnownFields(true)
			if e = dec.Decode(&record.Manifest); e != nil {
				return record, fmt.Errorf("Описание YAM: %w", e)
			}
		} else if strings.HasPrefix(name, "files/") {
			rel := strings.TrimPrefix(name, "files/")
			if !validArchivePath(rel) || (!strings.HasPrefix(rel, "bin/") && !strings.HasPrefix(rel, "share/")) {
				return record, fmt.Errorf("Файлы пакета должны находиться в bin/ или share/")
			}
			hash := sha256.New()
			if _, e = io.Copy(hash, archive); e != nil {
				return record, e
			}
			hashes[rel] = hex.EncodeToString(hash.Sum(nil))
		} else if name != "SOURCE" {
			return record, fmt.Errorf("Неизвестный файл в пакете: %s", name)
		}
	}
	m := record.Manifest
	if !pluginName.MatchString(m.Name) || !pluginVersion.MatchString(m.Version) || len(m.Description) > 4000 || len(m.Services) > 16 || len(m.Files) == 0 {
		return record, fmt.Errorf("Некорректные имя, версия или состав пакета")
	}
	if len(hashes) != len(m.Files) {
		return record, fmt.Errorf("Список файлов не совпадает с описанием")
	}
	for p, h := range m.Files {
		if hashes[p] != h {
			return record, fmt.Errorf("Не совпадает SHA-256: %s", p)
		}
		if strings.HasPrefix(p, "share/") && !strings.HasPrefix(m.Name, "yam/") {
			prefix := strings.Split(m.Name, "/")[0] + "."
			scoped := false
			for _, part := range strings.Split(strings.TrimPrefix(p, "share/"), "/") {
				if strings.HasPrefix(part, prefix) {
					scoped = true
				}
			}
			if !scoped {
				return record, fmt.Errorf("Путь %s должен содержать пространство имён %s; например share/%s/", p, prefix, strings.ReplaceAll(m.Name, "/", "."))
			}
		}
	}
	if m.Arch != "aarch64" || (m.Target != "any" && m.Target != "yandexstation_2") {
		record.Compatible = false
		record.Notes = append(record.Notes, "Нужен пакет для yandexstation_2 / aarch64")
	}
	for name, spec := range m.Services {
		if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`).MatchString(name) || len(spec.Exec) == 0 || len(spec.Exec) > 32 || !strings.HasPrefix(spec.Exec[0], "bin/") || m.Files[spec.Exec[0]] == "" {
			return record, fmt.Errorf("Недопустимый сервис %s", name)
		}
		if spec.Restart != "" && spec.Restart != "never" && spec.Restart != "always" && spec.Restart != "on-failure" {
			return record, fmt.Errorf("Неизвестный режим перезапуска")
		}
		for k, v := range spec.Env {
			if !regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`).MatchString(k) || len(v) > 4096 || strings.ContainsRune(v, 0) {
				return record, fmt.Errorf("Некорректное окружение сервиса")
			}
		}
	}
	if len(m.Postinstall) > 0 {
		record.Notes = append(record.Notes, "При установке выполняется root-команда: "+strings.Join(m.Postinstall, " "))
	}
	if m.Entware != nil && len(m.Entware.Packages) > 0 {
		record.Compatible = false
		record.Notes = append(record.Notes, "Пакету требуется Entware; он пока не установлен")
	}
	if len(m.Replaces) > 0 {
		record.Notes = append(record.Notes, "Пакет запрашивает замену: "+strings.Join(m.Replaces, ", "))
	}
	record.Notes = append(record.Notes, "Локальный пакет не имеет подписи реестра. Код и службы выполняются с root-правами после вашего запуска.")
	return record, nil
}
func pluginRecords() []PluginRecord {
	rows := []PluginRecord{}
	files, _ := filepath.Glob(filepath.Join(pluginDir(), "*.json"))
	for _, file := range files {
		b, e := os.ReadFile(file)
		var r PluginRecord
		if e == nil && json.Unmarshal(b, &r) == nil && pluginID.MatchString(r.ID) {
			rows = append(rows, r)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Uploaded > rows[j].Uploaded })
	return rows
}
func yamCall(args ...string) (string, error) {
	return run(110*time.Second, yamRoot+"/bin/yam", append([]string{"--platform", "yandexstation_2", "--profiles", yamRoot + "/platforms", "--root", yamRoot}, args...)...)
}
func plugins(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		fail(w, "Нужен GET", 405)
		return
	}
	output, e := yamCall("list")
	installed := map[string]string{}
	if e == nil {
		for _, line := range strings.Split(output, "\n") {
			p := strings.Split(line, "\t")
			if len(p) == 3 {
				installed[p[0]] = p[1]
			}
		}
	}
	rows := pluginRecords()
	for name, version := range installed {
		found := false
		for _, row := range rows {
			if row.Manifest.Name == name && row.Manifest.Version == version {
				found = true
			}
		}
		if !found {
			if m, err := loadInstalledManifest(name); err == nil {
				rows = append(rows, PluginRecord{Manifest: m, Compatible: true, Notes: []string{"Пакет установлен. Загруженный архив удалён."}})
			}
		}
	}
	for i := range rows {
		rows[i].Panel = nil
		if installed[rows[i].Manifest.Name] == rows[i].Manifest.Version {
			if p, e := readPluginPanel(rows[i].Manifest); e == nil {
				rows[i].Panel = &p
			}
		}
	}
	response(w, map[string]any{"ok": true, "engine": fileExists(yamRoot + "/bin/yam"), "packages": rows, "installed": installed, "services": pluginServiceStates(), "legacy": "Старые .yap требуют Luna. Адаптер Luna для Android Max пока не подтверждён."})
}
func pluginUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		fail(w, "Нужен POST", 405)
		return
	}
	pluginMu.Lock()
	defer pluginMu.Unlock()
	name := r.Header.Get("X-Filename")
	if decoded, err := url.PathUnescape(name); err == nil {
		name = decoded
	}
	if !strings.HasSuffix(strings.ToLower(name), ".yampkg") {
		fail(w, "Нужен пакет YAM .yampkg. Старые .yap несовместимы с текущим YAM.", 400)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPlugin)
	os.MkdirAll(pluginDir(), 0700)
	records := pluginRecords()
	var used int64
	for _, p := range records {
		used += p.Size
	}
	if used > 128<<20 {
		fail(w, "Архив загрузок заполнен. Удалите ненужные архивы.", 409)
		return
	}
	f, e := os.CreateTemp(pluginDir(), ".upload-")
	if e != nil {
		fail(w, e.Error(), 500)
		return
	}
	defer os.Remove(f.Name())
	defer f.Close()
	f.Chmod(0600)
	hash := sha256.New()
	size, e := io.Copy(io.MultiWriter(f, hash), r.Body)
	if e != nil {
		fail(w, "Не удалось загрузить пакет; лимит 64 МБ", 400)
		return
	}
	if used+size > 128<<20 {
		fail(w, "Архив загрузок заполнен. Удалите ненужные архивы.", 409)
		return
	}
	f.Seek(0, 0)
	record, e := inspectPlugin(f)
	if e != nil {
		fail(w, e.Error(), 400)
		return
	}
	record.ID = hex.EncodeToString(hash.Sum(nil))
	record.Size = size
	record.Filename = filepath.Base(name)
	record.Uploaded = time.Now().Unix()
	dest := filepath.Join(pluginDir(), record.ID+".yampkg")
	f.Close()
	if e = os.Rename(f.Name(), dest); e != nil {
		fail(w, e.Error(), 500)
		return
	}
	data, _ := json.Marshal(record)
	if e = os.WriteFile(filepath.Join(pluginDir(), record.ID+".json"), data, 0600); e != nil {
		fail(w, e.Error(), 500)
		return
	}
	event("Загружен плагин " + record.Manifest.Name)
	response(w, record)
}
func pluginAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		fail(w, "Нужен POST", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	var q struct {
		Action    string `json:"action"`
		ID        string `json:"id"`
		Name      string `json:"name"`
		Service   string `json:"service"`
		Replace   bool   `json:"replace"`
		TrustRoot bool   `json:"trustRoot"`
	}
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		fail(w, "Некорректный запрос", 400)
		return
	}
	pluginMu.Lock()
	defer pluginMu.Unlock()
	var output string
	var e error
	switch q.Action {
	case "install":
		if !pluginID.MatchString(q.ID) {
			fail(w, "Некорректный архив", 400)
			return
		}
		file := filepath.Join(pluginDir(), q.ID+".yampkg")
		f, err := os.Open(file)
		if err != nil {
			fail(w, "Архив не найден", 404)
			return
		}
		record, err := inspectPlugin(f)
		f.Close()
		if err != nil || !record.Compatible {
			fail(w, "Пакет не прошёл проверку совместимости", 400)
			return
		}
		if (len(record.Manifest.Services) > 0 || len(record.Manifest.Postinstall) > 0) && !q.TrustRoot {
			fail(w, "Для установки исполняемого плагина подтвердите полный root-доступ", 403)
			return
		}
		var stat syscall.Statfs_t
		if syscall.Statfs(base, &stat) != nil || int64(stat.Bavail)*int64(stat.Bsize) < record.Unpacked+(100<<20) {
			fail(w, "Недостаточно места: после установки нужно оставить 100 МБ", 409)
			return
		}
		args := []string{"install", file}
		if q.Replace {
			args = append(args, "--yes")
		}
		output, e = yamCall(args...)
		if e == nil {
			record.ID = q.ID
			stopPluginPackage(record.Manifest.Name)
			saveInstalledManifest(record.Manifest)
			if q.TrustRoot {
				e = setPluginTrust(record.Manifest, true)
			}
			event("Установлен плагин " + record.Manifest.Name)
		}
	case "remove":
		if !pluginName.MatchString(q.Name) {
			fail(w, "Некорректное имя", 400)
			return
		}
		stopPluginPackage(q.Name)
		output, e = yamCall("remove", q.Name)
		if e == nil {
			os.Remove(installedManifestPath(q.Name))
			os.Remove(pluginTrustPath(q.Name))
			os.Remove(pluginHomePath(q.Name))
			event("Удалён плагин " + q.Name)
		}
	case "start", "stop":
		if !pluginName.MatchString(q.Name) {
			fail(w, "Некорректное имя", 400)
			return
		}
		if q.Action == "start" {
			e = startPluginService(q.Name, q.Service, true)
		} else {
			e = stopPluginService(q.Name, q.Service)
		}
	case "forget":
		if !pluginID.MatchString(q.ID) {
			fail(w, "Некорректный архив", 400)
			return
		}
		os.Remove(filepath.Join(pluginDir(), q.ID+".yampkg"))
		os.Remove(filepath.Join(pluginDir(), q.ID+".json"))
	default:
		fail(w, "Неизвестное действие", 400)
		return
	}
	if e != nil {
		fail(w, output+" "+e.Error(), 409)
		return
	}
	response(w, map[string]any{"ok": true, "output": output})
}

type managedPlugin struct {
	cmd        *exec.Cmd
	done       chan struct{}
	enabled    bool
	generation uint64
	log        string
	restarts   int
	status     string
}

var servicesMu sync.Mutex
var managedPlugins = map[string]*managedPlugin{}

func installedManifestPath(name string) string {
	return filepath.Join(pluginDir(), "installed", strings.ReplaceAll(name, "/", ".")+".json")
}
func saveInstalledManifest(m PluginManifest) {
	os.MkdirAll(filepath.Join(pluginDir(), "installed"), 0700)
	b, _ := json.Marshal(m)
	os.WriteFile(installedManifestPath(m.Name), b, 0600)
}
func loadInstalledManifest(name string) (PluginManifest, error) {
	var m PluginManifest
	b, e := os.ReadFile(installedManifestPath(name))
	if e == nil {
		e = json.Unmarshal(b, &m)
	}
	if e == nil && m.Name != name {
		e = fmt.Errorf("Имя пакета не совпало")
	}
	return m, e
}

type pluginLog struct{ key string }

func (l pluginLog) Write(p []byte) (int, error) {
	servicesMu.Lock()
	defer servicesMu.Unlock()
	if s := managedPlugins[l.key]; s != nil {
		s.log += string(p)
		if len(s.log) > 16000 {
			s.log = s.log[len(s.log)-16000:]
		}
	}
	return len(p), nil
}
func enabledPluginPath(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(pluginDir(), "enabled-"+hex.EncodeToString(sum[:])+".json")
}
func startPluginService(name, service string, persist bool) error {
	m, e := loadInstalledManifest(name)
	if e != nil {
		return e
	}
	if !pluginRootAllowed(m) {
		return fmt.Errorf("Сначала разрешите root-доступ этой версии в карточке плагина")
	}
	spec, ok := m.Services[service]
	if !ok {
		return fmt.Errorf("Сервис не объявлен в пакете")
	}
	folder := filepath.Join(yamRoot, "pkgs", name, m.Version)
	executable := filepath.Join(folder, filepath.FromSlash(spec.Exec[0]))
	resolved, e := filepath.EvalSymlinks(executable)
	if e != nil || resolved != executable {
		return fmt.Errorf("Исполняемый файл пакета изменён")
	}
	raw, e := os.ReadFile(executable)
	if e != nil {
		return e
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != m.Files[spec.Exec[0]] {
		return fmt.Errorf("SHA-256 программы не совпал с пакетом")
	}
	if m.Files[pluginPanelPath(m)] != "" {
		p, err := readPluginPanel(m)
		if err != nil {
			return err
		}
		if err = writePluginSettings(name, pluginSettingsValues(name, p)); err != nil {
			return err
		}
	}
	key := name + "/" + service
	servicesMu.Lock()
	s := managedPlugins[key]
	if s != nil && s.cmd != nil {
		servicesMu.Unlock()
		return nil
	}
	if s == nil {
		s = &managedPlugin{}
		managedPlugins[key] = s
	}
	s.enabled = true
	s.generation++
	generation := s.generation
	s.status = "Запускается"
	servicesMu.Unlock()
	if persist {
		b, _ := json.Marshal(map[string]string{"name": name, "service": service})
		os.WriteFile(enabledPluginPath(key), b, 0600)
	}
	go func() {
		for tries := 0; tries < 4; tries++ {
			cmd := exec.Command(executable, spec.Exec[1:]...)
			cmd.Dir = folder
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Stdout = pluginLog{key}
			cmd.Stderr = pluginLog{key}
			env := map[string]string{"PATH": yamRoot + "/bin:/system/bin:/vendor/bin", "ANDROID_ROOT": "/system", "ANDROID_DATA": "/data", "YAM_ROOT": yamRoot, "YAM_PKG": folder, "YAM_ETC": yamRoot + "/etc", "YAM_VAR": yamRoot + "/var", "YAM_RUN": yamRoot + "/run", "YAM_PLATFORM": "yandexstation_2"}
			expand := func(value string) string { return os.Expand(value, func(k string) string { return env[k] }) }
			for k, v := range spec.Env {
				env[k] = expand(v)
			}
			if m.Files[pluginPanelPath(m)] != "" {
				env["STATION_STUDIO_SETTINGS"] = pluginSettingsPath(name)
			}
			for i, argument := range spec.Exec[1:] {
				cmd.Args[i+1] = expand(argument)
			}
			for k, v := range env {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
			done := make(chan struct{})
			servicesMu.Lock()
			s := managedPlugins[key]
			if !s.enabled || s.generation != generation {
				servicesMu.Unlock()
				return
			}
			s.cmd = cmd
			s.done = done
			s.restarts = tries
			e := cmd.Start()
			if e == nil {
				s.status = "Работает"
			} else {
				s.status = e.Error()
				s.cmd = nil
			}
			servicesMu.Unlock()
			if e == nil {
				e = cmd.Wait()
			}
			close(done)
			if cmd.Process != nil {
				syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
			servicesMu.Lock()
			s = managedPlugins[key]
			if s.generation != generation {
				servicesMu.Unlock()
				return
			}
			s.cmd = nil
			if e != nil {
				s.status = e.Error()
			} else {
				s.status = "Завершён"
			}
			enabled := s.enabled
			servicesMu.Unlock()
			if !enabled || spec.Oneshot || spec.Restart == "never" || spec.Restart == "" || (spec.Restart == "on-failure" && e == nil) {
				return
			}
			time.Sleep(time.Duration(tries+1) * 3 * time.Second)
		}
		servicesMu.Lock()
		if s := managedPlugins[key]; s != nil && s.generation == generation {
			s.status = "Остановлен после повторных ошибок"
		}
		servicesMu.Unlock()
	}()
	return nil
}
func stopPluginService(name, service string) error {
	key := name + "/" + service
	servicesMu.Lock()
	s := managedPlugins[key]
	if s != nil {
		s.enabled = false
		s.generation++
		s.status = "Остановлен"
	}
	var proc *os.Process
	var done chan struct{}
	if s != nil && s.cmd != nil {
		proc = s.cmd.Process
		done = s.done
		s.cmd = nil
	}
	servicesMu.Unlock()
	os.Remove(enabledPluginPath(key))
	if proc != nil {
		syscall.Kill(-proc.Pid, syscall.SIGTERM)
		go func() {
			select {
			case <-done:
				return
			case <-time.After(2 * time.Second):
				syscall.Kill(-proc.Pid, syscall.SIGKILL)
			}
		}()
	}
	return nil
}
func stopPluginPackage(name string) {
	m, e := loadInstalledManifest(name)
	if e == nil {
		for service := range m.Services {
			stopPluginService(name, service)
		}
	}
}
func pluginServiceStates() map[string]any {
	servicesMu.Lock()
	defer servicesMu.Unlock()
	out := map[string]any{}
	for k, s := range managedPlugins {
		out[k] = map[string]any{"enabled": s.enabled, "running": s.cmd != nil, "status": s.status, "log": s.log, "restarts": s.restarts}
	}
	return out
}
func restorePluginServices() {
	for i := 0; i < 180; i++ {
		if read("/sys/fs/selinux/enforce") == "0" {
			break
		}
		time.Sleep(time.Second)
	}
	files, _ := filepath.Glob(filepath.Join(pluginDir(), "enabled-*.json"))
	for _, p := range files {
		var q map[string]string
		b, _ := os.ReadFile(p)
		if json.Unmarshal(b, &q) == nil && pluginName.MatchString(q["name"]) {
			if e := startPluginService(q["name"], q["service"], false); e != nil {
				event("Плагин не запущен: " + e.Error())
			}
		}
	}
}
