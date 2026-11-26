package app

import (
	"strings"
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	cases := []struct {
		in   string
		must []string // substrings that must NOT appear
	}{
		{"Authorization: Bearer sk-abc1234567890", []string{"sk-abc1234567890"}},
		{"Cookie: SID=secretvalue; SAPISID=abc", []string{"secretvalue", "SAPISID=abc"}},
		{"url https://x/?key=SECRET&foo=bar", []string{"SECRET"}},
		{"AIzaSyABCDEFGHIJKLMNOPQRSTUVW", []string{"AIzaSyABCDEFGHIJKLMNOPQRSTUVW"}},
	}
	for _, c := range cases {
		out := redactSecrets(c.in)
		for _, m := range c.must {
			if strings.Contains(out, m) {
				t.Errorf("redact failed for %q: %q still present in %q", c.in, m, out)
			}
		}
	}
}
