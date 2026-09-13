// Package safehttp restricts both request hosts and the actual dialed IPs.
package safehttp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"
)

const UserAgent = "image-gateway/1.0"

var blocked = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("64:ff9b::/96"),
}

func PublicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, p := range blocked {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}
func Validate(u *url.URL, hosts []string) error {
	if u == nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return errors.New("upstream URL rejected")
	}
	for _, h := range hosts {
		if u.Hostname() == h {
			return nil
		}
	}
	return errors.New("upstream host rejected")
}

type policyKey struct{}

func WithHosts(ctx context.Context, hosts []string) context.Context {
	return context.WithValue(ctx, policyKey{}, hosts)
}

type transport struct{ base *http.Transport }

func (t transport) RoundTrip(r *http.Request) (*http.Response, error) {
	hosts, _ := r.Context().Value(policyKey{}).([]string)
	if err := Validate(r.URL, hosts); err != nil {
		return nil, err
	}
	r.Header.Set("User-Agent", UserAgent)
	return t.base.RoundTrip(r)
}
func New() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	t := &http.Transport{MaxIdleConns: 32, MaxIdleConnsPerHost: 4, MaxConnsPerHost: 20, IdleConnTimeout: 60 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second, DisableCompression: true, ForceAttemptHTTP2: true}
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, errors.New("upstream DNS failed")
		}
		if len(ips) == 0 {
			return nil, errors.New("empty DNS result")
		}
		for _, ip := range ips {
			if !PublicIP(ip) {
				return nil, errors.New("non-public upstream address")
			}
		}
		for _, ip := range ips {
			conn, e := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if e == nil {
				return conn, nil
			}
		}
		return nil, errors.New("upstream connection failed")
	}
	return &http.Client{Transport: transport{t}, Timeout: 3 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return nil
	}}
}
