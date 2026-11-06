package main

import "testing"

func TestBadgeRequiresNewExplicitConsent(t *testing.T) {
	for _, raw := range []string{`{}`, `{"enabled":true,"size":64,"shape":"circle"}`, `{"enabled":false,"consentVersion":1}`} {
		q := effectiveBadge([]byte(raw), true)
		if q.Enabled || q.Size != 64 || q.Shape != "circle" {
			t.Fatal("legacy configuration enabled or invalid defaults", q)
		}
	}
	q := effectiveBadge([]byte(`{"enabled":true,"consentVersion":1,"size":80,"shape":"rounded"}`), true)
	if !q.Enabled || q.Size != 80 || q.Media != "badge.media" {
		t.Fatal("explicit opt-in lost", q)
	}
	q = effectiveBadge([]byte(`{"enabled":true,"consentVersion":1}`), false)
	if q.Enabled || q.Media != "" {
		t.Fatal("enabled missing image", q)
	}
}
