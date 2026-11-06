package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
)

func homeColorModel(d homeDevice) string {
	if d.State == "offline" || !strings.HasPrefix(d.Type, "devices.types.light") {
		return ""
	}
	for _, c := range d.Capabilities {
		if c.Type == "devices.capabilities.color_setting" {
			m, _ := c.Parameters["color_model"].(string)
			if m == "rgb" || m == "hsv" {
				return m
			}
		}
	}
	return ""
}
func homeLightList() ([]lamp, error) {
	var info homeInfo
	if !homeConnected() {
		return []lamp{}, nil
	}
	if e := homeCall(homeToken(), "GET", "/v1.0/user/info", nil, &info); e != nil {
		return nil, e
	}
	lamps := []lamp{}
	for _, d := range info.Devices {
		if homeColorModel(d) != "" {
			lamps = append(lamps, lamp{ID: "home:" + d.ID, Name: d.Name, Kind: "yandex", Host: "Дом с Алисой"})
		}
	}
	return lamps, nil
}
func homeLightsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		fail(w, "Нужен GET", 405)
		return
	}
	lamps, e := homeLightList()
	if e != nil {
		fail(w, e.Error(), 502)
		return
	}
	response(w, map[string]any{"lamps": lamps, "minimumInterval": 4, "notice": "Облачный свет обновляется раз в 4 секунды или реже. Для быстрой реакции используйте LAN Control."})
}
func homeLightSnapshot(l lamp) (homeDevice, error) {
	var d homeDevice
	id := strings.TrimPrefix(l.ID, "home:")
	if !homeID.MatchString(id) {
		return d, fmt.Errorf("Некорректная лампа")
	}
	e := homeCall(homeToken(), "GET", "/v1.0/devices/"+url.PathEscape(id), nil, &d)
	if e != nil {
		return d, e
	}
	if d.ID != id || homeColorModel(d) == "" {
		return d, fmt.Errorf("Лампа недоступна или не поддерживает цвет")
	}
	on := false
	for _, c := range d.Capabilities {
		if c.Type == "devices.capabilities.on_off" {
			on = c.State["value"] == true
		}
	}
	if !on {
		return d, fmt.Errorf("Сначала включите лампу «%s» в доме", l.Name)
	}
	return d, nil
}
func homeLightAction(id, typ, instance string, value any) error {
	q := homeAction{ID: id}
	raw, _ := json.Marshal(map[string]any{"id": id, "actions": []any{map[string]any{"type": typ, "state": map[string]any{"instance": instance, "value": value}}}})
	_ = json.Unmarshal(raw, &q)
	_, e := homeExecute(q)
	return e
}
func homeLightColor(d homeDevice, rgb [3]int, brightness int) error {
	// HSV carries brightness in the same request. RGB lamps use a separately
	// advertised brightness capability; no invented capability is sent.
	if homeColorModel(d) == "hsv" {
		r, g, b := float64(rgb[0])/255, float64(rgb[1])/255, float64(rgb[2])/255
		hi, lo := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
		delta := hi - lo
		h, s := 0.0, 0.0
		if delta > 0 {
			s = delta / hi * 100
			switch hi {
			case r:
				h = 60 * math.Mod((g-b)/delta, 6)
			case g:
				h = 60 * ((b-r)/delta + 2)
			default:
				h = 60 * ((r-g)/delta + 4)
			}
			if h < 0 {
				h += 360
			}
		}
		return homeLightAction(d.ID, "devices.capabilities.color_setting", "hsv", map[string]any{"h": math.Round(h), "s": math.Round(s), "v": float64(brightness)})
	}
	// Scale the RGB value for RGB-only lights rather than issuing an unsupported
	// brightness command. This also bounds load to one color request per lamp.
	value := (rgb[0]*brightness/100)<<16 | (rgb[1]*brightness/100)<<8 | rgb[2]*brightness/100
	return homeLightAction(d.ID, "devices.capabilities.color_setting", "rgb", value)
}
func restoreHomeLight(d homeDevice) error {
	for _, c := range d.Capabilities {
		if c.Type == "devices.capabilities.color_setting" {
			i, _ := c.State["instance"].(string)
			if i == "rgb" || i == "hsv" || i == "temperature_k" {
				return homeLightAction(d.ID, c.Type, i, c.State["value"])
			}
		}
	}
	return fmt.Errorf("Исходный цвет «%s» недоступен для восстановления", d.Name)
}
