package main

import "testing"

func TestSetupStatusDoesNotConfuseRootAndProtection(t *testing.T) {
	for _, tt := range []struct {
		root            bool
		selinux, status string
		want            bool
	}{{true, "0", "state=PROTECTED\nprofile=0921", true}, {false, "0", "state=PROTECTED", false}, {true, "1", "state=PROTECTED", false}, {true, "0", "state=DEGRADED", false}} {
		j := setupSnapshot(tt.root, tt.selinux, tt.status, "5000000921")
		if j["protected"] != tt.want || j["adb"] != "not-tested" || j["next"] == "" {
			t.Fatalf("misleading readiness: %#v", j)
		}
	}
}
func TestSetupStatusOmitsPrivateFields(t *testing.T) {
	j := setupSnapshot(true, "0", "state=PROTECTED\ntoken=private\npassword=private\nboot_id=private\nprofile=0921", "0921")
	f := j["protection"].(map[string]string)
	if len(f) != 2 {
		t.Fatalf("unexpected fields: %#v", f)
	}
}
