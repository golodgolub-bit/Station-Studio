package main

import "testing"

func TestAndroidDNSUsesOnlyActiveNetwork(t *testing.T) {
	raw := `Active default network: 102
 NetworkAgentInfo{ network{100} DnsAddresses: [203.0.113.99,] UsePrivateDns: false PrivateDnsServerName: null }
 NetworkAgentInfo{ network{102} DnsAddresses: [/192.0.2.53,2001:db8::53,] UsePrivateDns: false PrivateDnsServerName: null }
  2026-01-01 OBSERVED DnsAddresses: [203.0.113.100,]`
	c, e := parseAndroidDNS(raw)
	if e != nil {
		t.Fatal(e)
	}
	if len(c.Servers) != 2 || c.Servers[0] != "192.0.2.53" || c.Servers[1] != "2001:db8::53" || c.Private {
		t.Fatalf("wrong network: %+v", c)
	}
}
func TestAndroidDNSPreservesPrivateDNSAndRejectsStaleFallback(t *testing.T) {
	c, e := parseAndroidDNS("Active default network: 100\nNetworkAgentInfo{ network{100} DnsAddresses: [192.0.2.53,] UsePrivateDns: true PrivateDnsServerName: resolver.example }")
	if e != nil || !c.Private || c.Hostname != "resolver.example" {
		t.Fatalf("private DNS lost: %+v %v", c, e)
	}
	for _, raw := range []string{
		"Active default network: none\nNetworkAgentInfo{ network{100} DnsAddresses: [192.0.2.53,] }",
		"Active default network: 101\nNetworkAgentInfo{ network{100} DnsAddresses: [192.0.2.53,] }",
		"Active default network: 100\nNetworkAgentInfo{ network{100} DnsAddresses: [0.0.0.0,garbage,] }",
	} {
		if _, e := parseAndroidDNS(raw); e == nil {
			t.Fatal("invalid network accepted")
		}
	}
}
