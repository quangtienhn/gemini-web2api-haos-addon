package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCSRF_WriteWithoutTokenRejected(t *testing.T) {
	cfg.AdminToken = "admin-secret"
	resetLoginLimiter()
	// login to get a session cookie
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/admin/api/login", bytes.NewReader([]byte(`{"token":"admin-secret"}`)))
	handleAdminLogin(w, r)
	if w.Code != 200 {
		t.Fatalf("login failed: %d", w.Code)
	}
	var sessionCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookieName {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("no session cookie")
	}
	// POST without CSRF header -> 403
	req := httptest.NewRequest("POST", "/admin/api/proxies", bytes.NewReader([]byte(`{"name":"x","url":"http://1.2.3.4:8080"}`)))
	req.AddCookie(sessionCookie)
	rr := httptest.NewRecorder()
	requireAuth(handleAdminProxies)(rr, req)
	if rr.Code != 403 {
		t.Fatalf("expected 403 without CSRF, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCSRF_WriteWithTokenAccepted(t *testing.T) {
	cfg.AdminToken = "admin-secret"
	resetLoginLimiter()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/admin/api/login", bytes.NewReader([]byte(`{"token":"admin-secret"}`)))
	handleAdminLogin(w, r)
	sessionCookie, csrfCookie := findCookies(w.Result().Cookies())
	if sessionCookie == nil || csrfCookie == nil {
		t.Fatal("missing cookies")
	}
	req := httptest.NewRequest("POST", "/admin/api/proxies", bytes.NewReader([]byte(`{"name":"x","url":"http://1.2.3.4:8080"}`)))
	req.AddCookie(sessionCookie)
	req.AddCookie(csrfCookie)
	req.Header.Set("X-CSRF-Token", csrfCookie.Value)
	rr := httptest.NewRecorder()
	requireAuth(handleAdminProxies)(rr, req)
	// Either created (200) or validation error from proxyCreate; must not be 403 CSRF.
	if rr.Code == 403 {
		t.Fatalf("CSRF should have passed, got 403: %s", rr.Body.String())
	}
}

func TestCSRF_BearerBypasses(t *testing.T) {
	cfg.AdminToken = "admin-secret"
	req := httptest.NewRequest("POST", "/admin/api/proxies", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer admin-secret")
	rr := httptest.NewRecorder()
	requireAuth(handleAdminProxies)(rr, req)
	if rr.Code == 403 {
		t.Fatalf("Bearer auth must bypass CSRF, got 403")
	}
}

func findCookies(cs []*http.Cookie) (*http.Cookie, *http.Cookie) {
	var s, c *http.Cookie
	for _, ck := range cs {
		if ck.Name == sessionCookieName {
			s = ck
		}
		if ck.Name == csrfCookieName {
			c = ck
		}
	}
	return s, c
}

func TestCSRF_LoginSetsCSRFCookie(t *testing.T) {
	cfg.AdminToken = "admin-secret"
	resetLoginLimiter()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/admin/api/login", bytes.NewReader([]byte(`{"token":"admin-secret"}`)))
	handleAdminLogin(w, r)
	found := false
	for _, c := range w.Result().Cookies() {
		if c.Name == csrfCookieName && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("login did not set csrf cookie")
	}
	_ = json.Marshal
}
