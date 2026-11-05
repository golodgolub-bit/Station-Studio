package main

import "strings"

func trustedHelperAPKPath(path string) bool {
	if strings.ContainsAny(path, "\n\r \t") {
		return false
	}
	if path == "/odm/priv-app/StationStudio/StationStudio.apk" {
		return true
	}
	return strings.HasPrefix(path, "/data/app/local.station.studio-") && strings.HasSuffix(path, "/base.apk")
}
