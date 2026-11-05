package main

import "net/http"

func musicLibrarySearchAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		fail(w, "Нужен GET", 405)
		return
	}
	query := r.URL.Query().Get("query")
	if len([]rune(query)) < 2 || len(query) > 500 {
		fail(w, "Введите название песни или исполнителя", 400)
		return
	}
	result, e := helperMedia("library-search", query, selectedProvider())
	if e != nil {
		if r.URL.Query().Get("details") == "1" && result != nil {
			w.WriteHeader(409)
			response(w, result)
			return
		}
		fail(w, e.Error(), 409)
		return
	}
	response(w, result)
}

func musicSessionAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		fail(w, "Нужен GET", 405)
		return
	}
	result, e := helperMedia("metadata", "", selectedProvider())
	if e != nil {
		response(w, map[string]any{"available": false, "error": e.Error(), "supported": []string{}})
		return
	}
	mask, _ := result["actions"].(float64)
	actions := uint64(mask)
	supported := []string{}
	for _, a := range []struct {
		name string
		mask uint64
	}{{"play", 4 | 512}, {"pause", 2 | 512}, {"previous", 16}, {"next", 32}, {"seek", 256}, {"search", 2048}, {"repeat", 262144}, {"shuffle", 2097152}} {
		if actions&a.mask != 0 {
			supported = append(supported, a.name)
		}
	}
	result["available"] = true
	result["supported"] = supported
	response(w, result)
}
