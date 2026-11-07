package stockdiscovery

import (
	"context"
	"net"
	"time"
)

// Choose the most specific directly attached IPv4 subnet, ahead of VPN defaults.
func sourceFor(ip net.IP, addresses []net.Addr) net.IP {
	if !ip.IsPrivate() {
		return nil
	}
	var chosen net.IP
	bits := -1
	for _, address := range addresses {
		local, network, e := net.ParseCIDR(address.String())
		if e != nil || local.To4() == nil || !local.IsPrivate() || !network.Contains(ip) {
			continue
		}
		ones, _ := network.Mask.Size()
		if ones > bits {
			chosen = local
			bits = ones
		}
	}
	return chosen
}
func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, _, e := net.SplitHostPort(address)
	if e != nil {
		return nil, e
	}
	ip := net.ParseIP(host)
	var source net.IP
	if ip != nil && ip.IsPrivate() {
		addresses, _ := net.InterfaceAddrs()
		source = sourceFor(ip, addresses)
	}
	dialer := net.Dialer{Timeout: 6 * time.Second, KeepAlive: 30 * time.Second}
	if source != nil {
		dialer.LocalAddr = &net.TCPAddr{IP: source}
	}
	return dialer.DialContext(ctx, network, address)
}
