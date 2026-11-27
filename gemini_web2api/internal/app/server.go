package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const Version = "4.35.2-dev"

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	body, _ := json.Marshal(data)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	w.Write(body)
}

func handleModels(w http.ResponseWriter, r *http.Request) {
	var data []map[string]interface{}
	for name, c := range availableModels() {
		data = append(data, map[string]interface{}{
			"id":          name,
			"object":      "model",
			"created":     1700000000,
			"owned_by":    "google",
			"description": c.Desc,
		})
	}
	writeJSON(w, 200, map[string]interface{}{"object": "list", "data": data})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write([]byte(`{"status":"ok"}`))
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	var modelNames []string
	for n := range availableModels() {
		modelNames = append(modelNames, n)
	}
	writeJSON(w, 200, map[string]interface{}{
		"status":  "ok",
		"version": Version,
		"models":  modelNames,
	})
}

func handleOptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.WriteHeader(204)
}

// buildUsage Ã§â€Â¨ tiktoken Ã§Â®â€” token Ã¦â€¢Â°Ã¯Â¼Å’Ã¨Â·Å¸ requests Ã¨Â¡Â¨Ã©â€¡Å’Ã¨Â®Â°Ã§Å¡â€žÃ¥ÂÂ£Ã¥Â¾â€žÃ¤Â¿ÂÃ¦Å’ÂÃ¤Â¸â‚¬Ã¨â€¡Â´Ã£â‚¬â€š
// responsesAPI=true Ã¦â€”Â¶Ã§â€Â¨ /v1/responses Ã§Å¡â€žÃ¥Â­â€”Ã¦Â®ÂµÃ¥ÂÂÃ¯Â¼Ë†input_tokens/output_tokensÃ¯Â¼â€°Ã£â‚¬â€š
func buildUsage(prompt, text string, responsesAPI bool) map[string]int {
	return buildUsageWithReasoning(prompt, text, "", responsesAPI)
}

// buildUsageWithReasoning Ã¥Å“Â¨ usage Ã©â€¡Å’Ã¥Ââ€¢Ã¥Ë†â€”Ã¦â‚¬ÂÃ¨â‚¬Æ’Ã©â€œÂ¾Ã§Å¡â€ž token Ã¦â€¢Â°Ã£â‚¬â€š
//
// Ã¦â‚¬ÂÃ¨â‚¬Æ’Ã©â€œÂ¾**Ã¤Â¸ÂÃ¨Â®Â¡Ã¥â€¦Â¥** completion_tokensÃ¯Â¼Å¡Ã¥Â®Æ’Ã¦ËœÂ¯Ã¦Â¨Â¡Ã¥Å¾â€¹Ã¨â€¡ÂªÃ¥Â·Â±Ã§Å¡â€žÃ¦Å½Â¨Ã§Ââ€ Ã¨Â¿â€¡Ã§Â¨â€¹Ã¯Â¼Å’Ã¥Â®Â¢Ã¦Ë†Â·Ã§Â«Â¯Ã©Â»ËœÃ¨Â®Â¤Ã¤Â¸ÂÃ¥Â±â€¢Ã§Â¤ÂºÃ¯Â¼Å’
// Ã§Â®â€”Ã¨Â¿â€ºÃ¥Å½Â»Ã§Â­â€°Ã¤ÂºÅ½Ã¨Â®Â©Ã§â€Â¨Ã¦Ë†Â·Ã¤Â¸ÂºÃ§Å“â€¹Ã¤Â¸ÂÃ¨Â§ÂÃ§Å¡â€žÃ¨Â¾â€œÃ¥â€¡ÂºÃ¤Â¹Â°Ã¥Ââ€¢Ã¯Â¼Ë†Ã¤Â¸â€¹Ã¦Â¸Â¸ newapi Ã¦ËœÂ¯Ã¦Å’â€° completion_tokens Ã¨Â®Â¡Ã¨Â´Â¹Ã§Å¡â€žÃ¯Â¼â€°Ã£â‚¬â€š
// Ã¥Ââ€¢Ã¥Ë†â€”Ã¥Å“Â¨ completion_tokens_details.reasoning_tokens Ã©â€¡Å’Ã¯Â¼Å’Ã¨Â·Å¸ OpenAI Ã§Å¡â€žÃ¥ÂÅ¡Ã¦Â³â€¢Ã¤Â¸â‚¬Ã¨â€¡Â´Ã¯Â¼Å’
// Ã¦Æ’Â³Ã¨Â®Â¡Ã¨Â´Â¹Ã§Å¡â€žÃ¤ÂºÂºÃ¨â€¡ÂªÃ¥Â·Â±Ã¥Å Â Ã£â‚¬â€š
func buildUsageWithReasoning(prompt, text, reasoning string, responsesAPI bool) map[string]int {
	// Ã¥Âªâ€™Ã¤Â½â€œÃ¤ÂºÂ§Ã§â€°Â©Ã¦ËœÂ¯ base64 data URLÃ¯Â¼Å’Ã¤Â¸Å Ã§â„¢Â¾Ã¤Â¸â€¡Ã¥Â­â€”Ã§Â¬Â¦Ã£â‚¬â€šÃ¤Â¸ÂÃ¨Â®Â¡Ã¨Â¿â€º output tokenÃ¯Â¼Å¡Ã©â€šÂ£Ã¦ËœÂ¯Ã¤ÂºÅ’Ã¨Â¿â€ºÃ¥Ë†Â¶Ã¥â€ â€¦Ã¥Â®Â¹Ã¯Â¼Å’
	// Ã¦Å’â€° token Ã¨Â®Â¡Ã¨Â´Â¹Ã§Â­â€°Ã¤ÂºÅ½Ã¨Â®Â©Ã§â€Â¨Ã¦Ë†Â·Ã¤Â¸ÂºÃ§Å“â€¹Ã¤Â¸ÂÃ¨Â§ÂÃ§Å¡â€žÃ¥Â­â€”Ã¨Å â€šÃ¤Â¹Â°Ã¥Ââ€¢Ã¯Â¼Å’Ã¤Â¸â€¹Ã¦Â¸Â¸ newapi Ã¦Å’â€° output token Ã¦â€Â¶Ã©â€™Â±Ã£â‚¬â€š
	in, out := countTokens(prompt), countTokens(stripDataURLs(text))
	if responsesAPI {
		return map[string]int{
			"input_tokens":  in,
			"output_tokens": out,
			"total_tokens":  in + out,
		}
	}
	u := map[string]int{
		"prompt_tokens":     in,
		"completion_tokens": out,
		"total_tokens":      in + out,
	}
	if reasoning != "" {
		u["reasoning_tokens"] = countTokens(reasoning)
	}
	return u
}

// rejectUnsupported Ã¦Â£â‚¬Ã¦Å¸Â¥Ã¥Â®Â¢Ã¦Ë†Â·Ã§Â«Â¯Ã¤Â¼Â Ã¤Âºâ€ Ã¤Â½â€ Ã¦Ë†â€˜Ã¤Â»Â¬Ã¥â€¦â€˜Ã§Å½Â°Ã¤Â¸ÂÃ¤Âºâ€ Ã§Å¡â€žÃ¥Â­â€”Ã¦Â®ÂµÃ£â‚¬â€š
//
// Ã¥ÂÂªÃ¦â€¹Â¦"Ã©Ââ„¢Ã©Â»ËœÃ¥Â¿Â½Ã§â€¢Â¥Ã¤Â¼Å¡Ã¨Â®Â©Ã¥Â®Â¢Ã¦Ë†Â·Ã§Â«Â¯Ã¦â€¹Â¿Ã¥Ë†Â°Ã©â€â„¢Ã¨Â¯Â¯Ã§Â»â€œÃ¦Å¾Å“"Ã§Å¡â€žÃ¯Â¼Å¡n>1 Ã¥Â°â€˜Ã§Â»â„¢Ã¥â‚¬â„¢Ã©â‚¬â€°Ã£â‚¬ÂÃ¥â€ºÂ¾Ã§â€°â€¡Ã¨Â¾â€œÃ¥â€¦Â¥Ã¨Â¢Â«Ã¤Â¸Â¢Ã¦Å½â€°Ã¤Â¼Å¡
// Ã¨Â®Â©Ã¦Â¨Â¡Ã¥Å¾â€¹Ã§Â­â€Ã©ÂÅ¾Ã¦â€°â‚¬Ã©â€”Â®Ã£â‚¬â€šÃ©â€¡â€¡Ã¦Â Â·Ã§Â±Â»Ã¥Ââ€šÃ¦â€¢Â°Ã¯Â¼Ë†temperature/top_p/max_tokens/...Ã¯Â¼â€°Ã¤Â¸Å Ã¦Â¸Â¸Ã¦Â Â¹Ã¦Å“Â¬
// Ã¦Â²Â¡Ã¦Å“â€°Ã¥Â¯Â¹Ã¥Âºâ€Ã¦â€”â€¹Ã©â€™Â®Ã¯Â¼Å’Ã¦â€Â¶Ã¤Â¸â€¹Ã¥Â¿Â½Ã§â€¢Â¥Ã¥ÂÂ³Ã¥ÂÂ¯Ã¯Â¼Å’Ã¦Å Â¥Ã©â€â„¢Ã¥ÂÂÃ¨â‚¬Å’Ã¤Â¼Å¡Ã¦Å’Â¡Ã¤Â½ÂÃ¦Â­Â£Ã¥Â¸Â¸Ã¥Â®Â¢Ã¦Ë†Â·Ã§Â«Â¯Ã£â‚¬â€š
func rejectUnsupported(req map[string]interface{}, messages []map[string]interface{}) error {
	if n, ok := req["n"].(float64); ok && n > 1 {
		return fmt.Errorf("n=%d not supported: upstream returns a single candidate", int(n))
	}
	for _, m := range messages {
		parts, ok := m["content"].([]interface{})
		if !ok {
			continue
		}
		for _, c := range parts {
			cm, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			switch getStr(cm, "type") {
			case "image_url", "input_image":
				// Ã¦Å“â€° cookie Ã¥Â°Â±Ã¨Æ’Â½Ã¦â€Â¶Ã¯Â¼Å¡Ã¥â€ºÂ¾Ã¤Â¼Å¡Ã¨Â¢Â«Ã¤Â¸Å Ã¤Â¼Â Ã¦Ë†ÂÃ©â„¢â€žÃ¤Â»Â¶Ã¥â€ ÂÃ¥Â¼â€¢Ã§â€Â¨Ã£â‚¬â€šÃ¥Å’Â¿Ã¥ÂÂÃ¤Â¸ÂÃ¨Â¡Å’ Ã¢â‚¬â€Ã¢â‚¬â€ Ã¤Â¼Â Ã¥Â¾â€”Ã¤Â¸Å Ã¥Å½Â»Ã¯Â¼Å’
				// Ã¤Â½â€ Ã¤Â¸â‚¬Ã¥Â¼â€¢Ã§â€Â¨Ã¥Â°Â±Ã¨Â¢Â«Ã¦Å“ÂÃ¥Å Â¡Ã§Â«Â¯Ã¦â€¹â€™Ã¯Â¼Å’Ã¦â€Â¶Ã¤Â¸â€¹Ã¥ÂÂªÃ¤Â¼Å¡Ã¨Â®Â©Ã¥Â®Â¢Ã¦Ë†Â·Ã§Â«Â¯Ã¦â€¹Â¿Ã¥Ë†Â°Ã¤Â¸â‚¬Ã¤Â¸ÂªÃ§Å“â€¹Ã¤Â¸ÂÃ¦â€¡â€šÃ§Å¡â€žÃ¥Â¤Â±Ã¨Â´Â¥Ã£â‚¬â€š
				if !hasCookie() {
					return fmt.Errorf("image input needs a Google account cookie: anonymous " +
						"uploads succeed but referencing them in a conversation is rejected " +
						"upstream. Add a cookie in the admin panel (Cookie pool)")
				}
			case "video_url", "input_video":
				// Ã¨Â§â€ Ã©Â¢â€˜Ã¨Â·Å¸Ã¥â€ºÂ¾Ã§â€°â€¡Ã¥ÂÅ’Ã§Ââ€ Ã¯Â¼Å¡Ã§â„¢Â»Ã¥Â½â€¢Ã¦â‚¬ÂÃ¦â€°ÂÃ¨Æ’Â½Ã¥Â¼â€¢Ã§â€Â¨Ã¯Â¼Å’Ã¥Å’Â¿Ã¥ÂÂÃ¤Â¸â‚¬Ã¥Â¼â€¢Ã§â€Â¨Ã¥Â°Â± 1100Ã£â‚¬â€š
				if !hasCookie() {
					return fmt.Errorf("video input needs a Google account cookie: anonymous " +
						"uploads succeed but referencing them in a conversation is rejected " +
						"upstream. Add a cookie in the admin panel (Cookie pool)")
				}
			case "input_audio":
				return fmt.Errorf("audio input not supported")
			}
		}
	}
	return nil
}

// callGemini is the Gemini call seam; tests may replace it.
var callGemini = func(prompt, latest string, mc ModelConfig, tools []map[string]interface{},
	images []pendingUpload, onDelta, onReasoning func(string), trace *RequestTrace) (string, []ToolCall, *StreamResult, error) {
	res, err := streamGenerateWithFiles(prompt, latest, mc, images, onDelta, onReasoning, trace)
	if err != nil {
		// res Ã©ÂÅ¾ nilÃ¯Â¼Å¡Ã¥Â¤Â±Ã¨Â´Â¥Ã¦â€”Â¶Ã¥Â®Æ’Ã¥ÂÂªÃ¥Â¸Â¦Ã¥Â½â€™Ã¥Â±Å¾Ã¯Â¼Ë†Ã¥â€œÂªÃ¤Â¸ÂªÃ¥ÂÂ· / Ã¥â€œÂªÃ¤Â¸ÂªÃ¥â€¡ÂºÃ¥ÂÂ£Ã¯Â¼â€°Ã¯Â¼Å’Ã§Â»â„¢ recordRequest Ã§â€Â¨
		return "", nil, res, err
	}
	text := extractResponseText(res.Raw)
	// Ã§â€Â»Ã¥Â¸Æ’Ã¯Â¼Å¡HTML Ã¦â€“â€¡Ã¦Â¡Â£Ã¥â€ â€¦Ã¨Ââ€Ã¥Å“Â¨Ã¥â€œÂÃ¥Âºâ€Ã©â€¡Å’Ã¯Â¼Ë†Ã¤Â¸ÂÃ¥Æ’ÂÃ¥â€ºÂ¾/Ã¤Â¹ÂÃ¨Â¦ÂÃ¤Â¸â€¹Ã¨Â½Â½Ã¯Â¼â€°Ã¯Â¼Å’Ã¤Â»Å½ immersive Ã§Â»â€œÃ¦Å¾â€žÃ©â€¡Å’Ã¦Å Â Ã¥â€¡ÂºÃ¦ÂÂ¥Ã£â‚¬â€š
	// Ã¦Â â€¡Ã¥â€¡â€ Ã¦â€“â€¡Ã¦Å“Â¬Ã¦ÂÂÃ¥Ââ€“Ã¥ÂÂªÃ¦â€¹Â¿Ã¥Ë†Â° preambleÃ¯Â¼Ë†"I will generateÃ¢â‚¬Â¦"Ã¯Â¼â€°Ã¯Â¼Å’Ã¦â€“â€¡Ã¦Â¡Â£Ã¥Å“Â¨ inner[4][0][30]Ã¢â‚¬Â¦Ã¯Â¼Å’
	// Ã§â€Â¨ extractCanvasDoc Ã¥Ââ€¢Ã§â€¹Â¬Ã¥Ââ€“Ã£â‚¬â€š
	if mc.Tool == toolCanvas {
		doc := extractCanvasDoc(res.Raw)
		if doc == "" {
			return "", nil, res, fmt.Errorf("canvas generation failed: no HTML document in response (raw %d bytes)", len(res.Raw))
		}
		if text != "" && !strings.Contains(doc, text) {
			return text + "\n\n" + doc, nil, res, nil // preamble + Ã¦â€“â€¡Ã¦Â¡Â£
		}
		return doc, nil, res, nil
	}
	// Ã¥Âªâ€™Ã¤Â½â€œÃ¦Â¨Â¡Ã¥Å¾â€¹Ã¯Â¼Ë†Ã§â€Å¸Ã¥â€ºÂ¾/Ã©Å¸Â³Ã¤Â¹ÂÃ¯Â¼â€°Ã¯Â¼Å¡Ã§â€Å¸Ã¦Ë†Â 200 Ã¤Âºâ€ Ã¤Â½â€ Ã¤ÂºÂ§Ã§â€°Â©Ã¥Â­â€”Ã¨Å â€šÃ¦Â²Â¡Ã¥Ââ€“Ã¥â€ºÅ¾Ã¦ÂÂ¥Ã¯Â¼Å’Ã§â€ºÂ´Ã¦Å½Â¥Ã¦Å Â¥Ã©â€â„¢Ã¨â‚¬Å’Ã¤Â¸ÂÃ¦ËœÂ¯Ã¨Â¿â€Ã¥â€ºÅ¾Ã¤Â¸â‚¬Ã¤Â¸Âª
	// Ã¥ÂÂªÃ¦Å“â€°Ã¦â€“â€¡Ã¥Â­â€”Ã¦Â²Â¡Ã¦Å“â€°Ã¥â€ºÂ¾Ã§Å¡â€žÃ¥ÂÅ Ã¦Ë†ÂÃ¥â€œÂ Ã¢â‚¬â€Ã¢â‚¬â€ Ã¥Â®Â¢Ã¦Ë†Â·Ã§Â«Â¯Ã¨Â¦ÂÃ§Å¡â€žÃ¥Â°Â±Ã¦ËœÂ¯Ã©â€šÂ£Ã¥Â¼Â Ã¥â€ºÂ¾/Ã©â€šÂ£Ã¦Â®ÂµÃ¤Â¹ÂÃ£â‚¬â€š
	if mc.Tool == toolImage || mc.Tool == toolMusic || mc.Tool == toolVideo {
		if len(res.Artifacts) == 0 {
			cause := res.MediaErr
			if cause == nil {
				cause = errors.New("Ã¥Âªâ€™Ã¤Â½â€œÃ¤ÂºÂ§Ã§â€°Â©Ã¥Ââ€“Ã¥â€ºÅ¾Ã¥Â¤Â±Ã¨Â´Â¥")
			}
			return "", nil, res, fmt.Errorf("media generation succeeded but artifact retrieval failed: %w", cause)
		}
		// Ã¤ÂºÂ§Ã§â€°Â©Ã¤Â»Â¥ base64 data URL Ã¨Â¿Â½Ã¥Å Â Ã¥Ë†Â°Ã¦Â­Â£Ã¦â€“â€¡Ã¯Â¼Ë†Ã¥ÂÂ¯Ã¨Æ’Â½Ã¦Â²Â¡Ã¦Â­Â£Ã¦â€“â€¡Ã¯Â¼Å’Ã¥ÂÂªÃ¦Å“â€°Ã¥â€ºÂ¾/Ã¤Â¹Â/Ã¨Â§â€ Ã©Â¢â€˜Ã¯Â¼â€°Ã£â‚¬â€š
		text = appendArtifactMarkdown(text, res.Artifacts)
		return text, nil, res, nil
	}
	// Ã¦â€“â€¡Ã¦Å“Â¬Ã¦Â¨Â¡Ã¥Å¾â€¹Ã¨Â¢Â«Ã¤Â¸Å Ã¦Â¸Â¸Ã¥Ë†â€¡Ã¥Å½Â»Ã§â€Å¸Ã¥â€ºÂ¾ / Ã©Å¸Â³Ã¤Â¹ÂÃ¯Â¼Ë†#105Ã¯Â¼â€°Ã¯Â¼Å¡Ã¤ÂºÂ§Ã§â€°Â©Ã§â€¦Â§Ã¦Â Â·Ã©â„¢â€žÃ¥Å“Â¨Ã¦Â­Â£Ã¦â€“â€¡Ã¥ÂÅ½Ã©ÂÂ¢Ã£â‚¬â€š
	if len(res.Artifacts) > 0 {
		var toolCalls []ToolCall
		if len(tools) > 0 {
			text, toolCalls = parseToolCalls(text)
		}
		return appendArtifactMarkdown(text, res.Artifacts), toolCalls, res, nil
	}
	if text == "" {
		// Ã¤Â¸Å Ã¦Â¸Â¸Ã¦â€¹â€™Ã§Â»ÂÃ¦â€”Â¶Ã¥ÂÂªÃ¥â€ºÅ¾Ã¤Â¸â‚¬Ã¤Â¸ÂªÃ§Â»â€œÃ¦ÂÅ¸Ã¥Â¸Â§Ã£â‚¬ÂÃ¦Â²Â¡Ã¦Å“â€°Ã¥â€ â€¦Ã¥Â®Â¹Ã¥Â¸Â§Ã¯Â¼Ë†Ã¥Â®Å¾Ã¦Âµâ€¹Ã¨Â¢Â«Ã¦â€¹â€™Ã¦â€”Â¶ raw Ã¤Â»â€¦ 216
		// Ã¥Â­â€”Ã¨Å â€šÃ¯Â¼â€°Ã£â‚¬â€šÃ¨Â¿â„¢Ã§Â§ÂÃ¦Æ’â€¦Ã¥â€ ÂµÃ¥Â¿â€¦Ã©Â¡Â»Ã¦Å Â¥Ã©â€â„¢Ã¯Â¼Å¡Ã¤Â»Â¥Ã¥â€°ÂÃ¤Â¼Å¡Ã¥Â½â€œÃ¦Ë†ÂÃ§Â©ÂºÃ¥â€ºÅ¾Ã¥Â¤ÂÃ¨Â¿â€Ã¥â€ºÅ¾ 200 + content:nullÃ¯Â¼Å’
		// Ã¥Â®Â¢Ã¦Ë†Â·Ã§Â«Â¯Ã§Å“â€¹Ã¤Â¸ÂÃ¥â€¡ÂºÃ¨Â¯Â·Ã¦Â±â€šÃ¥â€¦Â¶Ã¥Â®Å¾Ã¥Â¤Â±Ã¨Â´Â¥Ã¤Âºâ€ Ã£â‚¬â€š
		// Ã¦Â³Â¨Ã¦â€žÂÃ¤Â¸ÂÃ¨Æ’Â½Ã§â€Â¨ BardErrorInfo Ã¥Ë†Â¤Ã©â€â„¢ Ã¢â‚¬â€Ã¢â‚¬â€ Ã¦Â­Â£Ã¥Â¸Â¸Ã¥â€œÂÃ¥Âºâ€Ã§Å¡â€žÃ§Â»â€œÃ¦ÂÅ¸Ã¥Â¸Â§Ã©â€¡Å’Ã¤Â¹Å¸Ã¥Â¸Â¦Ã¨Â¿â„¢Ã¤Â¸ÂªÃ§Â ÂÃ£â‚¬â€š
		return "", nil, res, fmt.Errorf("upstream returned no content frame (raw %d bytes)", len(res.Raw))
	}
	var toolCalls []ToolCall
	if len(tools) > 0 {
		text, toolCalls = parseToolCalls(text)
	}
	// Ã¥â€ºÅ¾Ã¥Â¤ÂÃ¥ÂÂªÃ¦Å“â€°Ã¤Â¸â‚¬Ã¤Â¸ÂªÃ¨Â§Â£Ã¦Å¾ÂÃ¤Â¸ÂÃ¤Âºâ€ Ã§Å¡â€ž ```tool_call``` Ã¥â€ºÂ´Ã¦Â ÂÃ¯Â¼Ë†JSON Ã¥â€ â„¢Ã¥ÂÂ / Ã¦Â²Â¡Ã¦Å“â€°Ã¥Â·Â¥Ã¥â€¦Â·Ã¥ÂÂÃ¯Â¼â€°Ã¦â€”Â¶Ã¯Â¼Å’
	// parseToolCalls Ã¥â€°Â¥Ã¦Å½â€°Ã¥â€ºÂ´Ã¦Â ÂÃ¥ÂÅ½Ã¦Â­Â£Ã¦â€“â€¡Ã¤Â¸ÂºÃ§Â©ÂºÃ£â‚¬ÂÃ¤Â¹Å¸Ã¦Â²Â¡Ã¦Å“â€°Ã¨Â°Æ’Ã§â€Â¨Ã£â‚¬â€šÃ¨Â·Å¸ callGeminiConv Ã¤Â¸â‚¬Ã¦Â Â·Ã¦Å Â¥Ã©â€â„¢Ã¯Â¼Å’
	// Ã¤Â¸ÂÃ¨Æ’Â½Ã¥â€ºÅ¾ 200 + content:null Ã¤Â¸â€Ã¤Â¸ÂÃ¥Â¸Â¦ tool_calls Ã¢â‚¬â€Ã¢â‚¬â€ Ã¥Â®Â¢Ã¦Ë†Â·Ã§Â«Â¯Ã§Å“â€¹Ã¤Â¸ÂÃ¥â€¡ÂºÃ¨Â¯Â·Ã¦Â±â€šÃ¥â€¦Â¶Ã¥Â®Å¾Ã¥Â¤Â±Ã¨Â´Â¥Ã¤Âºâ€ Ã£â‚¬â€š
	if text == "" && len(toolCalls) == 0 {
		return "", nil, res, fmt.Errorf("upstream reply had only an unparseable tool_call block (raw %d bytes)", len(res.Raw))
	}
	return text, toolCalls, res, nil
}

// recordRequest writes one row of metadata to the requests table.
// Privacy: the prompt/response strings themselves are never persisted Ã¢â‚¬â€
// only their length, model name, latency, status, and proxy info.
func recordRequest(endpoint, model, prompt, response string, res *StreamResult, status int, errStr string, stream bool) {
	// Ã¥Âªâ€™Ã¤Â½â€œÃ¤ÂºÂ§Ã§â€°Â©Ã¦ËœÂ¯Ã¨Â¶â€¦Ã©â€¢Â¿ base64Ã¯Â¼Å’Ã¥â€°Â¥Ã¦Å½â€°Ã¥â€ ÂÃ§Â®â€” token/Ã©â€¢Â¿Ã¥ÂºÂ¦Ã¯Â¼Å¡Ã¦â€”Â¢Ã¤Â¸ÂÃ¨Â®Â©Ã¥Â®Æ’Ã¦Â±Â¡Ã¦Å¸â€œÃ§Â»Å¸Ã¨Â®Â¡Ã¯Â¼Å’Ã¤Â¹Å¸Ã¥â€¦ÂÃ¥Â¾â€”Ã¥Å“Â¨Ã¨Â¯Â·Ã¦Â±â€šÃ§ÂºÂ¿Ã§Â¨â€¹Ã©â€¡Å’
	// Ã¥Â¯Â¹Ã¤Â¸Å Ã§â„¢Â¾Ã¤Â¸â€¡Ã¥Â­â€”Ã§Â¬Â¦Ã¨Â·â€˜ tiktoken Ã§â„¢Â½Ã§â„¢Â½Ã¦â€¹â€“Ã¦â€¦Â¢Ã£â‚¬â€š
	response = stripDataURLs(response)
	r := &RequestRow{
		TS:            time.Now().Unix(),
		Model:         model,
		Status:        status,
		Error:         errStr,
		PromptChars:   len(prompt),
		ResponseChars: len(response),
		PromptTokens:  countTokens(prompt),
		OutputTokens:  countTokens(response),
		Endpoint:      endpoint,
	}
	if stream {
		r.Stream = 1
	}
	if res != nil {
		r.UpstreamModel = res.UpstreamModel
		r.TotalMs = res.TotalMs
		r.TTFBMs = &res.TTFBMs
		if res.ProxyID > 0 {
			r.ProxyID = &res.ProxyID
		}
		r.ProxyName = res.ProxyName
		if res.AccountID > 0 {
			r.AccountID = &res.AccountID
		}
		r.AccountLabel = res.AccountLabel
	}
	go insertRequest(r)
}

func handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	trace := newRequestTrace("chat.completions", "", false)
	defer trace.LogSummary()
	defer func() {
		if r.Context().Err() != nil {
			trace.SetClientDisconnected()
			if trace.Status == 0 {
				trace.SetFinish(499, "client_disconnected", "client disconnected")
			}
		}
	}()

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, cfg.MaxChatBodyBytes))
	if err != nil {
		trace.SetFinish(400, "invalid_body", "bad_request")
		writeJSON(w, 400, map[string]interface{}{"error": map[string]string{"message": err.Error()}})
		return
	}
	trace.SetRequestBodyBytes(len(body))
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		trace.SetFinish(400, "invalid_json", "bad_request")
		writeJSON(w, 400, map[string]interface{}{"error": map[string]string{"message": "invalid JSON"}})
		return
	}

	modelInput := getStr(req, "model")
	if modelInput == "" {
		modelInput = rtCfg().DefaultModel
	}
	modelName, modelCfg, err := resolveModel(modelInput)
	if err != nil {
		trace.SetFinish(400, "model_resolve", "bad_request")
		writeJSON(w, 400, map[string]interface{}{"error": map[string]string{"message": err.Error()}})
		return
	}
	trace.SetModelResolved(modelInput, modelName, modelCfg.Mode)

	messagesRaw, _ := req["messages"].([]interface{})
	var messages []map[string]interface{}
	for _, m := range messagesRaw {
		if mm, ok := m.(map[string]interface{}); ok {
			messages = append(messages, mm)
		}
	}

	toolsRaw, _ := req["tools"].([]interface{})
	var tools []map[string]interface{}
	for _, t := range toolsRaw {
		if tm, ok := t.(map[string]interface{}); ok {
			tools = append(tools, tm)
		}
	}

	toolSchemaBytes := 0
	if len(tools) > 0 {
		if b, e := json.Marshal(tools); e == nil {
			toolSchemaBytes = len(b)
		}
	}
	trace.SetMessageCounts(len(messages), len(tools), len(messages), toolSchemaBytes)
	if err := rejectUnsupported(req, messages); err != nil {
		trace.SetFinish(400, "unsupported", "bad_request")
		writeJSON(w, 400, map[string]interface{}{"error": map[string]string{
			"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	// Ã¥â€ºÂ¾Ã§â€°â€¡Ã¥â€¦Ë†Ã¨Â§Â£Ã¥â€¡ÂºÃ¦ÂÂ¥Ã¥Â¸Â¦Ã§Ââ‚¬Ã¯Â¼Å’Ã§Å“Å¸Ã¦Â­Â£Ã¤Â¸Å Ã¤Â¼Â Ã¨Â¦ÂÃ§Â­â€°Ã¦Å’â€˜Ã¥Â®Å’Ã¨Â´Â¦Ã¥ÂÂ·Ã¥â€™Å’Ã¥â€¡ÂºÃ¥ÂÂ£Ã¯Â¼Ë†Ã¨Â§Â streamGenerateÃ¯Â¼â€°Ã£â‚¬â€š
	images, err := collectImages(messages, "")
	if err != nil {
		trace.SetFinish(400, "collect_images", "bad_request")
		writeJSON(w, 400, map[string]interface{}{"error": map[string]string{
			"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	trace.MarkPromptBuildStart()
	prompt, latest := messagesToPrompt(messages, tools, req["tool_choice"])
	trace.MarkPromptBuildEnd(prompt)
	if strings.TrimSpace(prompt) == "" && len(images) == 0 {
		trace.SetFinish(400, "empty_prompt", "bad_request")
		writeJSON(w, 400, map[string]interface{}{"error": map[string]string{"message": "empty prompt"}})
		return
	}

	stream, _ := req["stream"].(bool)
	trace.Stream = stream
	includeUsage := false
	if so, ok := req["stream_options"].(map[string]interface{}); ok {
		includeUsage, _ = so["include_usage"].(bool)
	}

	cid := "chatcmpl-" + randHex(12)
	created := time.Now().Unix()

	// Ã¥Â¸Â¦ tools Ã¦â€”Â¶Ã¦Â­Â£Ã¦â€“â€¡Ã¨Â¿â€¡Ã¤Â¸â‚¬Ã©Ââ€œÃ¥â€ºÂ´Ã¦Â ÂÃ©â€”Â¸Ã©â€”Â¨Ã¯Â¼Å¡```tool_call``` Ã¥Ââ€”Ã¨Â¦ÂÃ¥Â®Å’Ã¦â€¢Â´Ã¦â€“â€¡Ã¦Å“Â¬Ã¦â€°ÂÃ¨Æ’Â½Ã¨Â§Â£Ã¦Å¾ÂÃ¯Â¼Å’
	// Ã§â€ºÂ´Ã¦Å½Â¥Ã¨Â½Â¬Ã¥Ââ€˜Ã¤Â¼Å¡Ã¦Å Å Ã¥â€ºÂ´Ã¦Â ÂÃ¥Å½Å¸Ã¦â€“â€¡Ã¦Å½Â¨Ã§Â»â„¢Ã¥Â®Â¢Ã¦Ë†Â·Ã§Â«Â¯Ã¯Â¼Å’Ã¦â€°â‚¬Ã¤Â»Â¥Ã¥ÂÂªÃ¦â€Â¾Ã¨Â¡Å’Ã§Â¡Â®Ã¥Â®Å¡Ã¤Â¸ÂÃ¥Å“Â¨Ã¥â€ºÂ´Ã¦Â ÂÃ©â€¡Å’Ã§Å¡â€žÃ©Æ’Â¨Ã¥Ë†â€ Ã£â‚¬â€š
	// Ã¦â‚¬ÂÃ¨â‚¬Æ’Ã©â€œÂ¾Ã¨Â·Å¸Ã¥â€ºÂ´Ã¦Â ÂÃ¦â€”Â Ã¥â€¦Â³Ã¯Â¼Å’Ã¤Â¸Â¤Ã§Â§ÂÃ¦Æ’â€¦Ã¥â€ ÂµÃ©Æ’Â½Ã§â€ºÂ´Ã¦Å½Â¥Ã¦ÂµÂÃ£â‚¬â€š
	var sse *sseWriter
	var gate *toolFenceGate
	var onDelta, onReasoning func(string)
	var ka *streamKeepalive
	if stream {
		sse = newSSEWriter(w, cid, created, modelName, trace)
		onReasoning = sse.SendReasoning
		if len(tools) == 0 {
			onDelta = sse.SendContent
		} else {
			gate = newToolFenceGate(sse.SendContent, trace)
			onDelta = gate.Push
		}
		ka = startKeepalive(sse.Ping)
		onDelta, onReasoning = ka.guard(onDelta), ka.guard(onReasoning)
	}

	var text string
	var toolCalls []ToolCall
	var res *StreamResult
	if rtCfg().MultiTurn && len(images) == 0 && modelCfg.Tool == 0 {
		// Ã¥Â¤Å¡Ã¨Â½Â®Ã¯Â¼Å¡Ã¦Å’â€°Ã¥Å½â€ Ã¥ÂÂ²Ã¥â€°ÂÃ§Â¼â‚¬Ã¨Â¯â€ Ã¥Ë†Â«Ã§Â»Â­Ã¦Å½Â¥Ã¯Â¼Å’Ã¥â€˜Â½Ã¤Â¸Â­Ã¥Â°Â±Ã¥ÂÂªÃ¥Ââ€˜Ã¦â€“Â°Ã¦Â¶Ë†Ã¦ÂÂ¯Ã£â‚¬ÂÃ¥Å½â€ Ã¥ÂÂ²Ã§â€¢â„¢Ã¦Å“ÂÃ¥Å Â¡Ã§Â«Â¯Ã£â‚¬â€šÃ¥Â¸Â¦ tools Ã¤Â¹Å¸Ã¨ÂµÂ°Ã¨Â¿â„¢Ã¦ÂÂ¡Ã£â‚¬â€š
		text, toolCalls, res, err = callGeminiConv(messages, modelCfg, tools, req["tool_choice"], onDelta, onReasoning, trace, conversationOptions{requestScope: conversationRequestScope(r, getStr(req, "prompt_cache_key"), getStr(req, "user"))})
	} else {
		text, toolCalls, res, err = callGemini(prompt, latest, modelCfg, tools, images, onDelta, onReasoning, trace)
	}
	ka.Stop() // Ã¤Â¹â€¹Ã¥ÂÅ½Ã§Å¡â€žÃ¦â€Â¶Ã¥Â°Â¾Ã¥â€ºÅ¾Ã¥Ë†Â°Ã¥Ââ€¢ goroutine Ã¥â€ â„¢Ã¯Â¼â€ºStarted() Ã¤Â¹Å¸Ã¨Â¦ÂÃ¥Å“Â¨Ã¤Â¿ÂÃ¦Â´Â»Ã¥ÂÅ“Ã¤Â¸â€¹Ã¥ÂÅ½Ã¥â€ ÂÃ§Å“â€¹
	if err != nil {
		if res != nil && res.UpstreamModel != "" {
			trace.SetUpstreamModel(res.UpstreamModel)
		}
		mde, downgraded := err.(*ModelDowngradedError)
		status := 502
		if downgraded {
			status = 429
		}
		trace.SetFinish(status, "error", err.Error())
		recordRequest("chat.completions", modelName, prompt, "", res, status, err.Error(), stream)
		if sse != nil && sse.Started() {
			sse.Fail(err) // Ã¥Â·Â²Ã§Â»ÂÃ¥Â¼â‚¬Ã¦ÂµÂÃ¯Â¼Å’HTTP Ã§Å Â¶Ã¦â‚¬ÂÃ§Â ÂÃ¦â€Â¹Ã¤Â¸ÂÃ¤Âºâ€ Ã¤Âºâ€
			return
		}
		if downgraded {
			writeModelDowngraded(w, mde)
			return
		}
		if ptl, ok := err.(*PromptTooLongError); ok {
			// Ã¦ËœÅ½Ã§Â¡Â®Ã¦Å Â¥ 400 Ã¨â‚¬Å’Ã¤Â¸ÂÃ¦ËœÂ¯Ã¥Ââ€˜Ã¥â€¡ÂºÃ¥Å½Â»Ã¨Â®Â©Ã¤Â¸Å Ã¦Â¸Â¸Ã¦Å Å Ã§â€Â¨Ã¦Ë†Â·Ã§Å¡â€žÃ©â€”Â®Ã©Â¢ËœÃ¦Ë†ÂªÃ¦Å½â€° Ã¢â‚¬â€Ã¢â‚¬â€ Ã©â€šÂ£Ã¦Â Â·Ã¥Â®Â¢Ã¦Ë†Â·Ã§Â«Â¯Ã¦â€¹Â¿Ã¥Ë†Â°Ã§Å¡â€žÃ¦ËœÂ¯
			// Ã¤Â¸â‚¬Ã¤Â¸ÂªÃ§Â­â€Ã©ÂÅ¾Ã¦â€°â‚¬Ã©â€”Â®Ã§Å¡â€ž 200Ã¯Â¼Å’Ã¦Â Â¹Ã¦Å“Â¬Ã§Å“â€¹Ã¤Â¸ÂÃ¥â€¡ÂºÃ¨Â¯Â·Ã¦Â±â€šÃ¥â€¦Â¶Ã¥Â®Å¾Ã¦Â²Â¡Ã©â‚¬ÂÃ¥Ë†Â°Ã£â‚¬â€š
			writeJSON(w, 400, map[string]interface{}{"error": map[string]string{
				"message": ptl.Error(), "type": "invalid_request_error",
				"code": "context_length_exceeded"}})
			return
		}
		if rle, ok := err.(*RateLimitError); ok {
			writeJSON(w, 429, map[string]interface{}{"error": map[string]string{
				"message": rle.Error(),
				"type":    "rate_limit_exceeded",
				"code":    "ip_slot_full",
			}})
			return
		}
		writeJSON(w, 502, map[string]interface{}{"error": map[string]string{"message": "upstream error: " + err.Error()}})
		return
	}

	msg := map[string]interface{}{"role": "assistant"}
	if text != "" {
		msg["content"] = text
	} else {
		msg["content"] = nil
	}
	// Ã¦â‚¬ÂÃ¨â‚¬Æ’Ã©â€œÂ¾Ã¨ÂµÂ° reasoning_content Ã¢â‚¬â€Ã¢â‚¬â€ DeepSeek-R1 Ã¥Â¸Â¦Ã¨ÂµÂ·Ã¦ÂÂ¥Ã§Å¡â€žÃ¤Âºâ€¹Ã¥Â®Å¾Ã¦Â â€¡Ã¥â€¡â€ Ã¯Â¼Å’newapi Ã¥â€™Å’
	// Ã¤Â¸Â»Ã¦ÂµÂÃ¥Â®Â¢Ã¦Ë†Â·Ã§Â«Â¯Ã©Æ’Â½Ã¨Â®Â¤Ã¯Â¼Å’Ã¤Â¼Å¡Ã¦Â¸Â²Ã¦Å¸â€œÃ¦Ë†ÂÃ¥ÂÂ¯Ã¦Å ËœÃ¥ÂÂ Ã§Å¡â€žÃ£â‚¬Å’Ã¦â‚¬ÂÃ¨â‚¬Æ’Ã¨Â¿â€¡Ã§Â¨â€¹Ã£â‚¬ÂÃ£â‚¬â€šÃ¥ÂÂªÃ¦Å“â€° 3.1 Pro Ã¦Å“â€°Ã¯Â¼Å’Ã¥â€¦Â¶Ã¤Â½â„¢Ã¤Â¸ÂºÃ§Â©ÂºÃ£â‚¬â€š
	if res != nil && res.Reasoning != "" {
		msg["reasoning_content"] = res.Reasoning
	}
	finish := "stop"
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
		finish = "tool_calls"
	}

	if res != nil && res.UpstreamModel != "" {
		trace.SetUpstreamModel(res.UpstreamModel)
	}
	trace.SetFinish(200, "ok", "")
	recordRequest("chat.completions", modelName, prompt, text, res, 200, "", stream)
	if stream {
		var usage map[string]int
		if includeUsage {
			usage = usageOf(prompt, text, res)
		}
		// Ã§Å“Å¸Ã¦ÂµÂÃ¥Â¼ÂÃ¥Â·Â²Ã¥Ââ€˜Ã¥â€¡ÂºÃ§Å¡â€žÃ¤Â¸ÂÃ©â€¡ÂÃ¥Ââ€˜Ã¯Â¼Å’Ã¥ÂÂªÃ¨Â¡Â¥Ã¥Â°Â¾Ã¥Â·Â´Ã¯Â¼â€ºÃ¦Å“â€° tools Ã¦â€”Â¶Ã¦Â²Â¡Ã¨ÂµÂ°Ã§Å“Å¸Ã¦ÂµÂÃ¥Â¼ÂÃ¯Â¼Å’Ã¨Â¿â„¢Ã©â€¡Å’Ã¥Ââ€˜Ã¥â€¦Â¨Ã©â€¡ÂÃ£â‚¬â€š
		// Ã¦â‚¬ÂÃ¨â‚¬Æ’Ã©â€œÂ¾Ã¥ÂÅ’Ã§Ââ€ Ã¯Â¼Å’Ã¤Â¸â€Ã¨Â¦ÂÃ¥Å“Â¨Ã¦Â­Â£Ã¦â€“â€¡Ã¤Â¹â€¹Ã¥â€°ÂÃ¨Â¡Â¥Ã¢â‚¬â€Ã¢â‚¬â€Ã¤Â¿ÂÃ¦Å’ÂÃ£â‚¬Å’Ã¥â€¦Ë†Ã¦â‚¬ÂÃ¨â‚¬Æ’Ã¥ÂÅ½Ã¥â€ºÅ¾Ã§Â­â€Ã£â‚¬ÂÃ§Å¡â€žÃ©Â¡ÂºÃ¥ÂºÂÃ£â‚¬â€š
		if res != nil && res.Reasoning != "" {
			if rest := remainingOf(res.Reasoning, res.EmittedReasoning); rest != "" {
				sse.SendReasoning(rest)
			}
		}
		// Ã¨Â¡Â¥Ã¥Â°Â¾Ã¥Â·Â´Ã¨Â¦ÂÃ¨Â·Å¸**Ã¥Â®Å¾Ã©â„¢â€¦Ã¥Ââ€˜Ã§Â»â„¢Ã¥Â®Â¢Ã¦Ë†Â·Ã§Â«Â¯Ã§Å¡â€žÃ¥â€ â€¦Ã¥Â®Â¹**Ã¦Â¯â€Ã£â‚¬â€šÃ¨ÂµÂ°Ã¤Âºâ€ Ã©â€”Â¸Ã©â€”Â¨Ã¦â€”Â¶ res.Emitted Ã¥ÂÂ«Ã¥â€ºÂ´Ã¦Â Â
		// Ã¥Å½Å¸Ã¦â€“â€¡Ã¯Â¼Å’Ã¦â€¹Â¿Ã¥Â®Æ’Ã¦Â¯â€Ã¥â€°ÂÃ§Â¼â‚¬Ã¤Â¼Å¡Ã¥Â¯Â¹Ã¤Â¸ÂÃ¤Â¸Å Ã¯Â¼Å’Ã¥Â°Â¾Ã¥Â·Â´Ã¤Â¼Å¡Ã¦â€¢Â´Ã¦Â®ÂµÃ¤Â¸Â¢Ã¦Å½â€°Ã£â‚¬â€š
		if gate != nil {
			trace.FinishToolGate(gate.BufferedBytes())
		}
		if rest := remainingOf(text, sentText(res, gate)); rest != "" {
			if gate != nil {
				trace.MarkToolGateFallback()
			}
			sse.SendContent(rest)
		}
		if len(toolCalls) > 0 {
			sse.SendToolCalls(toolCalls)
		}
		trace.StreamFinishReason = finish
		sse.Finish(finish, usage)
		return
	}

	trace.StreamFinishReason = finish
	writeJSON(w, 200, map[string]interface{}{
		"id":      cid,
		"object":  "chat.completion",
		"created": created,
		"model":   modelName,
		"choices": []map[string]interface{}{{
			"index":         0,
			"message":       msg,
			"finish_reason": finish,
		}},
		"usage": usageOf(prompt, text, res),
	})
}

// usageOf Ã¦ËœÂ¯ buildUsageWithReasoning Ã§Å¡â€žÃ¤Â¾Â¿Ã¦ÂÂ·Ã¥Å’â€¦Ã¨Â£â€¦Ã¯Â¼Å’res Ã¥ÂÂ¯Ã¨Æ’Â½Ã¤Â¸Âº nilÃ£â‚¬â€š
func usageOf(prompt, text string, res *StreamResult) map[string]int {
	r := ""
	if res != nil {
		r = res.Reasoning
	}
	return buildUsageWithReasoning(prompt, text, r, false)
}

// remainingText Ã¨Â¿â€Ã¥â€ºÅ¾Ã¦Å“â‚¬Ã§Â»Ë†Ã¦â€“â€¡Ã¦Å“Â¬Ã©â€¡Å’Ã¨Â¿ËœÃ¦Â²Â¡Ã©â‚¬Å¡Ã¨Â¿â€¡ onDelta Ã¥Ââ€˜Ã¥â€¡ÂºÃ¥Å½Â»Ã§Å¡â€žÃ©Æ’Â¨Ã¥Ë†â€ Ã£â‚¬â€š
// Ã§Å“Å¸Ã¦ÂµÂÃ¥Â¼ÂÃ¤Â¸â€¹Ã©â‚¬Å¡Ã¥Â¸Â¸Ã¥ÂÂªÃ¥â€°Â©Ã¦Å“Â«Ã¥Â°Â¾Ã¤Â¸â‚¬Ã§â€šÂ¹Ã¦Ë†â€“Ã¤Â¸ÂºÃ§Â©ÂºÃ¯Â¼â€ºÃ¦Â²Â¡Ã¨ÂµÂ°Ã§Å“Å¸Ã¦ÂµÂÃ¥Â¼ÂÃ¦â€”Â¶ Emitted Ã¤Â¸ÂºÃ§Â©ÂºÃ¯Â¼Å’Ã¨Â¿â€Ã¥â€ºÅ¾Ã¥â€¦Â¨Ã¦â€“â€¡Ã£â‚¬â€š
func remainingText(text string, res *StreamResult) string {
	if res == nil {
		return text
	}
	return remainingOf(text, res.Emitted)
}

// sentText Ã¨Â¿â€Ã¥â€ºÅ¾Ã¦Å“Â¬Ã¦Â¬Â¡Ã¥Â®Å¾Ã©â„¢â€¦Ã¥Ââ€˜Ã§Â»â„¢Ã¥Â®Â¢Ã¦Ë†Â·Ã§Â«Â¯Ã§Å¡â€žÃ¦Â­Â£Ã¦â€“â€¡Ã£â‚¬â€š
// Ã¦Â²Â¡Ã¨ÂµÂ°Ã¥â€ºÂ´Ã¦Â ÂÃ©â€”Â¸Ã©â€”Â¨Ã¦â€”Â¶Ã¥Â°Â±Ã¦ËœÂ¯ deltaTracker Ã¥Ââ€˜Ã¥â€¡ÂºÃ§Å¡â€žÃ©â€šÂ£Ã¤Âºâ€ºÃ¯Â¼â€ºÃ¨ÂµÂ°Ã¤Âºâ€ Ã©â€”Â¸Ã©â€”Â¨Ã¦â€”Â¶Ã¤Â»Â¥Ã©â€”Â¸Ã©â€”Â¨Ã¤Â¸ÂºÃ¥â€¡â€  Ã¢â‚¬â€Ã¢â‚¬â€
// Ã©â€”Â¸Ã©â€”Â¨Ã¦â€°Â£Ã¦Å½â€°Ã¤Âºâ€ Ã¥â€ºÂ´Ã¦Â ÂÃ¯Â¼Å’Ã¨Â·Å¸ res.Emitted Ã¤Â¸ÂÃ¦ËœÂ¯Ã¥ÂÅ’Ã¤Â¸â‚¬Ã¤Â»Â½Ã¦â€“â€¡Ã¦Å“Â¬Ã£â‚¬â€š
func sentText(res *StreamResult, gate *toolFenceGate) string {
	if gate != nil {
		return gate.Sent()
	}
	if res == nil {
		return ""
	}
	return res.Emitted
}

// remainingOf Ã¨Â¿â€Ã¥â€ºÅ¾ full Ã©â€¡Å’Ã¨Â¿ËœÃ¦Â²Â¡Ã¥Ââ€˜Ã¥â€¡ÂºÃ¥Å½Â»Ã§Å¡â€žÃ¥Â°Â¾Ã¥Â·Â´Ã£â‚¬â€šemitted Ã¤Â¸ÂºÃ§Â©ÂºÃ¦â€”Â¶Ã¨Â¿â€Ã¥â€ºÅ¾Ã¥â€¦Â¨Ã¦â€“â€¡Ã£â‚¬â€š
func remainingOf(full, emitted string) string {
	if emitted == "" {
		return full
	}
	if strings.HasPrefix(full, emitted) {
		return full[len(emitted):]
	}
	// Ã¥â€°ÂÃ§Â¼â‚¬Ã¥Â¯Â¹Ã¤Â¸ÂÃ¤Â¸Å Ã¯Â¼Ë†Ã¤Â¸Å Ã¦Â¸Â¸Ã¤Â¸Â­Ã©â‚¬â€Ã¦â€Â¹Ã¥â€ â„¢Ã¨Â¿â€¡Ã¯Â¼â€°Ã¯Â¼Å’Ã¥Â·Â²Ã¥Ââ€˜Ã§Å¡â€žÃ¦â€Â¶Ã¤Â¸ÂÃ¥â€ºÅ¾Ã¯Â¼Å’Ã¤Â¸ÂÃ¥â€ ÂÃ¨Â¡Â¥Ã¥Ââ€˜Ã¤Â»Â¥Ã¥â€¦ÂÃ©â€¡ÂÃ¥Â¤ÂÃ£â‚¬â€š
	return ""
}

// handleResponses implements OpenAI's /v1/responses (Codex CLI format).
func handleResponses(w http.ResponseWriter, r *http.Request) {
	trace := newRequestTrace("responses", "", false)
	defer trace.LogSummary()
	defer func() {
		if r.Context().Err() != nil {
			trace.SetClientDisconnected()
			if trace.Status == 0 {
				trace.SetFinish(499, "client_disconnected", "client disconnected")
			}
		}
	}()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, cfg.MaxChatBodyBytes))
	if err != nil {
		trace.SetFinish(400, "invalid_body", "bad_request")
		writeJSON(w, 400, map[string]interface{}{"error": map[string]string{"message": err.Error()}})
		return
	}
	trace.SetRequestBodyBytes(len(body))
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		trace.SetFinish(400, "invalid_json", "bad_request")
		writeJSON(w, 400, map[string]interface{}{"error": map[string]string{"message": "invalid JSON"}})
		return
	}

	modelInput := getStr(req, "model")
	if modelInput == "" {
		modelInput = rtCfg().DefaultModel
	}
	modelName, modelCfg, err := resolveModel(modelInput)
	if err != nil {
		trace.SetFinish(400, "model_resolve", "bad_request")
		writeJSON(w, 400, map[string]interface{}{"error": map[string]string{"message": err.Error()}})
		return
	}
	trace.SetModelResolved(modelInput, modelName, modelCfg.Mode)

	messages := responsesToMessages(req)

	toolsRaw, _ := req["tools"].([]interface{})
	var tools []map[string]interface{}
	for _, t := range toolsRaw {
		tm, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		// Normalize Responses API tool shape to Chat Completions shape.
		if getStr(tm, "type") == "function" && tm["function"] == nil {
			tools = append(tools, map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        getStr(tm, "name"),
					"description": getStr(tm, "description"),
					"parameters":  tm["parameters"],
				},
			})
		} else {
			tools = append(tools, tm)
		}
	}

	toolSchemaBytes := 0
	if len(tools) > 0 {
		if b, e := json.Marshal(tools); e == nil {
			toolSchemaBytes = len(b)
		}
	}
	trace.SetMessageCounts(len(messages), len(tools), len(messages), toolSchemaBytes)
	if err := rejectUnsupported(req, messages); err != nil {
		trace.SetFinish(400, "unsupported", "bad_request")
		writeJSON(w, 400, map[string]interface{}{"error": map[string]string{
			"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	images, err := collectImages(messages, "")
	if err != nil {
		trace.SetFinish(400, "collect_images", "bad_request")
		writeJSON(w, 400, map[string]interface{}{"error": map[string]string{
			"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	trace.MarkPromptBuildStart()
	prompt, latest := messagesToPrompt(messages, tools, req["tool_choice"])
	trace.MarkPromptBuildEnd(prompt)
	if strings.TrimSpace(prompt) == "" && len(images) == 0 {
		trace.SetFinish(400, "empty_prompt", "bad_request")
		writeJSON(w, 400, map[string]interface{}{"error": map[string]string{"message": "empty input"}})
		return
	}

	stream, _ := req["stream"].(bool)
	trace.Stream = stream
	rid := "resp_" + randHex(16)
	mid := "msg_" + randHex(12)

	// Responses Ã¦ÂµÂÃ¥Â¼ÂÃ¥ÂÂÃ¨Â®Â®Ã¨Â¦ÂÃ¦Â±â€šÃ¥â€¦Ë† response.output_item.added Ã¥Â£Â°Ã¦ËœÅ½ itemÃ¯Â¼Å’Ã¦â€°ÂÃ¨Æ’Â½Ã¥Â¯Â¹Ã¥Â®Æ’Ã¥Ââ€˜
	// output_text.deltaÃ¯Â¼â€ºÃ¦Â¼ÂÃ¤Âºâ€  Codex Ã¨Â¿â„¢Ã§Â±Â»Ã¤Â¸Â¥Ã¦Â Â¼Ã¥Â®Â¢Ã¦Ë†Â·Ã§Â«Â¯Ã¤Â¼Å¡Ã¦Å Â¥ "OutputTextDelta without active
	// item"Ã£â‚¬â€šmsgIndex/nextIdx Ã§Â»â„¢Ã¦Â¯ÂÃ¤Â¸Âª output item Ã¥Ë†â€ Ã©â€¦ÂÃ¥ÂºÂÃ¥ÂÂ·Ã¯Â¼Å’ensureMsg Ã¦Æ’Â°Ã¦â‚¬Â§Ã¥Â£Â°Ã¦ËœÅ½ message
	// itemÃ¯Â¼Ë†Ã©Â¦â€“Ã¤Â¸Âª delta Ã¦â€”Â¶Ã¦â€°ÂÃ¥Ââ€˜Ã¯Â¼Å’Ã§ÂºÂ¯Ã¥Â·Â¥Ã¥â€¦Â·Ã¨Â°Æ’Ã§â€Â¨Ã¨Â½Â®Ã¤Â¸ÂÃ¥Ââ€˜Ã§Â©Âº messageÃ¯Â¼â€°Ã£â‚¬â€š
	msgIndex := -1
	nextIdx := 0
	var ensureMsg func()

	// Ã¦ÂµÂÃ¥Â¼ÂÃ¨Â¦ÂÃ¥â€¦Ë†Ã¦Å Å Ã¥Â¤Â´Ã¥â€™Å’ response.created Ã¥Ââ€˜Ã¥â€¡ÂºÃ¥Å½Â»Ã¯Â¼Å’Ã¦â€°ÂÃ¨Æ’Â½Ã¨Â¾Â¹Ã¦â€Â¶Ã¨Â¾Â¹Ã¦Å½Â¨ deltaÃ£â‚¬â€š
	// Ã¤Â»Â£Ã¤Â»Â·Ã¦ËœÂ¯Ã¤Â¸â‚¬Ã¦â€”Â¦Ã¥Â¼â‚¬Ã¤Âºâ€ Ã¦ÂµÂ HTTP Ã§Å Â¶Ã¦â‚¬ÂÃ§Â ÂÃ¥Â°Â±Ã¦â€Â¹Ã¤Â¸ÂÃ¤Âºâ€ Ã¤Âºâ€ Ã¯Â¼Å’Ã¤Â¸Å Ã¦Â¸Â¸Ã¥Â¤Â±Ã¨Â´Â¥Ã¥ÂÂªÃ¨Æ’Â½Ã§â€Â¨ response.failed
	// Ã¤Âºâ€¹Ã¤Â»Â¶Ã¥â€˜Å Ã§Å¸Â¥ Ã¢â‚¬â€Ã¢â‚¬â€ Ã¨Â·Å¸ /v1/chat/completions Ã©â€šÂ£Ã¦ÂÂ¡Ã¨Â·Â¯Ã§Å¡â€žÃ¥Ââ€“Ã¨Ë†ÂÃ¤Â¸â‚¬Ã¨â€¡Â´Ã£â‚¬â€š
	var writeEvent func(string, interface{})
	var emitDelta func(string)
	var gate *toolFenceGate
	var onDelta func(string)
	var ka *streamKeepalive
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no") // Ã¥â€¦Â³Ã¦Å½â€°Ã¥ÂÂÃ¤Â»Â£Ã¥Â¯Â¹ SSE Ã§Å¡â€žÃ§Â¼â€œÃ¥â€ Â²Ã¯Â¼Å’Ã¨Â§Â sse.go
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		writeEvent = func(eventType string, payload interface{}) {
			pj, _ := json.Marshal(payload)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, pj)
			if flusher != nil {
				flusher.Flush()
			}
		}
		writeEvent("response.created", map[string]interface{}{
			"type": "response.created",
			"response": map[string]interface{}{
				"id":     rid,
				"object": "response",
				"status": "in_progress",
				"model":  modelName,
				"output": []interface{}{},
			},
		})
		ensureMsg = func() {
			if msgIndex >= 0 {
				return
			}
			msgIndex = nextIdx
			nextIdx++
			writeEvent("response.output_item.added", map[string]interface{}{
				"type":         "response.output_item.added",
				"output_index": msgIndex,
				"item": map[string]interface{}{
					"id": mid, "type": "message", "role": "assistant",
					"status": "in_progress", "content": []interface{}{},
				},
			})
			writeEvent("response.content_part.added", map[string]interface{}{
				"type": "response.content_part.added", "item_id": mid,
				"output_index": msgIndex, "content_index": 0,
				"part": map[string]interface{}{"type": "output_text", "text": "", "annotations": []interface{}{}},
			})
		}
		emitDelta = func(d string) {
			ensureMsg()
			writeEvent("response.output_text.delta", map[string]interface{}{
				"type":          "response.output_text.delta",
				"item_id":       mid,
				"output_index":  msgIndex,
				"content_index": 0,
				"delta":         d,
			})
		}
		// Ã¨Â·Å¸ chat Ã©â€šÂ£Ã¦ÂÂ¡Ã¨Â·Â¯Ã¥ÂÅ’Ã¤Â¸â‚¬Ã¥Â¥â€”Ã¥â€ºÂ´Ã¦Â ÂÃ©â€”Â¸Ã©â€”Â¨Ã¯Â¼Å¡Ã¥Â¸Â¦ tools Ã¦â€”Â¶Ã¥ÂÂªÃ¦â€Â¾Ã¨Â¡Å’Ã§Â¡Â®Ã¥Â®Å¡Ã¤Â¸ÂÃ¥Å“Â¨Ã¥â€ºÂ´Ã¦Â ÂÃ©â€¡Å’Ã§Å¡â€žÃ©Æ’Â¨Ã¥Ë†â€ Ã£â‚¬â€š
		if len(tools) == 0 {
			onDelta = emitDelta
		} else {
			gate = newToolFenceGate(emitDelta, trace)
			onDelta = gate.Push
		}
		// Ã¨Â¿â„¢Ã¦ÂÂ¡Ã¨Â·Â¯Ã¥â€œÂÃ¥Âºâ€Ã¥Â¤Â´Ã¤Â¸â‚¬Ã¥Â¼â‚¬Ã¥Â§â€¹Ã¥Â°Â±Ã¥Ââ€˜Ã¤Âºâ€ Ã¯Â¼Å’Ã¤Â¿ÂÃ¦Â´Â»Ã¦Â²Â¡Ã¦Å“â€°Ã©â€ÂÃ§Å Â¶Ã¦â‚¬ÂÃ§Â ÂÃ§Å¡â€žÃ¤Â»Â£Ã¤Â»Â·Ã£â‚¬â€š
		ka = startKeepalive(func() {
			fmt.Fprint(w, ": ping\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		})
		onDelta = ka.guard(onDelta)
	}

	// onReasoning Ã¤Â¼Â  nilÃ¯Â¼Å¡Responses API Ã¦Å“â€°Ã¨â€¡ÂªÃ¥Â·Â±Ã§Å¡â€ž reasoning Ã¤Âºâ€¹Ã¤Â»Â¶Ã¥Â½Â¢Ã§Å Â¶Ã¯Â¼Å’Ã¨Â·Å¸ chat Ã§Å¡â€ž
	// reasoning_content Ã¤Â¸ÂÃ©â‚¬Å¡Ã§â€Â¨Ã¯Â¼Å’Ã¨Â¿â„¢Ã¦ÂÂ¡Ã¨Â·Â¯Ã§â€ºÂ®Ã¥â€°ÂÃ¤Â¸ÂÃ¦Å¡Â´Ã©Å“Â²Ã¦â‚¬ÂÃ¨â‚¬Æ’Ã©â€œÂ¾Ã£â‚¬â€š
	var text string
	var toolCalls []ToolCall
	var res *StreamResult
	if rtCfg().MultiTurn && len(images) == 0 && modelCfg.Tool == 0 {
		text, toolCalls, res, err = callGeminiConv(messages, modelCfg, tools, req["tool_choice"], onDelta, nil, trace, conversationOptions{requestScope: conversationRequestScope(r, getStr(req, "prompt_cache_key"), getStr(req, "user"))})
	} else {
		text, toolCalls, res, err = callGemini(prompt, latest, modelCfg, tools, images, onDelta, nil, trace)
	}
	ka.Stop()
	if err != nil {
		mde, downgraded := err.(*ModelDowngradedError)
		status := 502
		if downgraded {
			status = 429
		}
		if res != nil && res.UpstreamModel != "" {
			trace.SetUpstreamModel(res.UpstreamModel)
		}
		trace.SetFinish(status, "error", err.Error())
		recordRequest("responses", modelName, prompt, "", res, status, err.Error(), stream)
		if stream {
			errObj := map[string]string{"message": err.Error()}
			if downgraded {
				errObj["code"] = "model_downgraded"
			}
			writeEvent("response.failed", map[string]interface{}{
				"type": "response.failed",
				"response": map[string]interface{}{
					"id":     rid,
					"object": "response",
					"status": "failed",
					"model":  modelName,
					"error":  errObj,
				},
			})
			return
		}
		if downgraded {
			writeModelDowngraded(w, mde)
			return
		}
		if ptl, ok := err.(*PromptTooLongError); ok {
			// Ã¦ËœÅ½Ã§Â¡Â®Ã¦Å Â¥ 400 Ã¨â‚¬Å’Ã¤Â¸ÂÃ¦ËœÂ¯Ã¥Ââ€˜Ã¥â€¡ÂºÃ¥Å½Â»Ã¨Â®Â©Ã¤Â¸Å Ã¦Â¸Â¸Ã¦Å Å Ã§â€Â¨Ã¦Ë†Â·Ã§Å¡â€žÃ©â€”Â®Ã©Â¢ËœÃ¦Ë†ÂªÃ¦Å½â€° Ã¢â‚¬â€Ã¢â‚¬â€ Ã©â€šÂ£Ã¦Â Â·Ã¥Â®Â¢Ã¦Ë†Â·Ã§Â«Â¯Ã¦â€¹Â¿Ã¥Ë†Â°Ã§Å¡â€žÃ¦ËœÂ¯
			// Ã¤Â¸â‚¬Ã¤Â¸ÂªÃ§Â­â€Ã©ÂÅ¾Ã¦â€°â‚¬Ã©â€”Â®Ã§Å¡â€ž 200Ã¯Â¼Å’Ã¦Â Â¹Ã¦Å“Â¬Ã§Å“â€¹Ã¤Â¸ÂÃ¥â€¡ÂºÃ¨Â¯Â·Ã¦Â±â€šÃ¥â€¦Â¶Ã¥Â®Å¾Ã¦Â²Â¡Ã©â‚¬ÂÃ¥Ë†Â°Ã£â‚¬â€š
			writeJSON(w, 400, map[string]interface{}{"error": map[string]string{
				"message": ptl.Error(), "type": "invalid_request_error",
				"code": "context_length_exceeded"}})
			return
		}
		if rle, ok := err.(*RateLimitError); ok {
			writeJSON(w, 429, map[string]interface{}{"error": map[string]string{
				"message": rle.Error(),
				"type":    "rate_limit_exceeded",
				"code":    "ip_slot_full",
			}})
			return
		}
		writeJSON(w, 502, map[string]interface{}{"error": map[string]string{"message": "upstream error: " + err.Error()}})
		return
	}

	var output []map[string]interface{}
	for _, tc := range toolCalls {
		output = append(output, map[string]interface{}{
			"type":      "function_call",
			"id":        tc.ID,
			"call_id":   tc.ID,
			"name":      tc.Function.Name,
			"arguments": tc.Function.Arguments,
			"status":    "completed",
		})
	}
	if text != "" || len(toolCalls) == 0 {
		output = append(output, map[string]interface{}{
			"type":   "message",
			"id":     mid,
			"role":   "assistant",
			"status": "completed",
			"content": []map[string]interface{}{{
				"type":        "output_text",
				"text":        text,
				"annotations": []interface{}{},
			}},
		})
	}

	if res != nil && res.UpstreamModel != "" {
		trace.SetUpstreamModel(res.UpstreamModel)
	}
	trace.SetFinish(200, "ok", "")
	recordRequest("responses", modelName, prompt, text, res, 200, "", stream)
	if stream {
		// Ã¨Â¡Â¥Ã¤Â¸Å Ã©â€”Â¸Ã©â€”Â¨Ã¦â€°Â£Ã¤Â½ÂÃ£â‚¬ÂÃ¦Ë†â€“Ã¥â€°ÂÃ§Â¼â‚¬ diff Ã¨Â·Â³Ã¨Â¿â€¡Ã§Å¡â€žÃ¥Â°Â¾Ã¥Â·Â´Ã¯Â¼Å’Ã¥â€ ÂÃ¥Ââ€˜Ã§Â»Ë†Ã¦â‚¬ÂÃ¤Âºâ€¹Ã¤Â»Â¶Ã£â‚¬â€š
		if gate != nil {
			trace.FinishToolGate(gate.BufferedBytes())
		}
		if rest := remainingOf(text, sentText(res, gate)); rest != "" {
			if gate != nil {
				trace.MarkToolGateFallback()
			}
			emitDelta(rest)
		}
		for _, item := range output {
			switch item["type"] {
			case "function_call":
				// Ã¥Â·Â¥Ã¥â€¦Â·Ã¨Â°Æ’Ã§â€Â¨ item Ã¤Â¹Å¸Ã¨Â¦Â added Ã¢â€ â€™ done Ã¥Å’â€¦Ã¨ÂµÂ·Ã¦ÂÂ¥Ã¯Â¼Å’Codex Ã¦â€°ÂÃ¨Â®Â¤Ã£â‚¬â€š
				idx := nextIdx
				nextIdx++
				writeEvent("response.output_item.added", map[string]interface{}{
					"type": "response.output_item.added", "output_index": idx, "item": item,
				})
				writeEvent("response.function_call_arguments.done", map[string]interface{}{
					"type":      "response.function_call_arguments.done",
					"item_id":   item["id"],
					"call_id":   item["call_id"],
					"name":      item["name"],
					"arguments": item["arguments"],
				})
				writeEvent("response.output_item.done", map[string]interface{}{
					"type": "response.output_item.done", "output_index": idx, "item": item,
				})
			case "message":
				ensureMsg() // Ã¦Â²Â¡Ã¦Å“â€° delta Ã¤Â½â€ Ã¦Å“â€°Ã¦Â­Â£Ã¦â€“â€¡Ã¦â€”Â¶Ã¯Â¼Å’Ã¨Â¿â„¢Ã©â€¡Å’Ã¨Â¡Â¥Ã¥Â£Â°Ã¦ËœÅ½ message item
				if cps, ok := item["content"].([]map[string]interface{}); ok {
					for ci, cp := range cps {
						writeEvent("response.output_text.done", map[string]interface{}{
							"type":          "response.output_text.done",
							"item_id":       item["id"],
							"output_index":  msgIndex,
							"content_index": ci,
							"text":          cp["text"],
						})
						writeEvent("response.content_part.done", map[string]interface{}{
							"type": "response.content_part.done", "item_id": item["id"],
							"output_index": msgIndex, "content_index": ci,
							"part": map[string]interface{}{"type": "output_text", "text": cp["text"], "annotations": []interface{}{}},
						})
					}
				}
				writeEvent("response.output_item.done", map[string]interface{}{
					"type": "response.output_item.done", "output_index": msgIndex, "item": item,
				})
			}
		}
		respObj := map[string]interface{}{
			"id":     rid,
			"object": "response",
			"status": "completed",
			"model":  modelName,
			"output": output,
			"usage":  buildUsage(prompt, text, true),
		}
		writeEvent("response.completed", map[string]interface{}{
			"type":     "response.completed",
			"response": respObj,
		})
		return
	}

	writeJSON(w, 200, map[string]interface{}{
		"id":         rid,
		"object":     "response",
		"created_at": time.Now().Unix(),
		"status":     "completed",
		"model":      modelName,
		"output":     output,
		"usage":      buildUsage(prompt, text, true),
	})
}
