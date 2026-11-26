package app

import (
	"bytes"
	"net/http/httptest"
	"testing"
)

func TestMCPAuth_RequiresAPIKey(t *testing.T) {
	saved := getAPIKey()
	apiKeyRuntime.Store("test-key-0123456789abcdef")
	defer apiKeyRuntime.Store(saved)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	rr := httptest.NewRecorder()
	requireAPIKey(handleMCPHTTP)(rr, req)
	if rr.Code != 401 {
		t.Fatalf("expected 401 without key, got %d", rr.Code)
	}
	req2 := httptest.NewRequest("POST", "/mcp", bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	req2.Header.Set("Authorization", "Bearer test-key-0123456789abcdef")
	rr2 := httptest.NewRecorder()
	requireAPIKey(handleMCPHTTP)(rr2, req2)
	if rr2.Code != 200 {
		t.Fatalf("expected 200 with key, got %d: %s", rr2.Code, rr2.Body.String())
	}
}

func TestMCP_UnknownToolReturnsError(t *testing.T) {
	saved := getAPIKey()
	apiKeyRuntime.Store("test-key-0123456789abcdef")
	defer apiKeyRuntime.Store(saved)
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nope","arguments":{}}}`
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer test-key-0123456789abcdef")
	rr := httptest.NewRecorder()
	requireAPIKey(handleMCPHTTP)(rr, req)
	if !bytes.Contains(rr.Body.Bytes(), []byte("unknown tool")) {
		t.Fatalf("expected unknown tool error, got %s", rr.Body.String())
	}
}
