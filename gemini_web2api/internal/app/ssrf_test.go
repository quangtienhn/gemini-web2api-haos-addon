package app

import (
	"net"
	"testing"
)

func TestValidateRemoteURL(t *testing.T) {
	cases := []struct {
		url  string
		ok   bool
	}{
		{"https://example.com/img.png", true},
		{"http://example.com/img.png", true},
		{"ftp://example.com/x", false},
		{"", false},
		{"http://localhost/x", false},
		{"http://127.0.0.1/x", false},
		{"http://[::1]/x", false},
		{"http://10.0.0.1/x", false},
		{"http://192.168.1.1/x", false},
		{"http://169.254.169.254/latest/meta-data/", false},
		{"http://172.16.0.1/x", false},
		{"http://0.0.0.0/x", false},
		{"http://metadata.google.internal/x", false},
	}
	for _, c := range cases {
		err := validateRemoteURL(c.url)
		if c.ok && err != nil {
			t.Errorf("expected ok for %q, got %v", c.url, err)
		}
		if !c.ok && err == nil {
			t.Errorf("expected reject for %q, got nil", c.url)
		}
	}
}

func TestIsPrivateIP(t *testing.T) {
	cases := []struct {
		ip   string
		priv bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"10.0.0.1", true},
		{"192.168.1.1", true},
		{"172.16.0.1", true},
		{"169.254.1.1", true},
		{"8.8.8.8", false},
		{"2001:4860:4860::8888", false},
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("bad ip %s", c.ip)
		}
		if isPrivateIP(ip) != c.priv {
			t.Errorf("isPrivateIP(%s) = %v, want %v", c.ip, isPrivateIP(ip), c.priv)
		}
	}
}
