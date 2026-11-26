package app

import (
	"net/url"
	"regexp"
	"strings"
)

// 日志/错误里的凭据脱敏。
//
// 日志是给面板和运维看的，不是凭据存档。cookie、Bearer token、API key、
// URL 查询参数里的 token/key 都不该原样落到 stderr、环形缓冲区或错误响应里。
// 这些正则覆盖本项目实际会出现的几种形状；宁可多遮一点，也不要漏。

var (
	reAuthHeader = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*bearer\s+)[^\s"',;]+`)
	reAPIKeyHdr  = regexp.MustCompile(`(?i)((?:x-api-key|x-goog-api-key)\s*[:=]\s*)[^\s"',;]+`)
	reCookieHdr  = regexp.MustCompile(`(?i)(cookie\s*[:=]\s*)[^\r\n]+`)
	reSetCookie  = regexp.MustCompile(`(?i)(set-cookie\s*[:=]\s*)[^\r\n]+`)
	reURLSecret  = regexp.MustCompile(`(?i)([?&](?:key|token|api_key|apikey|access_token|auth|sig|signature|password)=)[^&\s"']+`)
	reSkKey      = regexp.MustCompile(`sk-[A-Za-z0-9_\-]{8,}`)
	reGoogleKey  = regexp.MustCompile(`AIza[0-9A-Za-z_\-]{20,}`)
	reCookiePair = regexp.MustCompile(`(?i)(^|[;&\s])((?:SID|HSID|SSID|APISID|SAPISID|OSID|NID|__Secure-1PSID|__Secure-1PSIDTS|__Secure-3PSID|__Secure-3PSIDTS|SIDCC)=)[^;&\s"']+`)
)

func redactSecrets(s string) string {
	if s == "" {
		return s
	}
	s = reAuthHeader.ReplaceAllString(s, "${1}[REDACTED]")
	s = reAPIKeyHdr.ReplaceAllString(s, "${1}[REDACTED]")
	s = reCookieHdr.ReplaceAllString(s, "${1}[REDACTED]")
	s = reSetCookie.ReplaceAllString(s, "${1}[REDACTED]")
	s = reURLSecret.ReplaceAllString(s, "${1}[REDACTED]")
	s = reSkKey.ReplaceAllString(s, "[REDACTED]")
	s = reGoogleKey.ReplaceAllString(s, "[REDACTED]")
	s = reCookiePair.ReplaceAllString(s, "${1}${2}[REDACTED]")
	return s
}

// safeURLForLog 只保留 scheme://host/path，丢掉 userinfo 和查询串。
// 用于把用户提供的图片 URL 写进错误信息：host 足够定位问题，query 里可能有签名。
func safeURLForLog(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "[invalid-url]"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
