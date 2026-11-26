package app

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
)

// CSRF 防护：双提交 token。
//
// 会话 cookie 是 HttpOnly 的，JS 读不到；另发一个非 HttpOnly 的 csrf cookie，
// 前端把它的值放进 X-CSRF-Token 头。攻击者的跨站请求能自动带上 cookie，但
// 读不到 cookie 的值、也没法给跨站请求加自定义头（会触发预检），于是被挡。
//
// 用 Bearer token 认证的请求不走 CSRF：那本来就是显式凭据，不存在"浏览器自动
// 附带"的问题。

const (
	csrfCookieName = "gemini_admin_csrf"
	csrfHeaderName = "X-CSRF-Token"
)

func newCSRFToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败是系统级故障；退化成时间无关的固定值不可接受，
		// 这里返回空串让校验必然失败（fail closed）。
		return ""
	}
	return hex.EncodeToString(b)
}

func setCSRFCookie(w http.ResponseWriter, r *http.Request, token string) {
	if token == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: false, // 前端 JS 必须读得到
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func csrfCookieValue(r *http.Request) string {
	c, err := r.Cookie(csrfCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// csrfOK 校验 header 与 cookie 一致且非空。
func csrfOK(r *http.Request) bool {
	cookie := csrfCookieValue(r)
	header := strings.TrimSpace(r.Header.Get(csrfHeaderName))
	if cookie == "" || header == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookie), []byte(header)) == 1
}

func isSafeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

// requestIsHTTPS 判断这次请求是不是走 HTTPS（含反代/HA ingress 终止 TLS 的情况）。
func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Scheme")), "https")
}

// originSameHost 校验 Origin（或 Referer）与请求 Host / X-Forwarded-Host 一致。
// 仅在 cfg.CSRFStrictOrigin 打开时使用；默认不启用，避免 HA ingress 改写 Host
// 导致面板写操作全挂。双提交 token 本身已经不依赖 Host。
func originSameHost(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		origin = strings.TrimSpace(r.Header.Get("Referer"))
	}
	if origin == "" {
		return true // 非浏览器客户端（curl/脚本）没有这两个头
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	hosts := []string{r.Host}
	if xfh := r.Header.Get("X-Forwarded-Host"); xfh != "" {
		hosts = append(hosts, strings.Split(xfh, ",")...)
	}
	for _, h := range hosts {
		if strings.EqualFold(strings.TrimSpace(h), u.Host) {
			return true
		}
	}
	return false
}
