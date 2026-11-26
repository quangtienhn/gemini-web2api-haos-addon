package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireAPIKeyAcceptsGoogleSDKForms(t *testing.T) {
	saved := getAPIKey()
	apiKeyRuntime.Store("unit-test-api-key")
	defer apiKeyRuntime.Store(saved)

	h := requireAPIKey(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	cases := []struct {
		name string
		set  func(r *http.Request)
		want int
	}{
		{"bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer unit-test-api-key") }, 204},
		{"x-api-key", func(r *http.Request) { r.Header.Set("x-api-key", "unit-test-api-key") }, 204},
		{"x-goog-api-key", func(r *http.Request) { r.Header.Set("x-goog-api-key", "unit-test-api-key") }, 204},
		{"query (no longer accepted)", func(r *http.Request) { r.URL.RawQuery = "key=unit-test-api-key&alt=sse" }, 401},
		{"wrong", func(r *http.Request) { r.Header.Set("x-goog-api-key", "nope") }, 401},
		{"none", func(r *http.Request) {}, 401},
	}
	for _, c := range cases {
		req := httptest.NewRequest("POST", "/v1beta/models/x:generateContent", nil)
		c.set(req)
		w := httptest.NewRecorder()
		h(w, req)
		if w.Code != c.want {
			t.Errorf("%s: got %d, want %d", c.name, w.Code, c.want)
		}
	}
}


func TestInitAPIKeyReadsEnvironment(t *testing.T) {
	savedKey := getAPIKey()
	savedLocked := apiKeyLocked
	defer func() {
		apiKeyRuntime.Store(savedKey)
		apiKeyLocked = savedLocked
	}()

	t.Setenv("API_KEY", "unit-test-api-key-from-env")
	got := initAPIKey("")
	if got != "unit-test-api-key-from-env" {
		t.Fatalf("initAPIKey() = %q, want environment API key", got)
	}
	if !apiKeyLocked {
		t.Fatal("API_KEY environment value must lock the API key at startup")
	}
}
