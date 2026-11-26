package app

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleHealth(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/health", nil)
	handleHealth(w, r)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "ok") {
		t.Fatalf("expected ok, got %s", w.Body.String())
	}
}

func TestLimitRequestBody(t *testing.T) {
	h := limitRequestBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.WriteHeader(200)
		w.Write(b)
	}))
	// small body ok
	small := bytes.Repeat([]byte("a"), 100)
	req := httptest.NewRequest("POST", "/", bytes.NewReader(small))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("small body: got %d", w.Code)
	}
	// oversized body -> 413
	big := bytes.Repeat([]byte("a"), int(cfg.MaxChatBodyBytes)+1)
	req2 := httptest.NewRequest("POST", "/", bytes.NewReader(big))
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != 413 {
		t.Fatalf("oversized body: got %d, want 413", w2.Code)
	}
}
