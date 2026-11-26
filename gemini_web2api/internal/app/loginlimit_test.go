package app

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestLoginRateLimit_LocksAfterThreshold(t *testing.T) {
	cfg.AdminToken = "admin-secret"
	cfg.LoginRateLimit = 3
	cfg.LoginRateWindowSec = 60
	resetLoginLimiter()
	now := time.Now()
	ip := "10.0.0.1"
	for i := 0; i < 3; i++ {
		loginGuard.fail(ip, now, cfg.LoginRateLimit, time.Duration(cfg.LoginRateWindowSec)*time.Second)
	}
	ok, retry := loginGuard.allow(ip, now, cfg.LoginRateLimit, time.Duration(cfg.LoginRateWindowSec)*time.Second)
	if ok {
		t.Fatal("expected lockout after 3 failures")
	}
	if retry <= 0 {
		t.Fatalf("expected positive retry, got %d", retry)
	}
	// success clears
	loginGuard.success(ip)
	ok, _ = loginGuard.allow(ip, now, cfg.LoginRateLimit, time.Duration(cfg.LoginRateWindowSec)*time.Second)
	if !ok {
		t.Fatal("success should clear lockout")
	}
	_ = httptest.NewRecorder
}
