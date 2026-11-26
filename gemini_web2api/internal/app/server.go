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

const Version = "4.35.0"

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

// buildUsage ç”¨ tiktoken ç®— token æ•°ï¼Œè·Ÿ requests è¡¨é‡Œè®°çš„å£å¾„ä¿æŒä¸€è‡´ã€‚
// responsesAPI=true æ—¶ç”¨ /v1/responses çš„å­—æ®µåï¼ˆinput_tokens/output_tokensï¼‰ã€‚
func buildUsage(prompt, text string, responsesAPI bool) map[string]int {
	return buildUsageWithReasoning(prompt, text, "", responsesAPI)
}

// buildUsageWithReasoning åœ¨ usage é‡Œå•åˆ—æ€è€ƒé“¾çš„ token æ•°ã€‚
//
// æ€è€ƒé“¾**ä¸è®¡å…¥** completion_tokensï¼šå®ƒæ˜¯æ¨¡åž‹è‡ªå·±çš„æŽ¨ç†è¿‡ç¨‹ï¼Œå®¢æˆ·ç«¯é»˜è®¤ä¸å±•ç¤ºï¼Œ
// ç®—è¿›åŽ»ç­‰äºŽè®©ç”¨æˆ·ä¸ºçœ‹ä¸è§çš„è¾“å‡ºä¹°å•ï¼ˆä¸‹æ¸¸ newapi æ˜¯æŒ‰ completion_tokens è®¡è´¹çš„ï¼‰ã€‚
// å•åˆ—åœ¨ completion_tokens_details.reasoning_tokens é‡Œï¼Œè·Ÿ OpenAI çš„åšæ³•ä¸€è‡´ï¼Œ
// æƒ³è®¡è´¹çš„äººè‡ªå·±åŠ ã€‚
func buildUsageWithReasoning(prompt, text, reasoning string, responsesAPI bool) map[string]int {
	// åª’ä½“äº§ç‰©æ˜¯ base64 data URLï¼Œä¸Šç™¾ä¸‡å­—ç¬¦ã€‚ä¸è®¡è¿› output tokenï¼šé‚£æ˜¯äºŒè¿›åˆ¶å†…å®¹ï¼Œ
	// æŒ‰ token è®¡è´¹ç­‰äºŽè®©ç”¨æˆ·ä¸ºçœ‹ä¸è§çš„å­—èŠ‚ä¹°å•ï¼Œä¸‹æ¸¸ newapi æŒ‰ output token æ”¶é’±ã€‚
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

// rejectUnsupported æ£€æŸ¥å®¢æˆ·ç«¯ä¼ äº†ä½†æˆ‘ä»¬å…‘çŽ°ä¸äº†çš„å­—æ®µã€‚
//
// åªæ‹¦"é™é»˜å¿½ç•¥ä¼šè®©å®¢æˆ·ç«¯æ‹¿åˆ°é”™è¯¯ç»“æžœ"çš„ï¼šn>1 å°‘ç»™å€™é€‰ã€å›¾ç‰‡è¾“å…¥è¢«ä¸¢æŽ‰ä¼š
// è®©æ¨¡åž‹ç­”éžæ‰€é—®ã€‚é‡‡æ ·ç±»å‚æ•°ï¼ˆtemperature/top_p/max_tokens/...ï¼‰ä¸Šæ¸¸æ ¹æœ¬
// æ²¡æœ‰å¯¹åº”æ—‹é’®ï¼Œæ”¶ä¸‹å¿½ç•¥å³å¯ï¼ŒæŠ¥é”™åè€Œä¼šæŒ¡ä½æ­£å¸¸å®¢æˆ·ç«¯ã€‚
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
				// æœ‰ cookie å°±èƒ½æ”¶ï¼šå›¾ä¼šè¢«ä¸Šä¼ æˆé™„ä»¶å†å¼•ç”¨ã€‚åŒ¿åä¸è¡Œ â€”â€” ä¼ å¾—ä¸ŠåŽ»ï¼Œ
				// ä½†ä¸€å¼•ç”¨å°±è¢«æœåŠ¡ç«¯æ‹’ï¼Œæ”¶ä¸‹åªä¼šè®©å®¢æˆ·ç«¯æ‹¿åˆ°ä¸€ä¸ªçœ‹ä¸æ‡‚çš„å¤±è´¥ã€‚
				if !hasCookie() {
					return fmt.Errorf("image input needs a Google account cookie: anonymous " +
						"uploads succeed but referencing them in a conversation is rejected " +
						"upstream. Add a cookie in the admin panel (Cookie pool)")
				}
			case "video_url", "input_video":
				// è§†é¢‘è·Ÿå›¾ç‰‡åŒç†ï¼šç™»å½•æ€æ‰èƒ½å¼•ç”¨ï¼ŒåŒ¿åä¸€å¼•ç”¨å°± 1100ã€‚
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

func callGemini(prompt, latest string, mc ModelConfig, tools []map[string]interface{},
	images []pendingUpload, onDelta, onReasoning func(string)) (string, []ToolCall, *StreamResult, error) {
	res, err := streamGenerateWithFiles(prompt, latest, mc, images, onDelta, onReasoning)
	if err != nil {
		// res éž nilï¼šå¤±è´¥æ—¶å®ƒåªå¸¦å½’å±žï¼ˆå“ªä¸ªå· / å“ªä¸ªå‡ºå£ï¼‰ï¼Œç»™ recordRequest ç”¨
		return "", nil, res, err
	}
	text := extractResponseText(res.Raw)
	// ç”»å¸ƒï¼šHTML æ–‡æ¡£å†…è”åœ¨å“åº”é‡Œï¼ˆä¸åƒå›¾/ä¹è¦ä¸‹è½½ï¼‰ï¼Œä»Ž immersive ç»“æž„é‡ŒæŠ å‡ºæ¥ã€‚
	// æ ‡å‡†æ–‡æœ¬æå–åªæ‹¿åˆ° preambleï¼ˆ"I will generateâ€¦"ï¼‰ï¼Œæ–‡æ¡£åœ¨ inner[4][0][30]â€¦ï¼Œ
	// ç”¨ extractCanvasDoc å•ç‹¬å–ã€‚
	if mc.Tool == toolCanvas {
		doc := extractCanvasDoc(res.Raw)
		if doc == "" {
			return "", nil, res, fmt.Errorf("canvas generation failed: no HTML document in response (raw %d bytes)", len(res.Raw))
		}
		if text != "" && !strings.Contains(doc, text) {
			return text + "\n\n" + doc, nil, res, nil // preamble + æ–‡æ¡£
		}
		return doc, nil, res, nil
	}
	// åª’ä½“æ¨¡åž‹ï¼ˆç”Ÿå›¾/éŸ³ä¹ï¼‰ï¼šç”Ÿæˆ 200 äº†ä½†äº§ç‰©å­—èŠ‚æ²¡å–å›žæ¥ï¼Œç›´æŽ¥æŠ¥é”™è€Œä¸æ˜¯è¿”å›žä¸€ä¸ª
	// åªæœ‰æ–‡å­—æ²¡æœ‰å›¾çš„åŠæˆå“ â€”â€” å®¢æˆ·ç«¯è¦çš„å°±æ˜¯é‚£å¼ å›¾/é‚£æ®µä¹ã€‚
	if mc.Tool == toolImage || mc.Tool == toolMusic || mc.Tool == toolVideo {
		if len(res.Artifacts) == 0 {
			cause := res.MediaErr
			if cause == nil {
				cause = errors.New("åª’ä½“äº§ç‰©å–å›žå¤±è´¥")
			}
			return "", nil, res, fmt.Errorf("media generation succeeded but artifact retrieval failed: %w", cause)
		}
		// äº§ç‰©ä»¥ base64 data URL è¿½åŠ åˆ°æ­£æ–‡ï¼ˆå¯èƒ½æ²¡æ­£æ–‡ï¼Œåªæœ‰å›¾/ä¹/è§†é¢‘ï¼‰ã€‚
		text = appendArtifactMarkdown(text, res.Artifacts)
		return text, nil, res, nil
	}
	// æ–‡æœ¬æ¨¡åž‹è¢«ä¸Šæ¸¸åˆ‡åŽ»ç”Ÿå›¾ / éŸ³ä¹ï¼ˆ#105ï¼‰ï¼šäº§ç‰©ç…§æ ·é™„åœ¨æ­£æ–‡åŽé¢ã€‚
	if len(res.Artifacts) > 0 {
		var toolCalls []ToolCall
		if len(tools) > 0 {
			text, toolCalls = parseToolCalls(text)
		}
		return appendArtifactMarkdown(text, res.Artifacts), toolCalls, res, nil
	}
	if text == "" {
		// ä¸Šæ¸¸æ‹’ç»æ—¶åªå›žä¸€ä¸ªç»“æŸå¸§ã€æ²¡æœ‰å†…å®¹å¸§ï¼ˆå®žæµ‹è¢«æ‹’æ—¶ raw ä»… 216
		// å­—èŠ‚ï¼‰ã€‚è¿™ç§æƒ…å†µå¿…é¡»æŠ¥é”™ï¼šä»¥å‰ä¼šå½“æˆç©ºå›žå¤è¿”å›ž 200 + content:nullï¼Œ
		// å®¢æˆ·ç«¯çœ‹ä¸å‡ºè¯·æ±‚å…¶å®žå¤±è´¥äº†ã€‚
		// æ³¨æ„ä¸èƒ½ç”¨ BardErrorInfo åˆ¤é”™ â€”â€” æ­£å¸¸å“åº”çš„ç»“æŸå¸§é‡Œä¹Ÿå¸¦è¿™ä¸ªç ã€‚
		return "", nil, res, fmt.Errorf("upstream returned no content frame (raw %d bytes)", len(res.Raw))
	}
	var toolCalls []ToolCall
	if len(tools) > 0 {
		text, toolCalls = parseToolCalls(text)
	}
	// å›žå¤åªæœ‰ä¸€ä¸ªè§£æžä¸äº†çš„ ```tool_call``` å›´æ ï¼ˆJSON å†™å / æ²¡æœ‰å·¥å…·åï¼‰æ—¶ï¼Œ
	// parseToolCalls å‰¥æŽ‰å›´æ åŽæ­£æ–‡ä¸ºç©ºã€ä¹Ÿæ²¡æœ‰è°ƒç”¨ã€‚è·Ÿ callGeminiConv ä¸€æ ·æŠ¥é”™ï¼Œ
	// ä¸èƒ½å›ž 200 + content:null ä¸”ä¸å¸¦ tool_calls â€”â€” å®¢æˆ·ç«¯çœ‹ä¸å‡ºè¯·æ±‚å…¶å®žå¤±è´¥äº†ã€‚
	if text == "" && len(toolCalls) == 0 {
		return "", nil, res, fmt.Errorf("upstream reply had only an unparseable tool_call block (raw %d bytes)", len(res.Raw))
	}
	return text, toolCalls, res, nil
}

// recordRequest writes one row of metadata to the requests table.
// Privacy: the prompt/response strings themselves are never persisted â€”
// only their length, model name, latency, status, and proxy info.
func recordRequest(endpoint, model, prompt, response string, res *StreamResult, status int, errStr string, stream bool) {
	// åª’ä½“äº§ç‰©æ˜¯è¶…é•¿ base64ï¼Œå‰¥æŽ‰å†ç®— token/é•¿åº¦ï¼šæ—¢ä¸è®©å®ƒæ±¡æŸ“ç»Ÿè®¡ï¼Œä¹Ÿå…å¾—åœ¨è¯·æ±‚çº¿ç¨‹é‡Œ
	// å¯¹ä¸Šç™¾ä¸‡å­—ç¬¦è·‘ tiktoken ç™½ç™½æ‹–æ…¢ã€‚
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
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, cfg.MaxChatBodyBytes))
	if err != nil {
		writeJSON(w, 400, map[string]interface{}{"error": map[string]string{"message": err.Error()}})
		return
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, 400, map[string]interface{}{"error": map[string]string{"message": "invalid JSON"}})
		return
	}

	modelInput := getStr(req, "model")
	if modelInput == "" {
		modelInput = rtCfg().DefaultModel
	}
	modelName, modelCfg, err := resolveModel(modelInput)
	if err != nil {
		writeJSON(w, 400, map[string]interface{}{"error": map[string]string{"message": err.Error()}})
		return
	}

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

	if err := rejectUnsupported(req, messages); err != nil {
		writeJSON(w, 400, map[string]interface{}{"error": map[string]string{
			"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	// å›¾ç‰‡å…ˆè§£å‡ºæ¥å¸¦ç€ï¼ŒçœŸæ­£ä¸Šä¼ è¦ç­‰æŒ‘å®Œè´¦å·å’Œå‡ºå£ï¼ˆè§ streamGenerateï¼‰ã€‚
	images, err := collectImages(messages, "")
	if err != nil {
		writeJSON(w, 400, map[string]interface{}{"error": map[string]string{
			"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	prompt, latest := messagesToPrompt(messages, tools, req["tool_choice"])
	if strings.TrimSpace(prompt) == "" && len(images) == 0 {
		writeJSON(w, 400, map[string]interface{}{"error": map[string]string{"message": "empty prompt"}})
		return
	}

	stream, _ := req["stream"].(bool)
	includeUsage := false
	if so, ok := req["stream_options"].(map[string]interface{}); ok {
		includeUsage, _ = so["include_usage"].(bool)
	}

	cid := "chatcmpl-" + randHex(12)
	created := time.Now().Unix()

	// å¸¦ tools æ—¶æ­£æ–‡è¿‡ä¸€é“å›´æ é—¸é—¨ï¼š```tool_call``` å—è¦å®Œæ•´æ–‡æœ¬æ‰èƒ½è§£æžï¼Œ
	// ç›´æŽ¥è½¬å‘ä¼šæŠŠå›´æ åŽŸæ–‡æŽ¨ç»™å®¢æˆ·ç«¯ï¼Œæ‰€ä»¥åªæ”¾è¡Œç¡®å®šä¸åœ¨å›´æ é‡Œçš„éƒ¨åˆ†ã€‚
	// æ€è€ƒé“¾è·Ÿå›´æ æ— å…³ï¼Œä¸¤ç§æƒ…å†µéƒ½ç›´æŽ¥æµã€‚
	var sse *sseWriter
	var gate *toolFenceGate
	var onDelta, onReasoning func(string)
	var ka *streamKeepalive
	if stream {
		sse = newSSEWriter(w, cid, created, modelName)
		onReasoning = sse.SendReasoning
		if len(tools) == 0 {
			onDelta = sse.SendContent
		} else {
			gate = newToolFenceGate(sse.SendContent)
			onDelta = gate.Push
		}
		ka = startKeepalive(sse.Ping)
		onDelta, onReasoning = ka.guard(onDelta), ka.guard(onReasoning)
	}

	var text string
	var toolCalls []ToolCall
	var res *StreamResult
	if rtCfg().MultiTurn && len(images) == 0 && modelCfg.Tool == 0 {
		// å¤šè½®ï¼šæŒ‰åŽ†å²å‰ç¼€è¯†åˆ«ç»­æŽ¥ï¼Œå‘½ä¸­å°±åªå‘æ–°æ¶ˆæ¯ã€åŽ†å²ç•™æœåŠ¡ç«¯ã€‚å¸¦ tools ä¹Ÿèµ°è¿™æ¡ã€‚
		text, toolCalls, res, err = callGeminiConv(messages, modelCfg, tools, req["tool_choice"], onDelta, onReasoning, conversationOptions{requestScope: conversationRequestScope(r, getStr(req, "prompt_cache_key"), getStr(req, "user"))})
	} else {
		text, toolCalls, res, err = callGemini(prompt, latest, modelCfg, tools, images, onDelta, onReasoning)
	}
	ka.Stop() // ä¹‹åŽçš„æ”¶å°¾å›žåˆ°å• goroutine å†™ï¼›Started() ä¹Ÿè¦åœ¨ä¿æ´»åœä¸‹åŽå†çœ‹
	if err != nil {
		mde, downgraded := err.(*ModelDowngradedError)
		status := 502
		if downgraded {
			status = 429
		}
		recordRequest("chat.completions", modelName, prompt, "", res, status, err.Error(), stream)
		if sse != nil && sse.Started() {
			sse.Fail(err) // å·²ç»å¼€æµï¼ŒHTTP çŠ¶æ€ç æ”¹ä¸äº†äº†
			return
		}
		if downgraded {
			writeModelDowngraded(w, mde)
			return
		}
		if ptl, ok := err.(*PromptTooLongError); ok {
			// æ˜Žç¡®æŠ¥ 400 è€Œä¸æ˜¯å‘å‡ºåŽ»è®©ä¸Šæ¸¸æŠŠç”¨æˆ·çš„é—®é¢˜æˆªæŽ‰ â€”â€” é‚£æ ·å®¢æˆ·ç«¯æ‹¿åˆ°çš„æ˜¯
			// ä¸€ä¸ªç­”éžæ‰€é—®çš„ 200ï¼Œæ ¹æœ¬çœ‹ä¸å‡ºè¯·æ±‚å…¶å®žæ²¡é€åˆ°ã€‚
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
	// æ€è€ƒé“¾èµ° reasoning_content â€”â€” DeepSeek-R1 å¸¦èµ·æ¥çš„äº‹å®žæ ‡å‡†ï¼Œnewapi å’Œ
	// ä¸»æµå®¢æˆ·ç«¯éƒ½è®¤ï¼Œä¼šæ¸²æŸ“æˆå¯æŠ˜å çš„ã€Œæ€è€ƒè¿‡ç¨‹ã€ã€‚åªæœ‰ 3.1 Pro æœ‰ï¼Œå…¶ä½™ä¸ºç©ºã€‚
	if res != nil && res.Reasoning != "" {
		msg["reasoning_content"] = res.Reasoning
	}
	finish := "stop"
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
		finish = "tool_calls"
	}

	recordRequest("chat.completions", modelName, prompt, text, res, 200, "", stream)
	if stream {
		var usage map[string]int
		if includeUsage {
			usage = usageOf(prompt, text, res)
		}
		// çœŸæµå¼å·²å‘å‡ºçš„ä¸é‡å‘ï¼Œåªè¡¥å°¾å·´ï¼›æœ‰ tools æ—¶æ²¡èµ°çœŸæµå¼ï¼Œè¿™é‡Œå‘å…¨é‡ã€‚
		// æ€è€ƒé“¾åŒç†ï¼Œä¸”è¦åœ¨æ­£æ–‡ä¹‹å‰è¡¥â€”â€”ä¿æŒã€Œå…ˆæ€è€ƒåŽå›žç­”ã€çš„é¡ºåºã€‚
		if res != nil && res.Reasoning != "" {
			if rest := remainingOf(res.Reasoning, res.EmittedReasoning); rest != "" {
				sse.SendReasoning(rest)
			}
		}
		// è¡¥å°¾å·´è¦è·Ÿ**å®žé™…å‘ç»™å®¢æˆ·ç«¯çš„å†…å®¹**æ¯”ã€‚èµ°äº†é—¸é—¨æ—¶ res.Emitted å«å›´æ 
		// åŽŸæ–‡ï¼Œæ‹¿å®ƒæ¯”å‰ç¼€ä¼šå¯¹ä¸ä¸Šï¼Œå°¾å·´ä¼šæ•´æ®µä¸¢æŽ‰ã€‚
		if rest := remainingOf(text, sentText(res, gate)); rest != "" {
			sse.SendContent(rest)
		}
		if len(toolCalls) > 0 {
			sse.SendToolCalls(toolCalls)
		}
		sse.Finish(finish, usage)
		return
	}

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

// usageOf æ˜¯ buildUsageWithReasoning çš„ä¾¿æ·åŒ…è£…ï¼Œres å¯èƒ½ä¸º nilã€‚
func usageOf(prompt, text string, res *StreamResult) map[string]int {
	r := ""
	if res != nil {
		r = res.Reasoning
	}
	return buildUsageWithReasoning(prompt, text, r, false)
}

// remainingText è¿”å›žæœ€ç»ˆæ–‡æœ¬é‡Œè¿˜æ²¡é€šè¿‡ onDelta å‘å‡ºåŽ»çš„éƒ¨åˆ†ã€‚
// çœŸæµå¼ä¸‹é€šå¸¸åªå‰©æœ«å°¾ä¸€ç‚¹æˆ–ä¸ºç©ºï¼›æ²¡èµ°çœŸæµå¼æ—¶ Emitted ä¸ºç©ºï¼Œè¿”å›žå…¨æ–‡ã€‚
func remainingText(text string, res *StreamResult) string {
	if res == nil {
		return text
	}
	return remainingOf(text, res.Emitted)
}

// sentText è¿”å›žæœ¬æ¬¡å®žé™…å‘ç»™å®¢æˆ·ç«¯çš„æ­£æ–‡ã€‚
// æ²¡èµ°å›´æ é—¸é—¨æ—¶å°±æ˜¯ deltaTracker å‘å‡ºçš„é‚£äº›ï¼›èµ°äº†é—¸é—¨æ—¶ä»¥é—¸é—¨ä¸ºå‡† â€”â€”
// é—¸é—¨æ‰£æŽ‰äº†å›´æ ï¼Œè·Ÿ res.Emitted ä¸æ˜¯åŒä¸€ä»½æ–‡æœ¬ã€‚
func sentText(res *StreamResult, gate *toolFenceGate) string {
	if gate != nil {
		return gate.Sent()
	}
	if res == nil {
		return ""
	}
	return res.Emitted
}

// remainingOf è¿”å›ž full é‡Œè¿˜æ²¡å‘å‡ºåŽ»çš„å°¾å·´ã€‚emitted ä¸ºç©ºæ—¶è¿”å›žå…¨æ–‡ã€‚
func remainingOf(full, emitted string) string {
	if emitted == "" {
		return full
	}
	if strings.HasPrefix(full, emitted) {
		return full[len(emitted):]
	}
	// å‰ç¼€å¯¹ä¸ä¸Šï¼ˆä¸Šæ¸¸ä¸­é€”æ”¹å†™è¿‡ï¼‰ï¼Œå·²å‘çš„æ”¶ä¸å›žï¼Œä¸å†è¡¥å‘ä»¥å…é‡å¤ã€‚
	return ""
}

// handleResponses implements OpenAI's /v1/responses (Codex CLI format).
func handleResponses(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, cfg.MaxChatBodyBytes))
	if err != nil {
		writeJSON(w, 400, map[string]interface{}{"error": map[string]string{"message": err.Error()}})
		return
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, 400, map[string]interface{}{"error": map[string]string{"message": "invalid JSON"}})
		return
	}

	modelInput := getStr(req, "model")
	if modelInput == "" {
		modelInput = rtCfg().DefaultModel
	}
	modelName, modelCfg, err := resolveModel(modelInput)
	if err != nil {
		writeJSON(w, 400, map[string]interface{}{"error": map[string]string{"message": err.Error()}})
		return
	}

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

	if err := rejectUnsupported(req, messages); err != nil {
		writeJSON(w, 400, map[string]interface{}{"error": map[string]string{
			"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	images, err := collectImages(messages, "")
	if err != nil {
		writeJSON(w, 400, map[string]interface{}{"error": map[string]string{
			"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	prompt, latest := messagesToPrompt(messages, tools, req["tool_choice"])
	if strings.TrimSpace(prompt) == "" && len(images) == 0 {
		writeJSON(w, 400, map[string]interface{}{"error": map[string]string{"message": "empty input"}})
		return
	}

	stream, _ := req["stream"].(bool)
	rid := "resp_" + randHex(16)
	mid := "msg_" + randHex(12)

	// Responses æµå¼åè®®è¦æ±‚å…ˆ response.output_item.added å£°æ˜Ž itemï¼Œæ‰èƒ½å¯¹å®ƒå‘
	// output_text.deltaï¼›æ¼äº† Codex è¿™ç±»ä¸¥æ ¼å®¢æˆ·ç«¯ä¼šæŠ¥ "OutputTextDelta without active
	// item"ã€‚msgIndex/nextIdx ç»™æ¯ä¸ª output item åˆ†é…åºå·ï¼ŒensureMsg æƒ°æ€§å£°æ˜Ž message
	// itemï¼ˆé¦–ä¸ª delta æ—¶æ‰å‘ï¼Œçº¯å·¥å…·è°ƒç”¨è½®ä¸å‘ç©º messageï¼‰ã€‚
	msgIndex := -1
	nextIdx := 0
	var ensureMsg func()

	// æµå¼è¦å…ˆæŠŠå¤´å’Œ response.created å‘å‡ºåŽ»ï¼Œæ‰èƒ½è¾¹æ”¶è¾¹æŽ¨ deltaã€‚
	// ä»£ä»·æ˜¯ä¸€æ—¦å¼€äº†æµ HTTP çŠ¶æ€ç å°±æ”¹ä¸äº†äº†ï¼Œä¸Šæ¸¸å¤±è´¥åªèƒ½ç”¨ response.failed
	// äº‹ä»¶å‘ŠçŸ¥ â€”â€” è·Ÿ /v1/chat/completions é‚£æ¡è·¯çš„å–èˆä¸€è‡´ã€‚
	var writeEvent func(string, interface{})
	var emitDelta func(string)
	var gate *toolFenceGate
	var onDelta func(string)
	var ka *streamKeepalive
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no") // å…³æŽ‰åä»£å¯¹ SSE çš„ç¼“å†²ï¼Œè§ sse.go
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
		// è·Ÿ chat é‚£æ¡è·¯åŒä¸€å¥—å›´æ é—¸é—¨ï¼šå¸¦ tools æ—¶åªæ”¾è¡Œç¡®å®šä¸åœ¨å›´æ é‡Œçš„éƒ¨åˆ†ã€‚
		if len(tools) == 0 {
			onDelta = emitDelta
		} else {
			gate = newToolFenceGate(emitDelta)
			onDelta = gate.Push
		}
		// è¿™æ¡è·¯å“åº”å¤´ä¸€å¼€å§‹å°±å‘äº†ï¼Œä¿æ´»æ²¡æœ‰é”çŠ¶æ€ç çš„ä»£ä»·ã€‚
		ka = startKeepalive(func() {
			fmt.Fprint(w, ": ping\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		})
		onDelta = ka.guard(onDelta)
	}

	// onReasoning ä¼  nilï¼šResponses API æœ‰è‡ªå·±çš„ reasoning äº‹ä»¶å½¢çŠ¶ï¼Œè·Ÿ chat çš„
	// reasoning_content ä¸é€šç”¨ï¼Œè¿™æ¡è·¯ç›®å‰ä¸æš´éœ²æ€è€ƒé“¾ã€‚
	var text string
	var toolCalls []ToolCall
	var res *StreamResult
	if rtCfg().MultiTurn && len(images) == 0 && modelCfg.Tool == 0 {
		text, toolCalls, res, err = callGeminiConv(messages, modelCfg, tools, req["tool_choice"], onDelta, nil, conversationOptions{requestScope: conversationRequestScope(r, getStr(req, "prompt_cache_key"), getStr(req, "user"))})
	} else {
		text, toolCalls, res, err = callGemini(prompt, latest, modelCfg, tools, images, onDelta, nil)
	}
	ka.Stop()
	if err != nil {
		mde, downgraded := err.(*ModelDowngradedError)
		status := 502
		if downgraded {
			status = 429
		}
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
			// æ˜Žç¡®æŠ¥ 400 è€Œä¸æ˜¯å‘å‡ºåŽ»è®©ä¸Šæ¸¸æŠŠç”¨æˆ·çš„é—®é¢˜æˆªæŽ‰ â€”â€” é‚£æ ·å®¢æˆ·ç«¯æ‹¿åˆ°çš„æ˜¯
			// ä¸€ä¸ªç­”éžæ‰€é—®çš„ 200ï¼Œæ ¹æœ¬çœ‹ä¸å‡ºè¯·æ±‚å…¶å®žæ²¡é€åˆ°ã€‚
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

	recordRequest("responses", modelName, prompt, text, res, 200, "", stream)
	if stream {
		// è¡¥ä¸Šé—¸é—¨æ‰£ä½ã€æˆ–å‰ç¼€ diff è·³è¿‡çš„å°¾å·´ï¼Œå†å‘ç»ˆæ€äº‹ä»¶ã€‚
		if rest := remainingOf(text, sentText(res, gate)); rest != "" {
			emitDelta(rest)
		}
		for _, item := range output {
			switch item["type"] {
			case "function_call":
				// å·¥å…·è°ƒç”¨ item ä¹Ÿè¦ added â†’ done åŒ…èµ·æ¥ï¼ŒCodex æ‰è®¤ã€‚
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
				ensureMsg() // æ²¡æœ‰ delta ä½†æœ‰æ­£æ–‡æ—¶ï¼Œè¿™é‡Œè¡¥å£°æ˜Ž message item
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
