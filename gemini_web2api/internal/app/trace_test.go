package app

import (
	"strings"
	"testing"
	"time"
)

func TestRequestTraceSnapshotBasic(t *testing.T) {
	tr := newRequestTrace("chat.completions", "gemini-3.6-flash", true)
	tr.SetRequestBodyBytes(123)
	tr.SetMessageCounts(5, 2, 4, 456)
	tr.MarkPromptBuildStart()
	time.Sleep(2 * time.Millisecond)
	tr.MarkPromptBuildEnd("hello world")
	tr.SetModelResolved("gemini-3.6-flash", "gemini-3.6-flash", 1)
	tr.MarkUpstreamRequestStart()
	tr.MarkFirstUpstreamByte()
	tr.MarkFirstParsedFrame()
	tr.MarkFirstSemanticDelta()
	tr.MarkFirstClientSSEByte()
	tr.MarkFirstClientContent()
	tr.MarkUpstreamFinished()
	tr.SetFinish(200, "ok", "")

	m := tr.SnapshotMap()
	if m["request_id"] == "" {
		t.Fatalf("request_id missing")
	}
	if m["endpoint"] != "chat.completions" {
		t.Fatalf("endpoint mismatch: %v", m["endpoint"])
	}
	if m["requested_model"] != "gemini-3.6-flash" {
		t.Fatalf("requested_model mismatch")
	}
	if m["prompt_bytes"].(int) <= 0 {
		t.Fatalf("prompt_bytes should be > 0")
	}
	if m["total_ms"].(int64) < 0 {
		t.Fatalf("total_ms negative")
	}
	if m["upstream_ttfb_ms"].(int64) < 0 {
		t.Fatalf("upstream_ttfb_ms negative")
	}
	if m["first_client_sse_ms"].(int64) < 0 {
		t.Fatalf("first_client_sse_ms negative")
	}
}

func TestRequestTraceNoPromptLeak(t *testing.T) {
	secret := "SUPER_SECRET_PROMPT_SHOULD_NOT_APPEAR"
	tr := newRequestTrace("chat.completions", "m", false)
	tr.MarkPromptBuildStart()
	tr.MarkPromptBuildEnd(secret)
	tr.SetFinish(200, "ok", "")
	// Snapshot/log formatting should not include prompt content.
	m := tr.SnapshotMap()
	for k, v := range m {
		s := strings.ToLower(strings.TrimSpace(k))
		if strings.Contains(s, "prompt_text") || strings.Contains(s, "response_text") || s == "prompt" || s == "content" {
			t.Fatalf("unexpected key %s", k)
		}
		if str, ok := v.(string); ok && strings.Contains(str, secret) {
			t.Fatalf("secret leaked in field %s", k)
		}
	}
}

func TestRequestTraceToolGate(t *testing.T) {
	tr := newRequestTrace("chat.completions", "m", true)
	tr.MarkToolGateActivated()
	tr.MarkToolGateFenceDetected()
	tr.FinishToolGate(42)
	tr.MarkToolGateFlushed()
	m := tr.SnapshotMap()
	if m["tool_gate_activated"] != true {
		t.Fatalf("tool_gate_activated")
	}
	if m["tool_gate_fence_detected"] != true {
		t.Fatalf("tool_gate_fence_detected")
	}
	if m["tool_gate_buffered_bytes"].(int) != 42 {
		t.Fatalf("buffered bytes")
	}
	if m["tool_gate_hold_ms"].(int64) < 0 {
		t.Fatalf("hold ms negative")
	}
}
