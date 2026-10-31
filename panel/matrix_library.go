package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
)

var libraryMu sync.Mutex
var libraryID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type matrixArtwork struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Format     string   `json:"format"`
	Frames     [][]byte `json:"frames"`
	Delays     []int    `json:"delays"`
	Gamma      float64  `json:"gamma"`
	Exposure   float64  `json:"exposure"`
	Duration   int      `json:"duration"`
	Brightness int      `json:"brightness"`
}

func matrixLibrary(w http.ResponseWriter, r *http.Request) {
	libraryMu.Lock()
	defer libraryMu.Unlock()
	dir := filepath.Join(base, "artworks")
	os.MkdirAll(dir, 0700)
	id := r.URL.Query().Get("id")
	if id != "" && !libraryID.MatchString(id) {
		fail(w, "Неверное имя", 400)
		return
	}
	path := filepath.Join(dir, id+".json")
	switch r.Method {
	case "GET":
		if id != "" {
			b, e := os.ReadFile(path)
			if e != nil {
				fail(w, "Анимация не найдена", 404)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(b)
			return
		}
		entries, _ := os.ReadDir(dir)
		list := []map[string]string{}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			b, e := os.ReadFile(filepath.Join(dir, entry.Name()))
			var a matrixArtwork
			if e == nil && json.Unmarshal(b, &a) == nil {
				list = append(list, map[string]string{"id": a.ID, "name": a.Name})
			}
		}
		sort.Slice(list, func(i, j int) bool { return list[i]["name"] < list[j]["name"] })
		response(w, list)
	case "POST":
		r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
		var a matrixArtwork
		if json.NewDecoder(r.Body).Decode(&a) != nil || !libraryID.MatchString(a.ID) || a.Format != "station-studio-matrix-v1" || len(a.Name) < 1 || len(a.Name) > 160 || len(a.Frames) < 1 || len(a.Frames) > 120 || len(a.Frames) != len(a.Delays) {
			fail(w, "Некорректная анимация", 400)
			return
		}
		for i, f := range a.Frames {
			if len(f) != 400 || a.Delays[i] < 66 || a.Delays[i] > 10000 {
				fail(w, "Неверный размер кадра или задержка", 400)
				return
			}
		}
		if a.Gamma < 0.25 || a.Gamma > 3 || a.Exposure < 0.25 || a.Exposure > 4 || (a.Duration != 0 && a.Duration < 5) || a.Duration > 86400 || a.Brightness < 0 || a.Brightness > 200 {
			fail(w, "Проверьте настройки показа", 400)
			return
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) >= 100 {
			if _, e := os.Stat(filepath.Join(dir, a.ID+".json")); e != nil {
				fail(w, "В библиотеке может быть до 100 анимаций", 400)
				return
			}
		}
		path = filepath.Join(dir, a.ID+".json")
		b, e := json.Marshal(a)
		if e == nil {
			e = os.WriteFile(path+".tmp", b, 0600)
		}
		if e == nil {
			e = os.Rename(path+".tmp", path)
		}
		if e != nil {
			fail(w, fmt.Sprint(e), 500)
			return
		}
		response(w, map[string]bool{"ok": true})
	case "DELETE":
		if id == "" {
			fail(w, "Выберите анимацию", 400)
			return
		}
		if e := os.Remove(path); e != nil {
			fail(w, "Не удалось удалить", 404)
			return
		}
		response(w, map[string]bool{"ok": true})
	default:
		fail(w, "Метод не поддерживается", 405)
	}
}
