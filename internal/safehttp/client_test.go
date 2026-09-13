package safehttp

import (
	"context"
	"net/http"
	"net/netip"
	"net/url"
	"testing"
)

func TestPublicIP(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "::1", "::ffff:127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254", "fe80::1", "fc00::1", "0.0.0.0", "100.100.100.200", "224.0.0.1", "198.18.0.1", "64:ff9b::a00:1", "2002:7f00:1::"} {
		if PublicIP(netip.MustParseAddr(s)) {
			t.Errorf("accepted %s", s)
		}
	}
	if !PublicIP(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("rejected public IP")
	}
}
func TestHostPolicy(t *testing.T) {
	for _, s := range []string{"https://localhost/a", "https://cdn.donmai.us.evil.com/a", "https://cdn.donmai.us:8443/a", "http://cdn.donmai.us/a", "https://user@cdn.donmai.us/a"} {
		u, _ := url.Parse(s)
		if Validate(u, []string{"cdn.donmai.us"}) == nil {
			t.Errorf("accepted %s", s)
		}
	}
}
func TestTransportRejectsRedirectTarget(t *testing.T) {
	c := New()
	ctx := WithHosts(context.Background(), []string{"cdn.donmai.us"})
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1/", nil)
	if _, e := c.Do(req); e == nil {
		t.Fatal("accepted private redirect target")
	}
	req, _ = http.NewRequestWithContext(ctx, "GET", "https://cdn.donmai.us/a", nil)
	if c.CheckRedirect(req, make([]*http.Request, 5)) == nil {
		t.Fatal("no redirect limit")
	}
}
