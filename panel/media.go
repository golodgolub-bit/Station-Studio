package main

import "strings"

// Read metadata and playback state from the same Spotify session.
func spotifySession(dump string) string { return mediaSession(dump, "com.spotify.tv.android") }
func mediaSession(dump, pkg string) string {
	lines := strings.Split(dump, "\n")
	for index, line := range lines {
		if strings.TrimSpace(line) != "package="+pkg {
			continue
		}
		start, end := index, len(lines)
		for i := index; i >= 0; i-- {
			if strings.Contains(lines[i], "(userId=") {
				start = i
				break
			}
		}
		for i := index + 1; i < len(lines); i++ {
			if strings.Contains(lines[i], "(userId=") {
				end = i
				break
			}
		}
		return strings.Join(lines[start:end], "\n")
	}
	return ""
}
