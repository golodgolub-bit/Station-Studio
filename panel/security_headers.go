package main

import "net/http"

func panelHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Path) < 5 || r.URL.Path[:5] != "/api/" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		ancestor := "'none'"
		w.Header().Set("X-Frame-Options", "DENY")
		// The responsive preview can embed only this same-origin copy of the
		// static page. Authenticated APIs retain their normal frame restrictions.
		if r.URL.Path == "/" && r.URL.Query().Get("embedded-preview") == "1" {
			ancestor = "'self'"
			w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' blob: data:; media-src 'self' blob: data:; connect-src 'self'; object-src 'none'; frame-ancestors "+ancestor+"; base-uri 'none'")
		next.ServeHTTP(w, r)
	})
}
