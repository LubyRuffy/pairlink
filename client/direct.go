package client

import (
	"net"
	"strconv"
)

func srcZero(src [32]byte) bool {
	for _, b := range src {
		if b != 0 {
			return false
		}
	}
	return true
}

func endpointKey(addr *net.UDPAddr) string {
	if addr == nil || addr.IP == nil {
		return ""
	}
	ip := addr.IP
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return net.JoinHostPort(ip.String(), strconv.Itoa(addr.Port))
}

func udpSame(a, b *net.UDPAddr) bool {
	key := endpointKey(a)
	return key != "" && key == endpointKey(b)
}

func rememberEndpoints(l *Link, eps []string) {
	set := map[string]struct{}{}
	var addrs []*net.UDPAddr
	seen := map[string]struct{}{}
	for _, ep := range eps {
		addr, err := net.ResolveUDPAddr("udp", ep)
		if err != nil {
			continue
		}
		key := endpointKey(addr)
		if key == "" {
			continue
		}
		set[key] = struct{}{}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		addrs = append(addrs, addr)
	}
	l.peers.Store(set)
	l.endpoints.Store(addrs)
	l.gotDisco.Store(true)
}

func allowPeer(l *Link, from *net.UDPAddr) bool {
	if l == nil || from == nil {
		return false
	}
	set, _ := l.peers.Load().(map[string]struct{})
	if len(set) == 0 {
		return false
	}
	_, ok := set[endpointKey(from)]
	return ok
}
