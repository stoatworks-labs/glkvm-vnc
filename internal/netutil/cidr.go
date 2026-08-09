// Package netutil holds small networking helpers, notably the CIDR allowlist
// used to constrain which hosts a direct-dial endpoint may reach.
package netutil

import (
	"net"
	"strings"
)

// ParseCIDRs parses CIDR strings (bare IPs are accepted as /32 or /128),
// skipping malformed entries.
func ParseCIDRs(cidrs []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !strings.Contains(c, "/") {
			if ip := net.ParseIP(c); ip != nil {
				if ip.To4() != nil {
					c += "/32"
				} else {
					c += "/128"
				}
			}
		}
		if _, n, err := net.ParseCIDR(c); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// IPAllowed reports whether ip is within any of nets. Empty nets = allowed.
func IPAllowed(nets []*net.IPNet, ip net.IP) bool {
	if len(nets) == 0 {
		return true
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// HostAllowed resolves host (IP literal or hostname) and reports whether it is
// permitted by nets. For hostnames every resolved address must be allowed
// (fail closed against split-horizon / rebinding). Empty nets = allowed.
func HostAllowed(nets []*net.IPNet, host string) bool {
	if len(nets) == 0 {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return IPAllowed(nets, ip)
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if !IPAllowed(nets, ip) {
			return false
		}
	}
	return true
}

// SplitCommaList splits a comma/space/newline-separated list into cleaned,
// non-empty items.
func SplitCommaList(s string) []string {
	s = strings.NewReplacer(",", " ", "\n", " ", "\t", " ").Replace(s)
	fields := strings.Fields(s)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}
