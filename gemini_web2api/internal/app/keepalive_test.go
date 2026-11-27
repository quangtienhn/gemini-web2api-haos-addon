package app

import (
	"errors"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func setKeepaliveSec(t *testing.T, sec int) {
	t.Helper()
	rtMu.Lock()
	old := rtVal
	rtVal.StreamKeepaliveSec = sec
	rtMu.Unlock()
	t.Cleanup(func() { rtMu.Lock(); rtVal = old; rtMu.Unlock() })
}

// ç©ºé—²æ»¡ä¸€ä¸ªé—´éš”æ‰ pingï¼›æ­£æ–‡åœ¨æµæ—¶ä¸ pingï¼›Stop ä¹‹åŽä¸å† pingã€‚
func TestStreamKeepalive(t *testing.T) {
	var pings atomic.Int32
	k := startKeepaliveEvery(200*time.Millisecond, func() { pings.Add(1) })
	time.Sleep(100 * time.Millisecond)
	if pings.Load() != 0 {
		t.Fatal("æ²¡ç©ºé—²æ»¡ä¸€ä¸ªé—´éš”å°± ping äº†")
	}
	time.Sleep(250 * time.Millisecond)
	if pings.Load() == 0 {
		t.Fatal("ç©ºé—²æ»¡ä¸€ä¸ªé—´éš”æ²¡ ping")
	}
	send := k.guard(func(string) {})
	before := pings.Load()
	for i := 0; i < 10; i++ { // æ¯ 50ms å†™ä¸€æ¬¡æ­£æ–‡ï¼ŒæŒç»­ 500ms
		send("x")
		time.Sleep(50 * time.Millisecond)
	}
	if pings.Load() != before {
		t.Fatalf("æ­£æ–‡åœ¨æµæ—¶è¿˜ ping äº† %d æ¬¡", pings.Load()-before)
	}
	k.Stop()
	k.Stop() // å¯é‡å¤è°ƒç”¨
	after := pings.Load()
	time.Sleep(300 * time.Millisecond)
	if pings.Load() != after {
		t.Fatal("Stop ä¹‹åŽè¿˜åœ¨ ping")
	}

	var nilK *streamKeepalive // è®¾ç½®ä¸º 0 æ—¶ startKeepalive è¿”å›ž nil
	if nilK.guard(nil) != nil {
		t.Fatal("nil å›žè°ƒåº”åŽŸæ ·è¿”å›ž")
	}
	nilK.Stop()
}

func TestStartKeepaliveOffByDefault(t *testing.T) {
	setKeepaliveSec(t, 0)
	if k := startKeepalive(func() {}); k != nil {
		k.Stop()
		t.Fatal("stream_keepalive_sec=0 æ—¶ä¸è¯¥èµ·ä¿æ´»")
	}
}

// /v1beta æµå¼ï¼šä¸Šæ¸¸æ…¢æ—¶å…ˆå‘ç©ºæ­£æ–‡å—ä¿æ´»ï¼Œæ­£æ–‡ç…§å¸¸ï¼Œæ‹¼èµ·æ¥è·Ÿä¸ä¿æ´»ä¸€æ ·ï¼›
// ä¿æ´»å¼€æµä¹‹åŽå†å¤±è´¥ï¼Œé”™è¯¯èµ°æµå†…æœ€åŽä¸€å—ï¼Œè€Œä¸æ˜¯ HTTP 502ã€‚
func TestStreamGenerateContentKeepalive(t *testing.T) {
	setKeepaliveSec(t, 1)
	saved := runGeminiCall
	t.Cleanup(func() { runGeminiCall = saved })
	var fail error
	runGeminiCall = func(g *geminiCall, onDelta, onReasoning func(string)) (string, []ToolCall, *StreamResult, error) {
		time.Sleep(2200 * time.Millisecond)
		if fail != nil {
			return "", nil, &StreamResult{}, fail
		}
		onDelta("Hello ")
		onDelta("world")
		return "Hello world", nil, &StreamResult{Emitted: "Hello world"}, nil
	}
	body := `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`

	w := httptest.NewRecorder()
	handleGeminiModelItem(w, httptest.NewRequest("POST",
		"/v1beta/models/gemini-3.6-flash:streamGenerateContent?alt=sse", strings.NewReader(body)))
	if w.Code != 200 {
		t.Fatalf("code %d %s", w.Code, w.Body)
	}
	chunks := sseChunks(t, w.Body.String())
	var text strings.Builder
	empty := 0
	for _, c := range chunks {
		for _, p := range chunkParts(c) {
			s := p.(map[string]interface{})["text"].(string)
			if s == "" {
				empty++
			}
			text.WriteString(s)
		}
	}
	if empty < 2 { // 2.2s é‡Œè‡³å°‘ ping ä¸€æ¬¡ï¼ŒåŠ ä¸Šæ”¶å°¾é‚£å—çš„ç©º text
		t.Fatalf("æ²¡çœ‹åˆ°ä¿æ´»å—: %s", w.Body)
	}
	if text.String() != "Hello world" {
		t.Fatalf("æ­£æ–‡æ‹¼å‡ºæ¥ä¸å¯¹: %q", text.String())
	}

	fail = errors.New("upstream timeout")
	w = httptest.NewRecorder()
	handleGeminiModelItem(w, httptest.NewRequest("POST",
		"/v1beta/models/gemini-3.6-flash:streamGenerateContent?alt=sse", strings.NewReader(body)))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"error"`) {
		t.Fatalf("å¼€æµåŽå¤±è´¥åº”èµ°æµå†…é”™è¯¯: %d %s", w.Code, w.Body)
	}
}

// chat çš„ Pingï¼šå…ˆå¼€æµï¼ˆrole chunkï¼‰ï¼Œå†å‘ SSE æ³¨é‡Šè¡Œã€‚
func TestSSEWriterPing(t *testing.T) {
	w := httptest.NewRecorder()
	s := newSSEWriter(w, "id", 0, "m", nil)
	s.Ping()
	s.Ping()
	b := w.Body.String()
	if !s.Started() || w.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatal("Ping åº”å…ˆå¼€æµ")
	}
	if strings.Count(b, `"role":"assistant"`) != 1 || strings.Count(b, ": ping\n\n") != 2 {
		t.Fatalf("got %q", b)
	}
}
