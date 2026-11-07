package main

import (
	"net/http"
	"os"
	"strings"
	"time"
)

// Read-only readiness snapshot. A listening port or mDNS announcement is never
// reported as an authorized ADB channel. No credentials or account data here.
func setupSnapshot(root bool, selinux, status, build string) map[string]any {
	fields := map[string]string{}
	for _, line := range strings.Split(status, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			switch k {
			case "state", "profile", "dns", "dot", "reason":
				fields[k] = v
			}
		}
	}
	protected := root && selinux == "0" && fields["state"] == "PROTECTED"
	next := "Панель работает. Для обычного управления ADB, AliceBridge и Shizuku не нужны."
	if !root {
		next = "Агент запущен без прав root. Откройте мастер доступа; не запускайте получение root повторно, пока не проверен журнал."
	} else if selinux != "0" {
		next = "Root есть, но SELinux ограничивает доступ. Проверьте автозапуск совместимого Hybrid Root."
	} else if !protected {
		next = "Root работает, но защита автозапуска не подтверждена. Откройте состояние Hybrid Root перед перезагрузкой."
	}
	return map[string]any{"ok": true, "model": "yandexstation_2", "build": build, "root": root, "permissive": selinux == "0", "protected": protected, "protection": fields, "panel": true, "adb": "not-tested", "next": next, "checkedAt": time.Now().Unix()}
}
func setupStatusAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		fail(w, "Нужен GET", 405)
		return
	}
	response(w, setupSnapshot(os.Geteuid() == 0, read("/sys/fs/selinux/enforce"), read("/data/local/tmp/rootstatus"), buildID()))
}
