package main

import (
	"net/http"
	"strings"
	"time"
)

// Compatibility is reported separately from account country and Play Protect.
// Root cannot make an uncertified device certified or supply a missing DRM key.
func appCompatibilityAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		fail(w, "Нужен GET", 405)
		return
	}
	prop := func(name string) string {
		v, _ := run(2*time.Second, "/system/bin/getprop", name)
		return strings.TrimSpace(v)
	}
	checks := []map[string]string{
		{"title": "Android и процессор", "detail": "Android " + prop("ro.build.version.release") + " · API " + prop("ro.build.version.sdk") + " · " + prop("ro.product.cpu.abilist")},
		{"title": "Версия для телевизора", "detail": "Выбирайте Android TV / Google TV с поддержкой пульта. Мобильный APK того же сервиса может устанавливаться, но работать иначе."},
		{"title": "Сертификация Google Play", "detail": "Статус проверяется в Google Play → Настройки → О программе → Сертификация Play Защиты. Наличие Google Play и включённая Play Защита сами по себе не подтверждают сертификацию."},
		{"title": "Netflix и DRM", "detail": "Netflix отдельно проверяет поддержку модели, сертификацию и DRM. Панель не меняет сертификацию и не обходит ограничения сервиса."},
		{"title": "Регион аккаунта", "detail": "Доступность на другом устройстве не подтверждает совместимость этой модели. Проверьте тот же аккаунт, страну Play и точную ссылку приложения. Страна аккаунта панелью не изменяется."},
	}
	if drm, e := helperMedia("compatibility", "", selectedProvider()); e == nil {
		if level, ok := drm["widevineLevel"].(string); ok && level != "" {
			checks = append(checks, map[string]string{"title": "Widevine", "detail": "Уровень, сообщённый системой: " + level + ". Это не подтверждение поддержки Netflix."})
		}
	}
	response(w, map[string]any{"checks": checks, "certification": "manual-check-required"})
}
