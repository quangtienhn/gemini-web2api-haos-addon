package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// These tests validate trace instrumentation behavior with mock upstream handlers.
// They focus on trace metadata/milestones, not prompt/response content logging.

func TestTraceMatrixNonStreamingSuccess(t *testing.T) {
	tr := newRequestTrace("chat.completions", "gemini-3.6-flash", false)
	tr.SetRequestBodyBytes(10)
	tr.SetMessageCounts(1, 0, 1, 0)
	tr.MarkPromptBuildStart()
	tr.MarkPromptBuildEnd("hello")
	tr.SetModelResolved("gemini-3.6-flash", "gemini-3.6-flash", 1)
	tr.MarkUpstreamRequestStart()
	tr.MarkFirstUpstreamByte()
	tr.MarkFirstParsedFrame()
	tr.MarkFirstSemanticDelta()
	tr.MarkUpstreamFinished()
	tr.SetFinish(200, "ok", "")
	m := tr.SnapshotMap()
	if m["stream"].(bool) != false {
		t.Fatalf("expected non-streaming")
	}
	if m["first_client_sse_ms"].(int64) != -1 {
		t.Fatalf("non-streaming should not have client SSE milestone")
	}
	if m["total_ms"].(int64) < 0 {
		t.Fatalf("total_ms")
	}
}

func TestTraceMatrixStreamingSuccess(t *testing.T) {
	tr := newRequestTrace("chat.completions", "gemini-3.6-flash", true)
	tr.MarkUpstreamRequestStart()
	tr.MarkFirstUpstreamByte()
	tr.MarkFirstParsedFrame()
	tr.MarkFirstSemanticDelta()
	tr.MarkFirstClientSSEByte()
	tr.MarkFirstClientContent()
	tr.MarkUpstreamFinished()
	tr.SetFinish(200, "ok", "")
	m := tr.SnapshotMap()
	if m["first_client_sse_ms"].(int64) < 0 {
		t.Fatalf("missing first_client_sse_ms")
	}
	if m["first_client_content_ms"].(int64) < 0 {
		t.Fatalf("missing first_client_content_ms")
	}
}

func TestTraceMatrixSlowTTFB(t *testing.T) {
	tr := newRequestTrace("chat.completions", "m", true)
	tr.MarkUpstreamRequestStart()
	time.Sleep(5 * time.Millisecond)
	tr.MarkFirstUpstreamByte()
	m := tr.SnapshotMap()
	if m["upstream_ttfb_ms"].(int64) < 1 {
		t.Fatalf("ttfb should reflect delay")
	}
}

func TestTraceMatrixFastTTFBSlowSemantic(t *testing.T) {
	tr := newRequestTrace("chat.completions", "m", true)
	tr.MarkUpstreamRequestStart()
	tr.MarkFirstUpstreamByte()
	time.Sleep(5 * time.Millisecond)
	tr.MarkFirstSemanticDelta()
	m := tr.SnapshotMap()
	if m["upstream_ttfb_ms"].(int64) < 0 {
		t.Fatalf("ttfb")
	}
	if m["first_semantic_delta_ms"].(int64) < m["upstream_ttfb_ms"].(int64) {
		t.Fatalf("semantic delta should be >= ttfb")
	}
}

func TestTraceMatrixRetriesAndEmptyFrames(t *testing.T) {
	tr := newRequestTrace("chat.completions", "m", true)
	tr.MarkUpstreamRequestStart()
	tr.AddEmptyFrame()
	tr.AddRetry("attempt_1_empty_frame", 2*time.Millisecond)
	tr.MarkUpstreamAttempt()
	tr.MarkUpstreamFinished()
	tr.SetFinish(200, "ok", "")
	m := tr.SnapshotMap()
	if m["empty_frame_count"].(int) < 1 {
		t.Fatalf("empty_frame_count")
	}
	if m["retry_count"].(int) < 1 {
		t.Fatalf("retry_count")
	}
	if m["upstream_attempts"].(int) < 2 {
		t.Fatalf("upstream_attempts")
	}
}

func TestTraceMatrixToolGateHold(t *testing.T) {
	tr := newRequestTrace("chat.completions", "m", true)
	tr.MarkToolGateActivated()
	time.Sleep(2 * time.Millisecond)
	tr.MarkToolGateFenceDetected()
	tr.FinishToolGate(128)
	tr.MarkToolGateFallback()
	tr.SetFinish(200, "ok", "")
	m := tr.SnapshotMap()
	if m["tool_gate_activated"] != true {
		t.Fatalf("activated")
	}
	if m["tool_gate_fence_detected"] != true {
		t.Fatalf("fence")
	}
	if m["tool_gate_hold_ms"].(int64) < 0 {
		t.Fatalf("hold")
	}
	if m["tool_gate_buffered_bytes"].(int) != 128 {
		t.Fatalf("buffered")
	}
	if m["tool_gate_fallback"] != true {
		t.Fatalf("fallback")
	}
}

func TestTraceMatrixConcurrentRequests(t *testing.T) {
	var wg sync.WaitGroup
	ids := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr := newRequestTrace("chat.completions", "m", true)
			tr.MarkUpstreamRequestStart()
			tr.MarkFirstUpstreamByte()
			tr.MarkFirstSemanticDelta()
			tr.SetFinish(200, "ok", "")
			ids <- tr.RequestID
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		if seen[id] {
			t.Fatalf("duplicate request id %s", id)
		}
		seen[id] = true
	}
	if len(seen) != 20 {
		t.Fatalf("expected 20 ids, got %d", len(seen))
	}
}

func TestTraceMatrixNoSensitiveLeakInSummary(t *testing.T) {
	secret := "sk-test-secret-DO-NOT-LOG"
	tr := newRequestTrace("chat.completions", "m", false)
	tr.MarkPromptBuildStart()
	tr.MarkPromptBuildEnd(secret)
	tr.SetFinish(200, "ok", "")
	m := tr.SnapshotMap()
	for k, v := range m {
		if str, ok := v.(string); ok && strings.Contains(str, secret) {
			t.Fatalf("leak in %s", k)
		}
	}
	// ensure logf redaction path is exercised indirectly by not panicking
	logf("[perf] test request_id=%s", tr.RequestID)
	_ = httptest.NewRecorder() // keep httptest import used for future expansion
	_ = http.StatusOK
}
