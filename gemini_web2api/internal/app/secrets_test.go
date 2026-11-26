package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCookieCipherRoundTrip(t *testing.T) {
	dir, _ := os.MkdirTemp("", "cipher-")
	defer os.RemoveAll(dir)
	cfg.DBPath = filepath.Join(dir, "t.db")
	ensureCookieCipher()
	enc := encryptSecret("SID=abc; SAPISID=xyz")
	if enc == "" || !isEncrypted(enc) {
		t.Fatalf("encrypt failed: %q", enc)
	}
	if got := decryptSecret(enc); got != "SID=abc; SAPISID=xyz" {
		t.Fatalf("roundtrip mismatch: %q", got)
	}
}

func TestCookieCipherLegacyPlaintext(t *testing.T) {
	if got := decryptSecret("SID=plain"); got != "SID=plain" {
		t.Fatalf("legacy plaintext not passed through: %q", got)
	}
}

func isEncrypted(s string) bool {
	return len(s) > len("enc:v1:") && s[:7] == "enc:v1:"
}
