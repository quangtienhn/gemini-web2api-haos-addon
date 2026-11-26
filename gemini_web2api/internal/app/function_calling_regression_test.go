package app

import (
	"strings"
	"testing"
)

func TestParseToolCallsRegression(t *testing.T) {
	text := "Let me search.\n```tool_call\n{\"name\":\"web_search\",\"arguments\":{\"query\":\"hello\"}}\n```\nDone."
	clean, calls := parseToolCalls(text)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Name != "web_search" {
		t.Fatalf("wrong name: %s", calls[0].Function.Name)
	}
	if !strings.Contains(clean, "Let me search.") {
		t.Fatalf("clean text lost: %q", clean)
	}
	// malformed block is skipped, not fatal
	bad := "```tool_call\n{not json}\n```"
	c2, c := parseToolCalls(bad)
	if len(c) != 0 {
		t.Fatalf("malformed should be skipped, got %d", len(c))
	}
	if c2 == "" {
		// acceptable: empty clean
	}
	// multiple blocks
	multi := "```tool_call\n{\"name\":\"a\",\"arguments\":{}}\n```\n```tool_call\n{\"name\":\"b\",\"arguments\":{}}\n```"
	_, cc := parseToolCalls(multi)
	if len(cc) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(cc))
	}
}
