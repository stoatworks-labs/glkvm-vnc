package netutil

import (
	"net"
	"testing"
)

func TestHostAllowed(t *testing.T) {
	nets := ParseCIDRs([]string{"127.0.0.0/8", "10.0.0.0/8"})
	cases := []struct {
		host string
		want bool
	}{
		{"127.0.0.1", true},
		{"10.4.5.6", true},
		{"8.8.8.8", false},
		{"192.168.1.1", false},
	}
	for _, c := range cases {
		if got := HostAllowed(nets, c.host); got != c.want {
			t.Errorf("HostAllowed(%s) = %v, want %v", c.host, got, c.want)
		}
	}
}

func TestEmptyAllowlistAllowsAll(t *testing.T) {
	if !HostAllowed(nil, "8.8.8.8") {
		t.Fatal("empty allowlist should allow all")
	}
	if !IPAllowed(nil, net.ParseIP("1.2.3.4")) {
		t.Fatal("empty allowlist should allow all IPs")
	}
}

func TestSplitCommaList(t *testing.T) {
	got := SplitCommaList("127.0.0.0/8, 10.0.0.0/8\n192.168.0.0/16")
	if len(got) != 3 {
		t.Fatalf("got %v, want 3 items", got)
	}
}
