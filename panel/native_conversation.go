package main

import (
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The firmware's writable conversation Binder requires a registered Android
// application caller. The helper supplies its own application thread; this
// root broker keeps the existing root identity for the protected service.
func nativeConversationWorker() {
	for {
		func() {
			info, e := os.Stat(avatarFolder)
			if e != nil {
				return
			}
			uid := info.Sys().(*syscall.Stat_t).Uid
			if uid < 10000 {
				return
			}
			out, e := run(5*time.Second, "/system/bin/pm", "path", "local.station.studio")
			if e != nil {
				return
			}
			path := strings.TrimPrefix(strings.TrimSpace(out), "package:")
			if !trustedHelperAPKPath(path) {
				return
			}
			// Use the same shell domain transition as other Android framework calls.
			cmd := exec.Command("/system/bin/sh", "-c", "exec \"$@\"", "station-conversation", "/system/bin/app_process", "/system/bin", "local.station.studio.NativeConversation", strconv.FormatUint(uint64(uid), 10))
			cmd.Env = append(os.Environ(), "CLASSPATH="+path)
			cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
			cmd.Stdout = io.Discard
			cmd.Stderr = io.Discard
			cmd.Run()
		}()
		time.Sleep(10 * time.Second)
	}
}
