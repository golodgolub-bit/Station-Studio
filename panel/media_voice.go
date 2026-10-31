package main

import (
	"fmt"
	"strconv"
	"strings"
)

func normalizeVoicePhrase(q string) string {
	q = normalizePhrase(q)
	q = strings.TrimSpace(strings.TrimPrefix(q, "алиса "))
	for i := 0; i < 2; i++ {
		q = strings.TrimSpace(strings.TrimPrefix(q, "пожалуйста "))
		q = strings.TrimSpace(strings.TrimSuffix(q, " пожалуйста"))
	}
	return q
}

func isShortMusicCommand(q string) bool {
	switch q {
	case "следующий", "следующая", "следующую", "дальше", "предыдущая", "предыдущий", "предыдущую", "назад", "пауза", "продолжи", "сними с паузы", "стоп", "хватит", "останови", "играй", "продолжай":
		return true
	}
	return false
}
func selectedMusicContext() bool { return selectedMusicHasContext() }
func spokenNumber(q string) (int, bool) {
	if n, e := strconv.Atoi(q); e == nil {
		return n, n >= 0 && n <= 3600
	}
	words := map[string]int{"ноль": 0, "один": 1, "одна": 1, "одну": 1, "два": 2, "две": 2, "три": 3, "четыре": 4, "пять": 5, "шесть": 6, "семь": 7, "восемь": 8, "девять": 9, "десять": 10, "одиннадцать": 11, "двенадцать": 12, "тринадцать": 13, "четырнадцать": 14, "пятнадцать": 15, "шестнадцать": 16, "семнадцать": 17, "восемнадцать": 18, "девятнадцать": 19, "двадцать": 20, "тридцать": 30, "сорок": 40, "пятьдесят": 50, "шестьдесят": 60, "семьдесят": 70, "восемьдесят": 80, "девяносто": 90, "сто": 100}
	sum := 0
	for _, w := range strings.Fields(q) {
		n, ok := words[w]
		if !ok {
			return 0, false
		}
		sum += n
	}
	return sum, q != "" && sum <= 3600
}
func mediaParameterVoice(q string) (voiceRule, bool) {
	makeRule := func(name string, data any) (voiceRule, bool) {
		return voiceRule{ID: "media-parameter", Name: name, Enabled: true, Steps: []scenarioStep{step("music/play", data)}}, true
	}
	if q == "включи повтор песни" || q == "повторяй этот трек" {
		return makeRule("Повтор песни", map[string]any{"action": "repeat", "value": 1})
	}
	if q == "выключи повтор песни" {
		return makeRule("Выключить повтор", map[string]any{"action": "repeat", "value": 0})
	}
	if q == "перемешай музыку" || q == "включи случайный порядок музыки" {
		return makeRule("Случайный порядок", map[string]any{"action": "shuffle", "value": 1})
	}
	if q == "выключи случайный порядок музыки" {
		return makeRule("Обычный порядок", map[string]any{"action": "shuffle", "value": 0})
	}
	if q == "включи песню сначала" || q == "начни трек сначала" {
		return makeRule("С начала песни", map[string]any{"action": "seek", "position": 0})
	}
	if !strings.HasPrefix(q, "перемотай ") && !strings.HasPrefix(q, "перемотка ") {
		return voiceRule{}, false
	}
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(q, "перемотай "), "перемотка "))
	negative := strings.Contains(text, "назад")
	for _, word := range []string{"вперед", "назад", "на"} {
		text = strings.Join(strings.Fields(strings.ReplaceAll(" "+text+" ", " "+word+" ", " ")), " ")
	}
	multiplier := 1
	for _, unit := range []string{"секунду", "секунды", "секунд", "минуту", "минуты", "минут"} {
		if strings.HasSuffix(text, " "+unit) || text == unit {
			if strings.HasPrefix(unit, "мин") {
				multiplier = 60
			}
			text = strings.TrimSpace(strings.TrimSuffix(text, unit))
			if text == "" {
				text = "1"
			}
			break
		}
	}
	n, ok := spokenNumber(text)
	if !ok || n < 1 || n*multiplier > 3600 {
		return voiceRule{}, false
	}
	delta := n * multiplier * 1000
	if negative {
		delta = -delta
	}
	return makeRule(fmt.Sprintf("Перемотка %d секунд", delta/1000), map[string]any{"action": "seek-by", "delta": delta})
}
