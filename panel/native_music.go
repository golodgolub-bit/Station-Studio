package main

import "strings"

// Called only after the firmware has emitted its music-specific response.
// This removes request wording for search; it never classifies an ASR request.
func nativeMusicQuery(text string) (string, bool) {
	text = strings.TrimSpace(text)
	for _, prefix := range []string{"алиса, ", "алиса "} {
		if strings.HasPrefix(strings.ToLower(text), prefix) {
			text = strings.TrimSpace(text[len(prefix):])
			break
		}
	}
	for _, prefix := range []string{"включи пожалуйста ", "поставь пожалуйста ", "воспроизведи пожалуйста ", "включи ", "поставь ", "воспроизведи ", "хочу послушать ", "дай послушать ", "послушаем "} {
		if strings.HasPrefix(strings.ToLower(text), prefix) {
			text = strings.TrimSpace(text[len(prefix):])
			break
		}
	}
	for _, prefix := range []string{"песню ", "песенку ", "трек ", "композицию ", "музыку "} {
		if strings.HasPrefix(strings.ToLower(text), prefix) {
			text = strings.TrimSpace(text[len(prefix):])
			break
		}
	}
	text = strings.Trim(text, " \"«»“”")
	text = strings.NewReplacer("\"", "", "«", "", "»", "", "“", "", "”", "").Replace(text)
	return text, len([]rune(text)) >= 2 && len([]rune(text)) <= 300
}
