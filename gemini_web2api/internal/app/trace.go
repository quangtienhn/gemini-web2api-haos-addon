package app

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// AttemptTiming captures per-upstream-attempt timing metadata.
type AttemptTiming struct {
	Index        int    `json:"index"`
	StartMs      int64  `json:"start_ms"`
	TTFBMs       int64  `json:"ttfb_ms"`
	DurationMs   int64  `json:"duration_ms"`
	Status       int    `json:"status"`
	ErrCategory  string `json:"err_category,omitempty"`
	RetryReason  string `json:"retry_reason,omitempty"`
	RetryDelayMs int64  `json:"retry_delay_ms,omitempty"`
}

// RequestTrace captures P0 performance instrumentation for a single request.
// It contains metadata/timings only - never prompt, response, credentials, or PII.
// All elapsed durations use time.Time values captured from time.Now() (Go time.Time
// includes a monotonic reading; Sub uses it when available).
type RequestTrace struct {
	mu sync.Mutex

	RequestID      string
	Endpoint       string
	RequestedModel string
	ResolvedModel  string
	UpstreamModel  string
	ModelMode      int
	Stream         bool

	RequestReceivedAt   time.Time
	RequestBodyBytes    int
	MessageCount        int
	ToolCount           int
	HistoryMessageCount int
	ToolSchemaBytes     int

	PromptBuildStart time.Time
	PromptBuildEnd   time.Time
	PromptBytes      int
	PromptChars      int
	EstimatedTokens  int

	UpstreamRequestStart     time.Time
	FirstUpstreamByteAt      time.Time
	FirstParsedFrameAt       time.Time
	FirstSemanticDeltaAt     time.Time
	FirstToolDeltaAt         time.Time // client tool-call emission (compat)
	FirstUpstreamToolDeltaAt time.Time
	FirstClientToolDeltaAt   time.Time
	UpstreamFinishedAt       time.Time

	FirstClientSSEByteAt time.Time
	FirstClientContentAt time.Time

	ToolGateActivated     bool
	ToolGateStart         time.Time
	ToolGateEnd           time.Time
	ToolGateHoldMs        int64
	ToolGateBufferedBytes int
	ToolGateFenceDetected bool
	ToolGateFlushed       bool
	ToolGateFallback      bool

	RetryCount              int
	RetryReasons            []string
	RetryDelayMs            int64
	UpstreamAttempts        int
	AttemptTimings          []AttemptTiming
	currentAttemptStart     time.Time
	currentAttemptFirstByte time.Time
	EmptyFrameCount         int
	QueueingSignals         int
	ThinkingSignals         int
	QueueingSupported       bool
	ThinkingSupported       bool

	ClientDisconnected bool
	StreamFinishReason string
	ErrorCategory      string
	Status             int

	requestDoneAt time.Time
}

func newRequestTrace(endpoint, requestedModel string, stream bool) *RequestTrace {
	return &RequestTrace{
		RequestID:         "req_" + randHex(16),
		Endpoint:          endpoint,
		RequestedModel:    requestedModel,
		Stream:            stream,
		RequestReceivedAt: time.Now(),
	}
}

func (t *RequestTrace) SetRequestBodyBytes(n int) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.RequestBodyBytes = n
	t.mu.Unlock()
}
func (t *RequestTrace) SetMessageCounts(messages, tools, history, toolSchemaBytes int) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.MessageCount, t.ToolCount, t.HistoryMessageCount, t.ToolSchemaBytes = messages, tools, history, toolSchemaBytes
	t.mu.Unlock()
}
func (t *RequestTrace) MarkPromptBuildStart() {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.PromptBuildStart.IsZero() {
		t.PromptBuildStart = time.Now()
	}
	t.mu.Unlock()
}
func (t *RequestTrace) MarkPromptBuildEnd(prompt string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.PromptBuildEnd.IsZero() {
		t.PromptBuildEnd = time.Now()
	}
	t.PromptBytes = len(prompt)
	t.PromptChars = len([]rune(prompt))
	// Best-effort estimate only; countTokens may be chars/4 fallback.
	t.EstimatedTokens = countTokens(stripDataURLs(prompt))
	t.mu.Unlock()
}
func (t *RequestTrace) SetModelResolved(requested, resolved string, mode int) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.RequestedModel, t.ResolvedModel, t.ModelMode = requested, resolved, mode
	t.mu.Unlock()
}
func (t *RequestTrace) SetUpstreamModel(model string) {
	if t == nil || model == "" {
		return
	}
	t.mu.Lock()
	if t.UpstreamModel == "" {
		t.UpstreamModel = model
	}
	t.mu.Unlock()
}
func (t *RequestTrace) MarkUpstreamRequestStart() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	if t.UpstreamRequestStart.IsZero() {
		t.UpstreamRequestStart = now
	}
	if t.currentAttemptStart.IsZero() {
		t.currentAttemptStart = now
		t.UpstreamAttempts++
	}
}
func (t *RequestTrace) MarkUpstreamAttempt() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	t.currentAttemptStart = now
	t.currentAttemptFirstByte = time.Time{}
	t.UpstreamAttempts++
}
func (t *RequestTrace) MarkFirstUpstreamByte() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	if t.FirstUpstreamByteAt.IsZero() {
		t.FirstUpstreamByteAt = now
	}
	if !t.currentAttemptStart.IsZero() && t.currentAttemptFirstByte.IsZero() {
		t.currentAttemptFirstByte = now
	}
}
func (t *RequestTrace) MarkFirstParsedFrame() {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.FirstParsedFrameAt.IsZero() {
		t.FirstParsedFrameAt = time.Now()
	}
	t.mu.Unlock()
}
func (t *RequestTrace) MarkFirstSemanticDelta() {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.FirstSemanticDeltaAt.IsZero() {
		t.FirstSemanticDeltaAt = time.Now()
	}
	t.mu.Unlock()
}
func (t *RequestTrace) MarkFirstToolDelta() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	if t.FirstToolDeltaAt.IsZero() {
		t.FirstToolDeltaAt = now
	}
	if t.FirstClientToolDeltaAt.IsZero() {
		t.FirstClientToolDeltaAt = now
	}
}
func (t *RequestTrace) MarkFirstUpstreamToolDelta() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.FirstUpstreamToolDeltaAt.IsZero() {
		t.FirstUpstreamToolDeltaAt = time.Now()
	}
}
func (t *RequestTrace) MarkFirstClientToolDelta() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	if t.FirstClientToolDeltaAt.IsZero() {
		t.FirstClientToolDeltaAt = now
	}
	if t.FirstToolDeltaAt.IsZero() {
		t.FirstToolDeltaAt = now
	}
}
func (t *RequestTrace) MarkUpstreamFinished() {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.UpstreamFinishedAt.IsZero() {
		t.UpstreamFinishedAt = time.Now()
	}
	t.mu.Unlock()
}
func (t *RequestTrace) MarkFirstClientSSEByte() {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.FirstClientSSEByteAt.IsZero() {
		t.FirstClientSSEByteAt = time.Now()
	}
	t.mu.Unlock()
}
func (t *RequestTrace) MarkFirstClientContent() {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.FirstClientContentAt.IsZero() {
		t.FirstClientContentAt = time.Now()
	}
	t.mu.Unlock()
}
func (t *RequestTrace) MarkToolGateActivated() {
	if t == nil {
		return
	}
	t.mu.Lock()
	if !t.ToolGateActivated {
		t.ToolGateActivated = true
		t.ToolGateStart = time.Now()
	}
	t.mu.Unlock()
}
func (t *RequestTrace) MarkToolGateFenceDetected() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.ToolGateFenceDetected = true
	t.mu.Unlock()
}
func (t *RequestTrace) MarkToolGateFlushed() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.ToolGateFlushed = true
	t.mu.Unlock()
}
func (t *RequestTrace) MarkToolGateFallback() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.ToolGateFallback = true
	t.mu.Unlock()
}
func (t *RequestTrace) FinishToolGate(bufferedBytes int) {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.ToolGateActivated && t.ToolGateEnd.IsZero() {
		t.ToolGateEnd = time.Now()
		t.ToolGateHoldMs = t.ToolGateEnd.Sub(t.ToolGateStart).Milliseconds()
	}
	if bufferedBytes > t.ToolGateBufferedBytes {
		t.ToolGateBufferedBytes = bufferedBytes
	}
	t.mu.Unlock()
}
func (t *RequestTrace) StartAttempt() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	t.currentAttemptStart = now
	t.currentAttemptFirstByte = time.Time{}
	if t.UpstreamRequestStart.IsZero() {
		t.UpstreamRequestStart = now
	}
	t.UpstreamAttempts++
}
func (t *RequestTrace) MarkAttemptFirstByte() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	if t.currentAttemptFirstByte.IsZero() {
		t.currentAttemptFirstByte = now
	}
	if t.FirstUpstreamByteAt.IsZero() {
		t.FirstUpstreamByteAt = now
	}
}
func (t *RequestTrace) FinishAttempt(status int, errCategory string, retryReason string, retryDelay time.Duration) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.currentAttemptStart.IsZero() {
		return
	}
	end := time.Now()
	at := AttemptTiming{Index: len(t.AttemptTimings) + 1, Status: status, ErrCategory: errCategory, RetryReason: retryReason, RetryDelayMs: retryDelay.Milliseconds()}
	at.StartMs = t.currentAttemptStart.Sub(t.RequestReceivedAt).Milliseconds()
	if !t.currentAttemptFirstByte.IsZero() {
		at.TTFBMs = t.currentAttemptFirstByte.Sub(t.currentAttemptStart).Milliseconds()
	} else {
		at.TTFBMs = -1
	}
	at.DurationMs = end.Sub(t.currentAttemptStart).Milliseconds()
	t.AttemptTimings = append(t.AttemptTimings, at)
	if retryReason != "" {
		t.RetryReasons = append(t.RetryReasons, retryReason)
	}
	t.currentAttemptStart = time.Time{}
	t.currentAttemptFirstByte = time.Time{}
}
func (t *RequestTrace) AddRetry(reason string, delay time.Duration) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.RetryCount++
	if reason != "" {
		t.RetryReasons = append(t.RetryReasons, reason)
	}
	if delay > 0 {
		t.RetryDelayMs += delay.Milliseconds()
	}
}
func (t *RequestTrace) AddEmptyFrame() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.EmptyFrameCount++
	t.mu.Unlock()
}
func (t *RequestTrace) AddQueueingSignal() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.QueueingSignals++
	t.mu.Unlock()
}
func (t *RequestTrace) AddThinkingSignal() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.ThinkingSignals++
	t.mu.Unlock()
}
func (t *RequestTrace) SetClientDisconnected() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.ClientDisconnected = true
	t.mu.Unlock()
}
func (t *RequestTrace) SetFinish(status int, reason, errCat string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.Status = status
	if reason != "" {
		t.StreamFinishReason = reason
	}
	if errCat != "" {
		t.ErrorCategory = errCat
	}
	if t.requestDoneAt.IsZero() {
		t.requestDoneAt = time.Now()
	}
	t.mu.Unlock()
}

func msSince(a, b time.Time) int64 {
	if a.IsZero() || b.IsZero() {
		return -1
	}
	ms := b.Sub(a).Milliseconds()
	if ms < 0 {
		return 0
	}
	return ms
}

func (t *RequestTrace) SnapshotMap() map[string]interface{} {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ToolGateActivated && t.ToolGateEnd.IsZero() {
		t.ToolGateEnd = time.Now()
		t.ToolGateHoldMs = t.ToolGateEnd.Sub(t.ToolGateStart).Milliseconds()
	}
	done := t.requestDoneAt
	if done.IsZero() {
		done = time.Now()
	}
	m := map[string]interface{}{
		"request_id":                   t.RequestID,
		"endpoint":                     t.Endpoint,
		"requested_model":              t.RequestedModel,
		"resolved_model":               t.ResolvedModel,
		"upstream_model":               t.UpstreamModel,
		"model_mode":                   t.ModelMode,
		"stream":                       t.Stream,
		"status":                       t.Status,
		"error_category":               t.ErrorCategory,
		"finish_reason":                t.StreamFinishReason,
		"client_disconnected":          t.ClientDisconnected,
		"request_body_bytes":           t.RequestBodyBytes,
		"message_count":                t.MessageCount,
		"history_message_count":        t.HistoryMessageCount,
		"tool_count":                   t.ToolCount,
		"tool_schema_bytes":            t.ToolSchemaBytes,
		"prompt_bytes":                 t.PromptBytes,
		"prompt_chars":                 t.PromptChars,
		"estimated_tokens":             t.EstimatedTokens,
		"prompt_build_ms":              msSince(t.PromptBuildStart, t.PromptBuildEnd),
		"upstream_ttfb_ms":             msSince(t.UpstreamRequestStart, t.FirstUpstreamByteAt),
		"first_parsed_frame_ms":        msSince(t.UpstreamRequestStart, t.FirstParsedFrameAt),
		"first_semantic_delta_ms":      msSince(t.UpstreamRequestStart, t.FirstSemanticDeltaAt),
		"first_tool_delta_ms":          msSince(t.UpstreamRequestStart, t.FirstToolDeltaAt),
		"first_upstream_tool_delta_ms": msSince(t.UpstreamRequestStart, t.FirstUpstreamToolDeltaAt),
		"first_client_tool_delta_ms":   msSince(t.RequestReceivedAt, t.FirstClientToolDeltaAt),
		"first_client_sse_ms":          msSince(t.RequestReceivedAt, t.FirstClientSSEByteAt),
		"first_client_content_ms":      msSince(t.RequestReceivedAt, t.FirstClientContentAt),
		"upstream_duration_ms":         msSince(t.UpstreamRequestStart, t.UpstreamFinishedAt),
		"total_ms":                     msSince(t.RequestReceivedAt, done),
		"retry_count":                  t.RetryCount,
		"retry_delay_ms":               t.RetryDelayMs,
		"upstream_attempts":            t.UpstreamAttempts,
		"empty_frame_count":            t.EmptyFrameCount,
		"queueing_signals": func() interface{} {
			if !t.QueueingSupported {
				return "UNSUPPORTED"
			}
			return t.QueueingSignals
		}(),
		"thinking_signals": func() interface{} {
			if !t.ThinkingSupported {
				return "UNSUPPORTED"
			}
			return t.ThinkingSignals
		}(),
		"queueing_supported":       t.QueueingSupported,
		"thinking_supported":       t.ThinkingSupported,
		"tool_gate_activated":      t.ToolGateActivated,
		"tool_gate_hold_ms":        t.ToolGateHoldMs,
		"tool_gate_buffered_bytes": t.ToolGateBufferedBytes,
		"tool_gate_fence_detected": t.ToolGateFenceDetected,
		"tool_gate_flushed":        t.ToolGateFlushed,
		"tool_gate_fallback":       t.ToolGateFallback,
	}
	if len(t.RetryReasons) > 0 {
		m["retry_reasons"] = strings.Join(t.RetryReasons, "|")
	}
	if len(t.AttemptTimings) > 0 {
		ats := make([]map[string]interface{}, 0, len(t.AttemptTimings))
		for _, a := range t.AttemptTimings {
			ats = append(ats, map[string]interface{}{"index": a.Index, "start_ms": a.StartMs, "ttfb_ms": a.TTFBMs, "duration_ms": a.DurationMs, "status": a.Status, "err_category": a.ErrCategory, "retry_reason": a.RetryReason, "retry_delay_ms": a.RetryDelayMs})
		}
		m["attempt_timings"] = ats
	}
	return m
}

func (t *RequestTrace) LogSummary() {
	if t == nil {
		return
	}
	m := t.SnapshotMap()
	// Stable key ordering for readability.
	keys := []string{
		"request_id", "endpoint", "status", "error_category", "finish_reason", "stream", "client_disconnected",
		"requested_model", "resolved_model", "upstream_model", "model_mode",
		"request_body_bytes", "message_count", "history_message_count", "tool_count", "tool_schema_bytes", "prompt_bytes", "estimated_tokens",
		"prompt_build_ms", "upstream_ttfb_ms", "first_parsed_frame_ms", "first_semantic_delta_ms", "first_tool_delta_ms",
		"first_client_sse_ms", "first_client_content_ms", "upstream_duration_ms", "total_ms",
		"upstream_attempts", "retry_count", "retry_delay_ms", "empty_frame_count", "queueing_signals", "thinking_signals",
		"tool_gate_activated", "tool_gate_hold_ms", "tool_gate_buffered_bytes", "tool_gate_fence_detected", "tool_gate_flushed", "tool_gate_fallback",
		"retry_reasons",
	}
	var b strings.Builder
	b.WriteString("[perf] ")
	for i, k := range keys {
		v, ok := m[k]
		if !ok || v == nil {
			continue
		}
		if s, ok := v.(string); ok && s == "" {
			continue
		}
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%s=%v", k, v)
	}
	logf("%s", b.String())
}
