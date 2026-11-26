package app

import (
	"bytes"
	"net/http/httptest"
	"testing"
)

func TestAuthOK_FailClosedEmptyToken(t *testing.T) {
	cfg.AdminToken = ""
	if authOK(httptest.NewRequest("GET", "/", nil)) {
		t.Fatal("expected false when token empty")
	}
}

func TestAuthOK_WrongToken(t *testing.T) {
	cfg.AdminToken = "secret-token"
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer wrong")
	if authOK(r) {
		t.Fatal("expected false for wrong token")
	}
}

func TestAuthOK_ValidToken(t *testing.T) {
	cfg.AdminToken = "secret-token"
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer secret-token")
	if !authOK(r) {
		t.Fatal("expected true for valid token")
	}
}

func TestAuthOK_QueryParamIgnored(t *testing.T) {
	cfg.AdminToken = "secret-token"
	r := httptest.NewRequest("GET", "/?token=secret-token", nil)
	if authOK(r) {
		t.Fatal("query param token must not authenticate")
	}
}

func TestHandleAdminLogin_NoTokenConfigured(t *testing.T) {
	cfg.AdminToken = ""
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/admin/api/login", bytes.NewReader([]byte(`{"token":"x"}`)))
	handleAdminLogin(w, r)
	if w.Code != 401 {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}
