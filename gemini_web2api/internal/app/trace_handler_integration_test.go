package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func setupTraceHandlerTest(t *testing.T) {
	t.Helper()
	oldCfg := cfg
	oldRT := rtVal
	cfg = defaultConfig()
	cfg.MaxChatBodyBytes = 32 << 20
	rtMu.Lock()
	rtVal.MultiTurn = false
	rtMu.Unlock()
	t.Cleanup(func() {
		cfg = oldCfg
		rtMu.Lock()
		rtVal = oldRT
		rtMu.Unlock()
	})
}

func captureTraceCallGemini(t *testing.T, fn func(prompt, latest string, mc ModelConfig, tools []map[string]interface{}, images []pendingUpload, onDelta, onReasoning func(string), trace *RequestTrace) (string, []ToolCall, *StreamResult, error)) <-chan *RequestTrace {
	t.Helper()
	old := callGemini
	ch := make(chan *RequestTrace, 8)
	callGemini = func(prompt, latest string, mc ModelConfig, tools []map[string]interface{}, images []pendingUpload, onDelta, onReasoning func(string), trace *RequestTrace) (string, []ToolCall, *StreamResult, error) {
		ch <- trace
		return fn(prompt, latest, mc, tools, images, onDelta, onReasoning, trace)
	}
	t.Cleanup(func() { callGemini = old })
	return ch
}

func chatReqBody(model string, stream bool, tools bool) string {
	body := map[string]interface{}{
		"model":    model,
		"messages": []map[string]interface{}{{"role": "user", "content": "hello"}},
		"stream":   stream,
	}
	if tools {
		body["tools"] = []map[string]interface{}{{"type": "function", "function": map[string]interface{}{"name": "get_weather", "parameters": map[string]interface{}{"type": "object"}}}}
	}
	b, _ := json.Marshal(body)
	return string(b)
}

func respReqBody(model string, stream bool) string {
	body := map[string]interface{}{
		"model":  model,
		"input":  "hello",
		"stream": stream,
	}
	b, _ := json.Marshal(body)
	return string(b)
}

func waitTrace(t *testing.T, ch <-chan *RequestTrace) *RequestTrace {
	t.Helper()
	select {
	case tr := <-ch:
		return tr
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for trace")
		return nil
	}
}

func TestTraceHandler_ChatNonStreamingSuccess(t *testing.T) {
	setupTraceHandlerTest(t)
	ch := captureTraceCallGemini(t, func(prompt, latest string, mc ModelConfig, tools []map[string]interface{}, images []pendingUpload, onDelta, onReasoning func(string), trace *RequestTrace) (string, []ToolCall, *StreamResult, error) {
		trace.MarkUpstreamRequestStart()
		trace.MarkFirstUpstreamByte()
		trace.MarkFirstParsedFrame()
		trace.MarkFirstSemanticDelta()
		trace.MarkUpstreamFinished()
		return "ok", nil, &StreamResult{UpstreamModel: "3.6 Flash"}, nil
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(chatReqBody("gemini-3.6-flash", false, false)))
	handleChatCompletions(w, r)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	tr := waitTrace(t, ch)
	m := tr.SnapshotMap()
	if m["stream"].(bool) {
		t.Fatal("expected non-streaming")
	}
	if m["first_client_sse_ms"].(int64) != -1 {
		t.Fatal("non-streaming should not set SSE milestone")
	}
	if m["status"].(int) != 200 {
		t.Fatalf("status=%v", m["status"])
	}
	if m["request_id"] == "" {
		t.Fatal("missing request_id")
	}
}

func TestTraceHandler_ChatStreamingSuccess(t *testing.T) {
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
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(chatReqBody("gemini-3.6-flash", true, false)))
	handleChatCompletions(w, r)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	tr := waitTrace(t, ch)
	m := tr.SnapshotMap()
	if !m["stream"].(bool) {
		t.Fatal("expected streaming")
	}
	if m["first_client_sse_ms"].(int64) < 0 {
		t.Fatal("missing first_client_sse_ms")
	}
	if m["first_client_content_ms"].(int64) < 0 {
		t.Fatal("missing first_client_content_ms")
	}
}

func TestTraceHandler_ResponsesNonStreaming(t *testing.T) {
	setupTraceHandlerTest(t)
	ch := captureTraceCallGemini(t, func(prompt, latest string, mc ModelConfig, tools []map[string]interface{}, images []pendingUpload, onDelta, onReasoning func(string), trace *RequestTrace) (string, []ToolCall, *StreamResult, error) {
		trace.MarkUpstreamRequestStart()
		trace.MarkFirstUpstreamByte()
		trace.MarkFirstParsedFrame()
		trace.MarkFirstSemanticDelta()
		trace.MarkUpstreamFinished()
		return "ok", nil, &StreamResult{UpstreamModel: "3.6 Flash"}, nil
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(respReqBody("gemini-3.6-flash", false)))
	handleResponses(w, r)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	tr := waitTrace(t, ch)
	m := tr.SnapshotMap()
	if m["endpoint"].(string) != "responses" {
		t.Fatalf("endpoint=%v", m["endpoint"])
	}
	if m["status"].(int) != 200 {
		t.Fatalf("status=%v", m["status"])
	}
}

func TestTraceHandler_ResponsesStreaming(t *testing.T) {
	setupTraceHandlerTest(t)
	ch := captureTraceCallGemini(t, func(prompt, latest string, mc ModelConfig, tools []map[string]interface{}, images []pendingUpload, onDelta, onReasoning func(string), trace *RequestTrace) (string, []ToolCall, *StreamResult, error) {
		trace.MarkUpstreamRequestStart()
		trace.MarkFirstUpstreamByte()
		trace.MarkFirstParsedFrame()
		if onDelta != nil {
			onDelta("hi")
		}
		trace.MarkFirstSemanticDelta()
		trace.MarkUpstreamFinished()
		return "hi", nil, &StreamResult{UpstreamModel: "3.6 Flash"}, nil
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(respReqBody("gemini-3.6-flash", true)))
	handleResponses(w, r)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	tr := waitTrace(t, ch)
	m := tr.SnapshotMap()
	if !m["stream"].(bool) {
		t.Fatal("expected streaming")
	}
	if m["status"].(int) != 200 {
		t.Fatalf("status=%v", m["status"])
	}
}

func TestTraceHandler_UpstreamError(t *testing.T) {
	setupTraceHandlerTest(t)
	ch := captureTraceCallGemini(t, func(prompt, latest string, mc ModelConfig, tools []map[string]interface{}, images []pendingUpload, onDelta, onReasoning func(string), trace *RequestTrace) (string, []ToolCall, *StreamResult, error) {
		trace.MarkUpstreamRequestStart()
		trace.MarkFirstUpstreamByte()
		trace.MarkUpstreamFinished()
		return "", nil, &StreamResult{UpstreamModel: "3.6 Flash"}, context.DeadlineExceeded
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(chatReqBody("gemini-3.6-flash", false, false)))
	handleChatCompletions(w, r)
	if w.Code == 200 {
		t.Fatalf("expected error status, got 200: %s", w.Body.String())
	}
	tr := waitTrace(t, ch)
	m := tr.SnapshotMap()
	if m["status"].(int) == 200 {
		t.Fatalf("trace status should be error: %v", m["status"])
	}
	if m["upstream_model"].(string) != "3.6 Flash" {
		t.Fatalf("upstream_model=%v", m["upstream_model"])
	}
}

func TestTraceHandler_ToolCallStreaming(t *testing.T) {
	setupTraceHandlerTest(t)
	ch := captureTraceCallGemini(t, func(prompt, latest string, mc ModelConfig, tools []map[string]interface{}, images []pendingUpload, onDelta, onReasoning func(string), trace *RequestTrace) (string, []ToolCall, *StreamResult, error) {
		trace.MarkUpstreamRequestStart()
		trace.MarkFirstUpstreamByte()
		trace.MarkFirstParsedFrame()
		trace.MarkUpstreamFinished()
		return "", []ToolCall{{ID: "call_1", Type: "function", Function: ToolCallFunction{Name: "get_weather", Arguments: `{"city":"Paris"}`}}}, &StreamResult{UpstreamModel: "3.6 Flash"}, nil
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(chatReqBody("gemini-3.6-flash", true, true)))
	handleChatCompletions(w, r)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	tr := waitTrace(t, ch)
	m := tr.SnapshotMap()
	if m["tool_count"].(int) < 1 {
		t.Fatalf("tool_count=%v", m["tool_count"])
	}
	// tool delta should be marked by SSE SendToolCalls
	if m["first_tool_delta_ms"].(int64) < 0 {
		t.Fatalf("first_tool_delta_ms should be set")
	}
}

func TestTraceHandler_ClientDisconnect(t *testing.T) {
	setupTraceHandlerTest(t)
	ch := captureTraceCallGemini(t, func(prompt, latest string, mc ModelConfig, tools []map[string]interface{}, images []pendingUpload, onDelta, onReasoning func(string), trace *RequestTrace) (string, []ToolCall, *StreamResult, error) {
		trace.MarkUpstreamRequestStart()
		trace.MarkFirstUpstreamByte()
		trace.MarkUpstreamFinished()
		return "ok", nil, &StreamResult{}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(chatReqBody("gemini-3.6-flash", false, false))).WithContext(ctx)
	w := httptest.NewRecorder()
	cancel() // disconnect before handler completes? may race; still exercises path
	handleChatCompletions(w, r)
	tr := waitTrace(t, ch)
	m := tr.SnapshotMap()
	// Disconnect detection is best-effort; ensure no panic and trace exists.
	if m["request_id"] == "" {
		t.Fatal("missing request_id")
	}
	_ = m["client_disconnected"]
}

func TestTraceHandler_ConcurrentRequests(t *testing.T) {
	setupTraceHandlerTest(t)
	old := callGemini
	var mu sync.Mutex
	ids := map[string]bool{}
	callGemini = func(prompt, latest string, mc ModelConfig, tools []map[string]interface{}, images []pendingUpload, onDelta, onReasoning func(string), trace *RequestTrace) (string, []ToolCall, *StreamResult, error) {
		mu.Lock()
		ids[trace.RequestID] = true
		mu.Unlock()
		trace.MarkUpstreamRequestStart()
		trace.MarkFirstUpstreamByte()
		trace.MarkFirstParsedFrame()
		trace.MarkFirstSemanticDelta()
		trace.MarkUpstreamFinished()
		return "ok", nil, &StreamResult{}, nil
	}
	t.Cleanup(func() { callGemini = old })
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(chatReqBody("gemini-3.6-flash", false, false)))
			handleChatCompletions(w, r)
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(ids) != 10 {
		t.Fatalf("expected 10 unique request ids, got %d", len(ids))
	}
}
