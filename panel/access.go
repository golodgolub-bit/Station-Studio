package main

import (
	"log"
	"os"
	"os/exec"
	"time"
)

// Run from the same established init domain as the panel. Transition to the
// shell only after rootd has made the kernel permissive.
func accessBootstrap() {
	for n := 0; n < 180; n++ {
		if read("/sys/fs/selinux/enforce") == "0" {
			cmd := exec.Command("/system/bin/sh", "/odm/bin/station-access-boot.sh")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				log.Printf("access bootstrap: %v", err)
				return
			}
			// Retain this init service's cgroup for the detached access processes.
			for {
				time.Sleep(time.Hour)
			}
		}
		time.Sleep(time.Second)
	}
	log.Print("access bootstrap: rootd readiness timeout")
}
