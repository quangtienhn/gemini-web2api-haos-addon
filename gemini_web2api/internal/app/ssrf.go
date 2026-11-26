package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	tls_client "github.com/bogdanfinn/tls-client"
)

// SSRF 防护（M1 强化版）。
//
// 三道防线，缺一不可：
//  1. URL 层：只允许 http/https、禁止 userinfo、禁止非常用端口、禁止内网主机名。
//  2. DNS 层：解析出的**所有**地址都必须是公网地址（任一命中内网即拒绝）。
//  3. 连接层：net.Dialer.Control 在真正 connect 前再看一次对端 IP —— 这一步
//     防的是 DNS rebinding（校验时解析到公网、连接时解析到内网）。
//
// 重定向：抓取客户端一律不跟随重定向（CheckRedirect 直接返回错误），
// 调用方看到 3xx 就当失败，不会把请求带到未校验的地址。
//
// 已知限制：走 HTTP 代理时，目标域名由代理端解析，连接层的 Control 只能看到
// 代理地址，看不到最终目标。此时仍有第 1、2 道防线（本地解析校验），但无法
// 防住"代理端 DNS 被污染/rebinding"。原型默认不启用代理池。

var blockedCIDRs = []string{
	"0.0.0.0/8",          // 本网络
	"10.0.0.0/8",         // 私有
	"100.64.0.0/10",      // CGNAT
	"127.0.0.0/8",        // 回环
	"169.254.0.0/16",     // link-local / 云元数据
	"172.16.0.0/12",      // 私有
	"192.0.0.0/24",       // IETF 协议分配
	"192.0.2.0/24",       // TEST-NET-1
	"192.88.99.0/24",     // 6to4 relay
	"192.168.0.0/16",     // 私有
	"198.18.0.0/15",      // 基准测试
	"198.51.100.0/24",    // TEST-NET-2
	"203.0.113.0/24",     // TEST-NET-3
	"224.0.0.0/4",        // 组播
	"240.0.0.0/4",        // 保留
	"255.255.255.255/32", // 广播
	"::/128",             // 未指定
	"::1/128",            // 回环
	"fc00::/7",           // 唯一本地
	"fe80::/10",          // link-local
	"ff00::/8",           // 组播
	"2001:db8::/32",      // 文档
	"2001:10::/28",       // ORCHID
	"64:ff9b::/96",       // NAT64
}

var blockedNets = func() []*net.IPNet {
	out := make([]*net.IPNet, 0, len(blockedCIDRs))
	for _, s := range blockedCIDRs {
		if _, n, err := net.ParseCIDR(s); err == nil {
			out = append(out, n)
		}
	}
	return out
}()

var blockedHostSuffixes = []string{
	".localhost",
	".local",
	".internal",
	".home.arpa",
	".lan",
}

var blockedHostNames = map[string]bool{
	"localhost":               true,
	"metadata":                true,
	"metadata.google.internal": true,
	"instance-data":           true,
}

// isBlockedIP 判断地址是否禁止访问（回环/私有/link-local/组播/保留）。
func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() {
		return true
	}
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// isAllowedMediaRedirectHost 限制媒体下载链的重定向目标。
// 只允许 Google 的媒体/产物域，防止上游返回恶意 Location 把带 cookie 的请求引到别处。
func isAllowedMediaRedirectHost(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if h == "" {
		return false
	}
	allowed := []string{
		"gemini.google.com",
		"google.com",
		"googleusercontent.com",
		"gstatic.com",
		"googleapis.com",
		"ggpht.com",
	}
	for _, a := range allowed {
		if h == a || strings.HasSuffix(h, "."+a) {
			return true
		}
	}
	return false
}

func blockedHost(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if blockedHostNames[h] {
		return true
	}
	for _, suf := range blockedHostSuffixes {
		if strings.HasSuffix(h, suf) {
			return true
		}
	}
	return false
}

// resolveAndValidate 解析主机名并校验所有地址。IP 字面量不查 DNS。
func resolveAndValidate(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return nil, fmt.Errorf("address not allowed: %s", ip)
		}
		return []net.IP{ip}, nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("dns lookup failed for %s: %w", host, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no addresses for host %s", host)
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		if isBlockedIP(a.IP) {
			return nil, fmt.Errorf("resolved address not allowed: %s", a.IP)
		}
		ips = append(ips, a.IP)
	}
	return ips, nil
}

// validateRemoteURL 校验一个用户提供的 URL 是否可安全抓取。
func validateRemoteURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme %q (only http/https)", u.Scheme)
	}
	if u.User != nil {
		return fmt.Errorf("url must not contain userinfo")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("missing host")
	}
	if blockedHost(host) {
		return fmt.Errorf("host not allowed: %s", host)
	}
	if p := u.Port(); p != "" && p != "80" && p != "443" {
		return fmt.Errorf("port %s not allowed (only 80/443)", p)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := resolveAndValidate(ctx, host); err != nil {
		return err
	}
	return nil
}

// ─── Guarded dialer ──────────────────────────────────────────────────────────

// dialControl 在 connect 前检查对端 IP。allow 里的地址放行（代理出口）。
func dialControl(allow map[string]bool) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("ssrf: bad dial address")
		}
		if allow[host] {
			return nil
		}
		ip := net.ParseIP(host)
		if ip == nil || isBlockedIP(ip) {
			return fmt.Errorf("ssrf: dial to blocked address %s", host)
		}
		return nil
	}
}

func guardedDialer(allow map[string]bool) *net.Dialer {
	return &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   dialControl(allow),
	}
}

// resolveAllowSet 把代理主机名解析成 IP 集合，供连接层放行。
func resolveAllowSet(host string) map[string]bool {
	out := map[string]bool{}
	if host == "" {
		return out
	}
	if ip := net.ParseIP(host); ip != nil {
		out[ip.String()] = true
		return out
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host); err == nil {
		for _, a := range addrs {
			out[a.IP.String()] = true
		}
	}
	return out
}

// ─── Fetch clients ───────────────────────────────────────────────────────────

const fetchTimeout = 30 * time.Second

func noRedirect(*http.Request, []*http.Request) error {
	return errors.New("redirect not allowed")
}

// newSafeHTTPClient 返回用于抓取用户 URL 的 stdlib 客户端。
// proxyURL 为空时走直连（连接层受 Control 保护）。
func newSafeHTTPClient(proxyURL string) *http.Client {
	t := &http.Transport{
		MaxIdleConns:        16,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     30 * time.Second,
		ForceAttemptHTTP2:   false,
	}
	if proxyURL == "" {
		t.DialContext = guardedDialer(nil).DialContext
	} else if u, err := url.Parse(proxyURL); err == nil {
		t.Proxy = http.ProxyURL(u)
		// 代理出口本身可能在 LAN；只放行它解析出的地址，其它目标仍受限。
		t.DialContext = guardedDialer(resolveAllowSet(u.Hostname())).DialContext
	} else {
		t.DialContext = guardedDialer(nil).DialContext
	}
	return &http.Client{
		Timeout:       fetchTimeout,
		Transport:     t,
		CheckRedirect: noRedirect,
	}
}

var (
	fetchTLSOnce   sync.Once
	fetchTLSClient tls_client.HttpClient
	fetchTLSErr    error
)

// getFetchTLSClient 返回直连抓取用的 tls-client（带连接层 SSRF 保护）。
func getFetchTLSClient() (tls_client.HttpClient, error) {
	fetchTLSOnce.Do(func() {
		opts := []tls_client.HttpClientOption{
			tls_client.WithTimeoutSeconds(int(fetchTimeout.Seconds())),
			tls_client.WithClientProfile(resolveProfile(effectiveImpersonate())),
			tls_client.WithNotFollowRedirects(),
			tls_client.WithDialer(*guardedDialer(nil)),
		}
		c, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), opts...)
		if err != nil {
			fetchTLSErr = err
			return
		}
		fetchTLSClient = c
	})
	return fetchTLSClient, fetchTLSErr
}


// isPrivateIP 保留旧名字，供既有测试使用。
func isPrivateIP(ip net.IP) bool { return isBlockedIP(ip) }
