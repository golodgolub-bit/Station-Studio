package main

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

func buildID() string {
	for _, line := range strings.Split(read("/system/build.prop"), "\n") {
		if strings.HasPrefix(line, "ro.build.version.incremental=") {
			return strings.TrimPrefix(line, "ro.build.version.incremental=")
		}
	}
	return "unknown"
}
func launch(pkg string) (string, error) {
	for _, category := range []string{"android.intent.category.LEANBACK_LAUNCHER", "android.intent.category.LAUNCHER"} {
		s, e := run(8*time.Second, "/system/bin/cmd", "package", "resolve-activity", "--brief", "-a", "android.intent.action.MAIN", "-c", category, pkg)
		if e == nil {
			for _, line := range strings.Split(s, "\n") {
				if strings.HasPrefix(line, pkg+"/") {
					return run(12*time.Second, "/system/bin/am", "start", "-n", line)
				}
			}
		}
	}
	return "", fmt.Errorf("У приложения нет экрана запуска")
}
func rgb(color string) (float64, float64, float64) {
	v, e := strconv.ParseUint(strings.TrimPrefix(color, "#"), 16, 24)
	if e != nil {
		return 0, 0, 0
	}
	return float64(v >> 16), float64(v >> 8 & 255), float64(v & 255)
}
func hsv(h, s, v float64) (float64, float64, float64) {
	h = h - math.Floor(h)
	i := int(h * 6)
	f := h*6 - float64(i)
	p, q, t := v*(1-s), v*(1-f*s), v*(1-(1-f)*s)
	switch i % 6 {
	case 0:
		return v * 255, t * 255, p * 255
	case 1:
		return q * 255, v * 255, p * 255
	case 2:
		return p * 255, v * 255, t * 255
	case 3:
		return p * 255, q * 255, v * 255
	case 4:
		return t * 255, p * 255, v * 255
	default:
		return v * 255, p * 255, q * 255
	}
}
func effects() {
	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()
	origin := time.Now()
	active := false
	var envelopes [24]float64
	autoLevel := .12
	for now := range ticker.C {
		mu.Lock()
		c := cfg
		if c.SleepAt > 0 && now.Unix() >= c.SleepAt {
			cfg.SleepAt = 0
		}
		mu.Unlock()
		if c.SleepAt > 0 && now.Unix() >= c.SleepAt {
			helperMedia("pause", "", selectedProvider())
			save()
			event("Таймер сна: музыка остановлена")
		}
		if c.Ring == "off" || assistantBusy() {
			if active {
				ringEnd()
				active = false
			}
			continue
		}
		if !active {
			ringBegin()
			active = true
		}
		t := now.Sub(origin).Seconds()
		musicMu.Lock()
		level := musicLevels
		musicMu.Unlock()
		motionTime := level.Phase
		if motionTime == 0 {
			motionTime = t
		}
		if level.Level > 0 {
			autoLevel = autoLevel*.985 + level.Level*.015
		}
		for n := 0; n < 24; n++ {
			r, g, b := rgb(c.Color)
			f := float64(c.Brightness) / 100
			switch c.Ring {
			case "pixels":
				if len(c.Palette) == 24 {
					r, g, b = rgb(c.Palette[n])
				} else {
					f = 0
				}
			case "flag":
				if len(c.Palette) > 0 {
					pos := float64(n) / 24
					if c.FlagMotion == "rotate" {
						pos = math.Mod(pos+t*.12, 1)
					}
					r, g, b = rgb(c.Palette[int(pos*float64(len(c.Palette)))%len(c.Palette)])
					if c.FlagMotion == "wave" {
						f *= .2 + .8*(math.Sin(float64(n)*.4-t*2)+1)/2
					}
					if c.FlagMotion == "breathe" {
						f *= .15 + .85*(math.Sin(t*1.7)+1)/2
					}
				}
			case "breathe":
				f *= .08 + .92*(math.Sin(t*1.7)+1)/2
			case "rainbow":
				r, g, b = hsv(float64(n)/24+t*.08, .9, 1)
			case "comet":
				d := math.Mod(float64(n)-t*7+24000, 24)
				f *= math.Exp(-d * .5)
			case "candle":
				r, g, b = 255, 100, 15
				f *= .55 + .18*math.Sin(t*3.1+float64(n)*.2) + .2*math.Sin(t*7.7)
			case "cover":
				pos := float64(n) / 24
				if c.FlagMotion == "rotate" {
					pos += t * .04
				}
				r, g, b = paletteBlend(c.Palette, pos)
				if c.FlagMotion == "wave" {
					f *= .3 + .7*(1+math.Sin(t*2-float64(n)*.3))/2
				}
				if c.FlagMotion == "breathe" {
					f *= .25 + .75*(1+math.Sin(t))/2
				}
			case "music":
				if c.PaletteSource == "cover" && len(c.Palette) == 0 {
					f = 0
				}
				if len(c.Palette) > 0 {
					pos := float64(n) / 24
					if c.FlagMotion == "rotate" || c.FlagMotion == "" {
						pos += motionTime * .04
					}
					r, g, b = paletteBlend(c.Palette, pos)
				}
				v := 0.0
				sensitivity := level.Gain / math.Max(.08, autoLevel*4)
				switch level.Mode {
				case "mirror":
					band := int(math.Abs(float64(n)-11.5) * 16 / 12)
					v = math.Sqrt(math.Max(0, level.Bands[band]*sensitivity*3))
				case "orbit":
					d := math.Mod(float64(n)-motionTime*5+2400000, 24)
					v = math.Sqrt(math.Max(0, level.Level*sensitivity)) * math.Exp(-d*.25)
				case "ribbon":
					v = math.Sqrt(math.Max(0, level.Level*sensitivity)) * (.2 + .8*(1+math.Sin(float64(n)*.5-motionTime*2))/2)
				case "wave":
					v = math.Abs(level.Wave[n*25/24]) * sensitivity
				case "pulse":
					v = level.Level * sensitivity
				case "vu":
					v = math.Max(0, math.Min(1, (math.Sqrt(level.Level*sensitivity)*12-float64(n%12))))
				default:
					band := n * 16 / 24
					v = math.Sqrt(math.Max(0, level.Bands[band]*sensitivity*3))
				}
				v = math.Max(0, math.Min(1, v))
				if level.Level < math.Max(.00001, level.NoiseFloor) {
					v = 0
				}
				attack := level.Decay
				if attack == 0 {
					attack = .18
				}
				if v > envelopes[n] {
					attack = level.Attack
					if attack == 0 {
						attack = .8
					}
				}
				envelopes[n] += (v - envelopes[n]) * attack
				f *= envelopes[n]
				if c.FlagMotion == "wave" {
					f *= .3 + .7*(1+math.Sin(motionTime*2-float64(n)*.3))/2
				}
				if c.FlagMotion == "breathe" {
					f *= .25 + .75*(1+math.Sin(motionTime))/2
				}
			}
			r, g, b = math.Max(0, math.Min(255, r*f)), math.Max(0, math.Min(255, g*f)), math.Max(0, math.Min(255, b*f))
			if !ringPixel(n+1, r, g, b) {
				for color, value := range map[string]float64{"red": r, "green": g, "blue": b} {
					os.WriteFile(fmt.Sprintf("/sys/class/leds/station2:%s:ring%d/brightness", color, n+1), []byte(strconv.Itoa(int(value))), 0644)
				}
			}
		}
		ringFlush()
	}
}

func paletteBlend(palette []string, position float64) (float64, float64, float64) {
	if len(palette) == 0 {
		return 0, 0, 0
	}
	position -= math.Floor(position)
	p := position * float64(len(palette))
	i := int(p)
	mix := p - float64(i)
	r, g, b := rgb(palette[i])
	r2, g2, b2 := rgb(palette[(i+1)%len(palette)])
	return r + (r2-r)*mix, g + (g2-g)*mix, b + (b2-b)*mix
}
