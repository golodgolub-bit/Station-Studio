package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type commandTemplate struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Group   string         `json:"group"`
	Phrases []string       `json:"phrases"`
	Steps   []scenarioStep `json:"steps"`
}

func step(api string, data any) scenarioStep { b, _ := json.Marshal(data); return scenarioStep{api, b} }
func commandCatalog() []commandTemplate {
	list := []commandTemplate{}
	add := func(id, name, group string, phrases []string, steps ...scenarioStep) {
		list = append(list, commandTemplate{id, name, group, phrases, steps})
	}
	ambient := func(style string, brightness int) scenarioStep {
		return step("ambient", map[string]any{"action": "start", "style": style, "duration": 0, "brightness": brightness, "speed": 6, "font": "pixel", "layout": "two", "artistLevel": 160})
	}
	ring := func(effect, color string, brightness int) scenarioStep {
		return step("action", map[string]any{"action": "ring", "effect": effect, "color": color, "value": brightness})
	}
	for _, q := range []struct {
		id, name, style string
		phrases         []string
	}{{"clock-mini", "Обычные часы", "digital", []string{"покажи часы на станции", "включи обычные часы"}}, {"clock-big", "Крупные часы", "segments", []string{"покажи крупные часы", "включи большие часы"}}, {"clock-stack", "Часы над минутами", "stack", []string{"покажи часы над минутами"}}, {"clock-stars", "Звёздные часы", "stars", []string{"покажи звёздные часы", "включи звездные часы"}}, {"clock-orbit", "Часы с орбитой", "orbit", []string{"покажи часы с орбитой"}}, {"clock-cat", "Кошачьи часы", "cat", []string{"покажи кошачьи часы"}}, {"clock-analog", "Стрелочные часы", "analog", []string{"покажи стрелочные часы"}}, {"clock-binary", "Двоичные часы", "binary", []string{"покажи двоичные часы"}}} {
		add(q.id, q.name, "Дисплей", q.phrases, ambient(q.style, 70))
	}
	add("track-display", "Песня и исполнитель", "Дисплей", []string{"покажи название песни", "покажи исполнителя на станции"}, ambient("track", 85))
	add("native-display", "Штатный дисплей", "Дисплей", []string{"верни штатный дисплей", "выключи рисунок на станции"}, step("matrix", map[string]any{"action": "stop"}))
	for _, q := range []struct{ id, name, effect, color string }{{"ring-rainbow", "Радуга", "rainbow", "#007aff"}, {"ring-breath", "Дыхание", "breathe", "#64d2ff"}, {"ring-comet", "Комета", "comet", "#ff9f0a"}, {"ring-candle", "Свеча", "candle", "#ff9f0a"}, {"ring-red", "Красное кольцо", "solid", "#ff453a"}, {"ring-blue", "Синее кольцо", "solid", "#0a84ff"}, {"ring-pink", "Розовое кольцо", "solid", "#ff375f"}, {"ring-green", "Зелёное кольцо", "solid", "#30d158"}, {"ring-white", "Белое кольцо", "solid", "#ffffff"}} {
		phrases := map[string][]string{"ring-rainbow": {"включи радугу на кольце", "покажи радугу на кольце"}, "ring-breath": {"включи дыхание кольца", "пусть кольцо дышит"}, "ring-comet": {"включи комету на кольце"}, "ring-candle": {"сделай кольцо свечой", "включи свечу на кольце"}, "ring-red": {"сделай кольцо красным", "включи красное кольцо"}, "ring-blue": {"сделай кольцо синим", "включи синее кольцо"}, "ring-pink": {"сделай кольцо розовым", "включи розовое кольцо"}, "ring-green": {"сделай кольцо зелёным", "включи зелёное кольцо"}, "ring-white": {"сделай кольцо белым", "включи белое кольцо"}}
		add(q.id, q.name, "Кольцо", phrases[q.id], ring(q.effect, q.color, 40))
	}
	add("ring-native", "Штатное кольцо", "Кольцо", []string{"выключи подсветку кольца", "верни штатное кольцо"}, ring("off", "#007aff", 35))
	for _, q := range []struct {
		id, name, action string
		phrases          []string
	}{{"music-pause", "Пауза", "pause", []string{"музыку на паузу", "поставь музыку на паузу", "останови музыку", "выключи музыку", "выруби музыку", "отключи музыку", "останови воспроизведение", "прекрати музыку", "стоп", "хватит", "останови", "пауза"}}, {"music-play", "Продолжить музыку", "play", []string{"продолжи музыку", "продолжай музыку", "возобнови музыку", "включи музыку", "включи воспроизведение", "сними музыку с паузы", "сними с паузы", "продолжи", "продолжай", "играй", "играй музыку"}}, {"music-next", "Следующая песня", "next", []string{"следующая песня", "следующий трек", "переключи песню", "следующий", "следующая", "следующую", "следующую песню", "следующий трек пожалуйста", "переключи трек", "пропусти песню", "пропусти трек", "дальше"}}, {"music-previous", "Предыдущая песня", "previous", []string{"предыдущая песня", "предыдущий трек", "предыдущая", "предыдущий", "предыдущую", "предыдущую песню", "верни предыдущий трек", "назад"}}} {
		add(q.id, q.name, "Музыка", q.phrases, step("music/play", map[string]any{"action": q.action}))
	}
	for _, minutes := range []int{15, 30, 60, 90} {
		add(fmt.Sprintf("sleep-%d", minutes), fmt.Sprintf("Таймер музыки · %d мин", minutes), "Музыка", []string{fmt.Sprintf("выключи музыку через %d минут", minutes)}, step("action", map[string]any{"action": "sleep", "value": minutes}))
	}
	add("sleep-off", "Отменить таймер музыки", "Музыка", []string{"отмени таймер музыки"}, step("action", map[string]any{"action": "sleep", "value": 0}))
	add("music-forward", "Вперёд на 30 секунд", "Музыка", []string{"перемотай вперед на тридцать секунд", "перемотай на 30 секунд вперед"}, step("music/play", map[string]any{"action": "seek-by", "delta": 30000}))
	add("music-rewind", "Назад на 30 секунд", "Музыка", []string{"перемотай назад на тридцать секунд", "перемотай на 30 секунд назад"}, step("music/play", map[string]any{"action": "seek-by", "delta": -30000}))
	add("music-restart", "С начала песни", "Музыка", []string{"включи песню сначала", "начни трек сначала"}, step("music/play", map[string]any{"action": "seek", "position": 0}))
	add("music-repeat", "Повтор песни", "Музыка", []string{"включи повтор песни", "повторяй этот трек"}, step("music/play", map[string]any{"action": "repeat", "value": 1}))
	add("music-shuffle", "Случайный порядок", "Музыка", []string{"перемешай музыку", "включи случайный порядок музыки"}, step("music/play", map[string]any{"action": "shuffle", "value": 1}))
	add("scene-evening", "Вечер", "Сцены", []string{"включи вечернее оформление", "уютный вечер на станции"}, ambient("stars", 35), ring("breathe", "#ffb56b", 15))
	visual := step("music/visual", map[string]any{"action": "start", "mode": "spectrum", "target": "both", "source": "selected", "palette": []string{"#0a84ff", "#bf5af2", "#ff375f"}, "gain": 6, "duration": 0, "brightness": 45, "matrixBrightness": 80})
	add("scene-music", "Светомузыка", "Сцены", []string{"включи светомузыку на станции", "включи дискотеку на станции"}, visual)
	add("scene-native", "Штатный вид", "Сцены", []string{"верни штатный вид станции", "выключи оформление станции"}, step("matrix", map[string]any{"action": "stop"}), ring("off", "#007aff", 35))
	add("memory-idle", "Закрыть фоновые приложения", "Система", []string{"закрой фоновые приложения на станции"}, step("maintenance", map[string]any{"action": "idle-apps"}))
	add("open-panel", "Открыть панель на ТВ", "Система", []string{"открой панель станции", "открой станционную панель"}, step("action", map[string]any{"action": "launch", "package": "local.station.studio"}))
	list = append(list, assetCommands()...)
	list = append(list, personalCommands()...)
	owners := map[string]string{}
	ambiguous := map[string]bool{}
	for _, c := range list {
		for _, phrase := range c.Phrases {
			n := normalizePhrase(phrase)
			if owner, ok := owners[n]; ok && owner != c.ID {
				ambiguous[n] = true
			}
			owners[n] = c.ID
		}
	}
	clean := []commandTemplate{}
	for _, c := range list {
		phrases := []string{}
		for _, phrase := range c.Phrases {
			if !ambiguous[normalizePhrase(phrase)] {
				phrases = append(phrases, phrase)
			}
		}
		c.Phrases = phrases
		if len(phrases) > 0 {
			clean = append(clean, c)
		}
	}
	return clean
}
func builtInVoice(query string, settings voiceSettings) (voiceRule, bool) {
	query = normalizeVoicePhrase(query)
	if !settings.BuiltinCommands {
		return voiceRule{}, false
	}
	disabled := map[string]bool{}
	for _, id := range settings.BuiltinDisabled {
		disabled[id] = true
	}
	for _, c := range commandCatalog() {
		if disabled[c.ID] {
			continue
		}
		for _, p := range c.Phrases {
			if normalizeVoicePhrase(p) == query {
				if isShortMusicCommand(query) && !selectedMusicContext() {
					return voiceRule{}, false
				}
				return voiceRule{ID: c.ID, Name: c.Name, Enabled: true, Phrases: c.Phrases, Steps: c.Steps}, true
			}
		}
	}
	if rule, ok := mediaParameterVoice(query); ok {
		return rule, true
	}
	// Numeric parameters are bounded before being passed through normal API validation.
	for _, prefix := range []string{"громкость ", "сделай громкость ", "установи громкость "} {
		if strings.HasPrefix(query, prefix) {
			text := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(query, prefix), " процентов"))
			n, e := strconv.Atoi(text)
			if e == nil && n >= 0 && n <= 100 {
				return voiceRule{ID: "volume", Name: "Громкость", Enabled: true, Steps: []scenarioStep{step("action", map[string]any{"action": "volume", "value": n})}}, true
			}
		}
	}
	return voiceRule{}, false
}
func commandCatalogAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		scenarioMu.Lock()
		settings := voiceCfg
		custom := append([]voiceRule{}, voiceCfg.Rules...)
		scenarioMu.Unlock()
		if r.URL.Query().Get("format") == "text" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Content-Disposition", `attachment; filename="Station-Studio-commands.txt"`)
			fmt.Fprint(w, "Station Studio — голосовые команды\n\nСначала скажите «Алиса». Свои сценарии имеют приоритет. Короткие команды без имени работают только с активным выбранным музыкальным плеером. Поддержка перемотки, повтора и поиска зависит от приложения.\n")
			for _, c := range commandCatalog() {
				enabled := settings.BuiltinCommands
				for _, id := range settings.BuiltinDisabled {
					if id == c.ID {
						enabled = false
					}
				}
				state := "включено"
				if !enabled {
					state = "голос отключён"
				}
				fmt.Fprintf(w, "%s · %s · %s\n", c.Group, c.Name, state)
				for _, phrase := range c.Phrases {
					fmt.Fprintf(w, "  Алиса, %s\n", phrase)
				}
				fmt.Fprintln(w)
			}
			for _, c := range custom {
				state := "включено"
				if !c.Enabled {
					state = "отключено"
				}
				fmt.Fprintf(w, "Мои сценарии · %s · %s\n", c.Name, state)
				for _, phrase := range c.Phrases {
					fmt.Fprintf(w, "  Алиса, %s\n", phrase)
				}
			}
			fmt.Fprintln(w, "Параметры: «перемотай вперёд на 40 секунд», «перемотай назад на две минуты», «громкость 35». Громкость: от 0 до 100.\nСвои фразы и действия: раздел «Команды → Создать сценарий».")
			return
		}
		response(w, map[string]any{"commands": commandCatalog(), "customRules": custom})
		return
	}
	if r.Method != "POST" {
		fail(w, "Нужен GET или POST", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	var q struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		fail(w, "Выберите сцену", 400)
		return
	}
	for _, c := range commandCatalog() {
		if c.ID == q.ID {
			scenarioRunMu.Lock()
			e := executeSteps(c.Steps)
			scenarioRunMu.Unlock()
			if e != nil {
				fail(w, e.Error(), 409)
				return
			}
			response(w, map[string]bool{"ok": true})
			return
		}
	}
	fail(w, "Команда не найдена", 404)
}
