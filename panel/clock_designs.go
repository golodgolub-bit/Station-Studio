package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type clockDesign struct {
	ID     string        `json:"id"`
	Name   string        `json:"name"`
	Config ambientConfig `json:"config"`
}

var designMu sync.Mutex

func clockDesignsAPI(w http.ResponseWriter, r *http.Request) {
	designMu.Lock()
	defer designMu.Unlock()
	p := filepath.Join(base, "clock-designs.json")
	list := []clockDesign{}
	b, _ := os.ReadFile(p)
	json.Unmarshal(b, &list)
	if r.Method == "GET" {
		response(w, list)
		return
	}
	if r.Method == "DELETE" {
		id := r.URL.Query().Get("id")
		next := []clockDesign{}
		for _, d := range list {
			if d.ID != id {
				next = append(next, d)
			}
		}
		list = next
	} else if r.Method == "POST" {
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		var q clockDesign
		if json.NewDecoder(r.Body).Decode(&q) != nil || !libraryID.MatchString(q.ID) || strings.TrimSpace(q.Name) == "" || len([]rune(q.Name)) > 60 {
			fail(w, "Укажите название дизайна", 400)
			return
		}
		c := q.Config
		styles := map[string]bool{"digital": true, "stars": true, "orbit": true, "cat": true, "segments": true, "stack": true, "analog": true, "binary": true}
		if !styles[c.Style] || (c.Duration != 0 && c.Duration < 5) || c.Duration > 86400 || c.Brightness < 0 || c.Brightness > 200 || c.Speed < 1 || c.Speed > 30 || c.OffsetY < -3 || c.OffsetY > 3 || (len(c.Background) != 0 && len(c.Background) != 400) {
			fail(w, "Проверьте настройки часов", 400)
			return
		}
		replaced := false
		for i, d := range list {
			if d.ID == q.ID {
				list[i] = q
				replaced = true
			}
		}
		if !replaced {
			if len(list) >= 40 {
				fail(w, "Можно сохранить 40 дизайнов", 409)
				return
			}
			list = append(list, q)
		}
	} else {
		fail(w, "Метод не поддерживается", 405)
		return
	}
	b, e := json.Marshal(list)
	if e == nil {
		e = os.WriteFile(p+".tmp", b, 0600)
	}
	if e == nil {
		e = os.Rename(p+".tmp", p)
	}
	if e != nil {
		fail(w, "Не удалось сохранить дизайн", 500)
		return
	}
	response(w, map[string]bool{"ok": true})
}
