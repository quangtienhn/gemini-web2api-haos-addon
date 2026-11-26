package app

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"strings"
)

// ─── CORS ────────────────────────────────────────────────────────────────────
//
// 默认不发 Access-Control-Allow-Origin：浏览器只能从同源页面调用 API。
// 需要跨域时在 config.json 里显式配 cors_allow_origins（或 ["*"]）。
// 这里用 ResponseWriter 包装器在 WriteHeader 时统一覆盖 handler 里写死的
// "*"，避免逐个 handler 改动、也避免漏掉新加的端点。

type corsResponseWriter struct {
	http.ResponseWriter
	allow string
}

func (c *corsResponseWriter) WriteHeader(code int) {
	if c.allow != "" {
		c.Header().Set("Access-Control-Allow-Origin", c.allow)
		if c.allow != "*" {
			c.Header().Add("Vary", "Origin")
		}
	} else {
		c.Header().Del("Access-Control-Allow-Origin")
		c.Header().Del("Access-Control-Allow-Credentials")
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *corsResponseWriter) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (c *corsResponseWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }

func corsAllowOrigin(origin string) string {
	if origin == "" {
		return ""
	}
	for _, o := range cfg.CORSAllowOrigins {
		o = strings.TrimSpace(o)
		if o == "*" {
			return "*"
		}
		if strings.EqualFold(o, origin) {
			return origin
		}
	}
	return ""
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allow := corsAllowOrigin(r.Header.Get("Origin"))
		cw := &corsResponseWriter{ResponseWriter: w, allow: allow}
		if allow != "" && allow != "*" {
			cw.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		next.ServeHTTP(cw, r)
	})
}

// ─── Security headers ────────────────────────────────────────────────────────

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		// HA ingress 用同源 iframe 嵌面板，SAMEORIGIN 不会挡它；DENY 会。
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// ─── Request body limits ─────────────────────────────────────────────────────

// bodyLimitFor 按端点返回请求体上限。文本请求和视频请求的量级差一个数量级，
// 用一个全局值会要么卡死视频、要么让文本端点能塞 100MB。
func bodyLimitFor(path string) int64 {
	var v int64
	switch {
	case strings.HasPrefix(path, "/v1/videos"):
		v = cfg.MaxVideoBodyBytes
	case strings.HasPrefix(path, "/admin/"):
		v = cfg.MaxAdminBodyBytes
	case path == "/mcp":
		v = cfg.MaxMCPBodyBytes
	default:
		v = cfg.MaxChatBodyBytes
	}
	if v <= 0 {
		v = 32 << 20
	}
	return v
}

// limitRequestBody 是全局请求体闸门。
//
// 已知 ContentLength 时先判断再读，超限直接 413，一个字节都不读；未超限则用
// http.MaxBytesReader 包一层，handler 边读边计数，不会把整个 body 先攒进内存。
// chunked（ContentLength < 0）无法预判，只能有界缓冲到 limit+1 再判 413 ——
// 缓冲上限就是配置的 limit，不会无限增长。
func limitRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil {
			next.ServeHTTP(w, r)
			return
		}
		limit := bodyLimitFor(r.URL.Path)
		if r.ContentLength > limit {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
			return
		}
		if r.ContentLength >= 0 {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
			return
		}
		buf, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
		_ = r.Body.Close()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request body"})
			return
		}
		if int64(len(buf)) > limit {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(buf))
		next.ServeHTTP(w, r)
	})
}

// ─── Client IP ───────────────────────────────────────────────────────────────

// clientIP 返回用于登录限流的客户端标识。
//
// 只有直连对端是本机回环（HA Supervisor / 本地反代）时才采信
// X-Forwarded-For —— 否则 LAN 客户端可以随便伪造头来绕过限流。
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if isLoopbackHost(host) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first := strings.TrimSpace(strings.Split(xff, ",")[0])
			if first != "" {
				return first
			}
		}
		if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); xr != "" {
			return xr
		}
	}
	return host
}

func isLoopbackHost(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
