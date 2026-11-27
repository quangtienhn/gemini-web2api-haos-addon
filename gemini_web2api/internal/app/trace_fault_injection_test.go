package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTraceFaultInjection_UpstreamTimeoutViaHTTP(t *testing.T) {
	// Real httptest.Server that delays longer than client timeout.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	trace := newRequestTrace("chat.completions", "gemini-3.6-flash", true)
	trace.MarkUpstreamRequestStart()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
	_, err := http.DefaultClient.Do(req)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	trace.FinishAttempt(0, "timeout", "attempt_1_timeout", 0)
	trace.SetFinish(502, "timeout", "upstream timeout")
	m := trace.SnapshotMap()
	if m["status"].(int) != 502 {
		t.Fatalf("status=%v", m["status"])
	}
	if ats, ok := m["attempt_timings"].([]map[string]interface{}); !ok || len(ats) < 1 {
		t.Fatalf("attempt_timings missing: %v", m["attempt_timings"])
	}
}

func TestTraceFaultInjection_ClientDisconnectWriter(t *testing.T) {
	setupTraceHandlerTest(t)
	ch := captureTraceCallGemini(t, func(prompt, latest string, mc ModelConfig, tools []map[string]interface{}, images []pendingUpload, onDelta, onReasoning func(string), trace *RequestTrace) (string, []ToolCall, *StreamResult, error) {
		trace.MarkUpstreamRequestStart()
		trace.MarkFirstUpstreamByte()
		trace.MarkFirstParsedFrame()
		if onDelta != nil {
			onDelta("hello")
		}
		trace.MarkFirstSemanticDelta()
		trace.MarkUpstreamFinished()
		return "hello", nil, &StreamResult{UpstreamModel: "3.6 Flash"}, nil
	})
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(chatReqBody("gemini-3.6-flash", true, false)))
	w := &failingWriter{ResponseRecorder: httptest.NewRecorder(), failAfter: 1}
	handleChatCompletions(w, r)
	tr := waitTrace(t, ch)
	if tr == nil {
		t.Fatal("nil trace")
	}
	m := tr.SnapshotMap()
	if m["request_id"] == "" {
		t.Fatal("missing request_id")
	}
	if m["status"] == nil {
		t.Fatal("missing status")
	}
}

type failingWriter struct {
	*httptest.ResponseRecorder
	failAfter int
	writes    int
}

func (f *failingWriter) Write(b []byte) (int, error) {
	f.writes++
	if f.writes > f.failAfter {
		return 0, context.Canceled
	}
	return f.ResponseRecorder.Write(b)
}

func (f *failingWriter) Flush() {}
