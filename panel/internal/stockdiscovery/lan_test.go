package stockdiscovery

import (
	"net"
	"testing"
)

type address string

func (a address) Network() string { return "ip" }
func (a address) String() string  { return string(a) }
func TestLANSourcePrefersDirectSubnetOverVPN(t *testing.T) {
	source := sourceFor(net.ParseIP("10.17.34.237"), []net.Addr{address("10.0.0.2/8"), address("10.17.34.26/24"), address("172.19.0.1/32")})
	if source.String() != "10.17.34.26" {
		t.Fatal(source)
	}
	if sourceFor(net.ParseIP("8.8.8.8"), []net.Addr{address("10.0.0.2/8")}) != nil {
		t.Fatal("public service bound to LAN")
	}
}
