package main

import "testing"

func TestHelperPathAllowsOnlyOwnVerifiedInstallLocations(t *testing.T) {
	for _, path := range []string{"/odm/priv-app/StationStudio/StationStudio.apk", "/data/app/local.station.studio-test==/base.apk"} {
		if !trustedHelperAPKPath(path) {
			t.Fatal(path)
		}
	}
	for _, path := range []string{"/odm/priv-app/Other/StationStudio.apk", "/data/local/tmp/StationStudio.apk", "/data/app/local.station.studio-test/base.apk\npackage:other"} {
		if trustedHelperAPKPath(path) {
			t.Fatal(path)
		}
	}
}
