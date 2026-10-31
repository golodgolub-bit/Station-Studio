package main

import (
	"bytes"
	"fmt"

	"os"
	"path/filepath"
	"syscall"
	"time"
)

const avatarFolder = "/data/user/0/local.station.studio/files"

func helperWrite(name string, data []byte) error {
	folder, err := os.Stat(avatarFolder)
	if err != nil {
		return err
	}
	st := folder.Sys().(*syscall.Stat_t)
	path := filepath.Join(avatarFolder, name)
	temp := path + ".studio-new"
	if err = os.WriteFile(temp, data, 0600); err != nil {
		return err
	}
	defer os.Remove(temp)
	if err = os.Chown(temp, int(st.Uid), int(st.Gid)); err != nil {
		return err
	}
	label := make([]byte, 512)
	n, err := syscall.Getxattr(avatarFolder, "security.selinux", label)
	if err != nil {
		return err
	}
	if err = syscall.Setxattr(temp, "security.selinux", label[:n], 0); err != nil {
		return err
	}
	return os.Rename(temp, path)
}
func visualService(operation string) error {
	out, e := run(15*time.Second, "/system/bin/am", "start-foreground-service", "--user", "0", "-n", "local.station.studio/.VisualService", "--es", "operation", operation)
	if e != nil {
		return e
	}
	if bytes.Contains([]byte(out), []byte("Error")) {
		return fmt.Errorf("%s", out)
	}
	return nil
}
func resumeAvatar() {
	for i := 0; i < 180; i++ {
		if read("/sys/fs/selinux/enforce") == "0" {
			break
		}
		time.Sleep(time.Second)
	}
	os.Remove(avatarFolder + "/avatar-enabled")
	visualService("observe")
	for i := 0; i < 30; i++ {
		if helperWrite("panel.key", []byte(token)) == nil {
			break
		}
		time.Sleep(time.Second)
	}
	run(10*time.Second, "/system/bin/am", "start-foreground-service", "--user", "0", "-n", "local.station.studio/.AssetsService")
}
