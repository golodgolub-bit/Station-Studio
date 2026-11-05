package main

import "testing"

func TestProcConnectionsDecodeWithoutClaimingPurpose(t *testing.T) {
	ip, port, ok := decodeProcEndpoint("057100CB:01BB")
	if !ok || ip != "203.0.113.5" || port != 443 {
		t.Fatalf("wrong endpoint: %s %d %v", ip, port, ok)
	}
	for _, s := range []string{"0100007F:01BB", "00000000:0000", "not-an-ip:01BB", "0100007F:zzzz"} {
		if _, _, ok := decodeProcEndpoint(s); ok {
			t.Fatalf("accepted %s", s)
		}
	}
}
