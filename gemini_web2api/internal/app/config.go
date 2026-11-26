package app

import (
	"encoding/json"
	"os"
	"sync"
)

type Config struct {
	Port           int    `json:"port"`
	Host           string `json:"host"`
	RetryAttempts  int    `json:"retry_attempts"`
	RetryDelaySec  int    `json:"retry_delay_sec"`
	RequestTimeout int    `json:"request_timeout_sec"`
	RetryOnTimeout bool   `json:"retry_on_timeout"`
	GeminiBL       string `json:"gemini_bl"`
	DefaultModel   string `json:"default_model"`
	LogRequests    bool   `json:"log_requests"`
	CookieFile     string `json:"cookie_file"`
	Proxy          string `json:"proxy"` // 只用于启动时播种代理池，见 seedProxiesFromConfig
	Impersonate    string `json:"impersonate"`
	DBPath         string `json:"db_path"`
	AdminToken     string `json:"admin_token"`
	AdminEnabled   bool   `json:"admin_enabled"`
	RetentionDays  int    `json:"retention_days"`

	// Per-IP rate limit (一个 slot = 直连/或一个代理),0 表示不限。
	// 默认值取实测区间 80-180 的下沿，详见 ratelimit.go 的说明。
	PerIPConcurrent int `json:"per_ip_concurrent"` // 瞬时并发上限
	PerIPRPM        int `json:"per_ip_rpm"`        // 每分钟请求上限
	PerIPRPH        int `json:"per_ip_rph"`        // 每小时请求上限

	// 代理连续失败熔断后隔多久放回池子（分钟），0 = 不恢复。
	ProxyCooldownMin int `json:"proxy_cooldown_min"`
	// 代理池无可用出口时是否退回直连。默认 false。
	FallbackDirect bool `json:"fallback_direct"`
	// 账号绑定的代理不可用时是否只换号、不换出口。默认 false。
	StrictProxyBinding bool `json:"strict_proxy_binding"`
	// cookie 失效时是否降级成匿名继续跑。默认 false。
	FallbackAnon bool `json:"fallback_anon"`
	// 是否自动从 /app 页面抓最新 bl 版本号。默认 true。
	GeminiBLAuto bool `json:"gemini_bl_auto"`
	// 单次请求 prompt 的 UTF-8 字节上限，0 = 不限。
	MaxPromptBytes int `json:"max_prompt_bytes"`
	// 是否走 Gemini 原生 conversation_id 服务端多轮。默认 false，见 conversation.go。
	MultiTurn bool `json:"multi_turn"`
	// 出完结果是否自动删掉 gemini.google.com 上留下的这条会话（#19，rpc GzXR5e）。
	// 只登录态生效（删除要 XSRF）；异步 best-effort，删失败只记日志不影响响应。默认 false。
	AutoDeleteConversation bool `json:"auto_delete_conversation"`
	// 匿名优先（#20）：这次请求不需要登录态能力（纯文本、非思考、无工具、无图）时
	// 不占用 cookie 账号，走匿名省额度；需要登录才挑号。默认 false。见 modelNeedsLogin。
	AnonFirst bool `json:"anon_first"`
	// 临时对话（#34）：发请求时塞 inner[45]=1，走 Gemini 网页端原生「临时对话」——
	// 会话不进账号的会话列表、不用于训练，天然取代 auto_delete_conversation（无需再发删除
	// RPC）。只登录态有意义（匿名本就不落账号）。与 multi_turn 冲突：multi_turn 开着时不发，
	// 见 temporaryChatEligible。默认 false。
	TemporaryChat bool `json:"temporary_chat"`
	// 生成图放大（#62）：出图后照网页「下载原图」那样发 rpc c8o8Fe，返回放大版（实测宽高各
	// 2 倍、JPEG）。要登录态；放大失败退回原来那张，不重新生成。默认 false。
	ImageUpscale bool `json:"image_upscale"`
	// 新协议（#43）：对话改走 processSession（WebChannel）而不是老的 StreamGenerate。后端是同一个，
	// 只换传输层；老端点还能用，默认 false。多轮续接仍走老端点，见 webchannel.go。
	WebChannel bool `json:"web_channel"`
	// FirefoxFingerprint 是「浏览器指纹」实验开关（#40 之上）：开 → 强制走 Firefox/Zen 指纹
	// （TLS profile Firefox_147 + Firefox UA + 去掉 Chromium 专有的 Sec-CH-UA）；关 → 用
	// Impersonate（默认 Chrome）。直连才是完整 Firefox 指纹，走代理时 TLS 这层仍是 Go 标准库
	// 的（隧道限制，改不了）。默认 false。见 client.go 的 isFirefoxProfile / effectiveImpersonate。
	FirefoxFingerprint bool `json:"firefox_fingerprint"`
	// 严格模型校验（#41）：服务端自报的模型跟请求的不一致（额度用完被静默降级）时
	// 不把降级回答当成功，换号重试，全池降级就回 429。默认 false。见 strict_model.go。
	StrictModel bool `json:"strict_model"`
	// 被降级的号在该模型上冷却多久（分钟），0 = 不记。网页端用量按 5 小时滚动窗算，默认 300。
	StrictModelCooldownMin int `json:"strict_model_cooldown_min"`
	// 流式保活间隔（秒），0 = 关（默认）。见 keepalive.go。
	StreamKeepaliveSec int `json:"stream_keepalive_sec"`
	// 空闲超时（秒，#81）：上游开流后连续这么多秒一个 chunk 都不来就掐断、当瞬时错误
	// 重试。0 = 关（默认）。总超时（request_timeout_sec）管不到"首字节到了但之后挂着"，
	// 这个补那个缺口。只老协议（StreamGenerate）生效，见 gemini.go 的 readBodyWithIdle。
	StreamIdleTimeoutSec int `json:"stream_idle_timeout_sec"`

	// ─── Security hardening (M1) ──────────────────────────────────────────
	// 这些是部署期配置（改了要重启），跟 runtime_config 里的调优参数分开。

	// CORSAllowOrigins 是允许跨域访问 API 的 Origin 白名单。
	// 空 = 不发 Access-Control-Allow-Origin（默认，最安全）。
	// 填 "*" = 通配（仅在你明确需要浏览器直连 API 时使用）。
	CORSAllowOrigins []string `json:"cors_allow_origins"`
	// CSRFStrictOrigin 为 true 时，管理端写操作还会校验 Origin/Referer 与
	// Host / X-Forwarded-Host 一致。默认 false：HA ingress 的 Host 改写行为
	// 需要在真实环境验证，双提交 token 已经足够挡住跨站请求。
	CSRFStrictOrigin bool `json:"csrf_strict_origin"`
	// LoginRateLimit 是管理端登录在窗口期内允许的失败次数，默认 5。0 = 关闭（不推荐）。
	LoginRateLimit int `json:"login_rate_limit"`
	// LoginRateWindowSec 是登录失败计数窗口，默认 900 秒。
	LoginRateWindowSec int `json:"login_rate_window_sec"`

	// 请求体上限（字节）。按端点分组，避免用一个值同时卡住文本和视频。
	MaxChatBodyBytes  int64 `json:"max_chat_body_bytes"`  // /v1/chat/completions, /v1/responses, /v1beta
	MaxVideoBodyBytes int64 `json:"max_video_body_bytes"` // /v1/videos
	MaxAdminBodyBytes int64 `json:"max_admin_body_bytes"` // /admin/api
	MaxMCPBodyBytes   int64 `json:"max_mcp_body_bytes"`   // /mcp
	// MaxMediaDownloadBytes 限制从上游下载的图片/视频体积，默认 256 MiB。
	MaxMediaDownloadBytes int64 `json:"max_media_download_bytes"`
	// MaxTotalUploadBytes 限制单次请求里所有内联附件的总字节数，默认 64 MiB。
	MaxTotalUploadBytes int64 `json:"max_total_upload_bytes"`

	// DisableURLFetch 禁止 Vision 从 http(s) URL 拉取图片（data: URL 不受影响）。
	// 默认 false；原型/未验证 SSRF 防护的部署建议 true。
	DisableURLFetch bool `json:"disable_url_fetch"`
	// DisableMediaGeneration 禁止图片/音乐/视频生成（账号池额度与上游风险控制）。
	DisableMediaGeneration bool `json:"disable_media_generation"`
	// DisableAccountPool 禁止新增/挑选 Google 账号（匿名模式）。已有账号行保留。
	DisableAccountPool bool `json:"disable_account_pool"`
	// DisableProxyPool 禁止新增/挑选代理（直连模式）。已有代理行保留。
	DisableProxyPool bool `json:"disable_proxy_pool"`
}

var (
	cfg     Config
	cfgOnce sync.Once
)

func defaultConfig() Config {
	return Config{
		Port:           8083,
		Host:           "0.0.0.0",
		RetryAttempts:  3,
		RetryDelaySec:  2,
		RequestTimeout: 180,
		RetryOnTimeout: true,
		GeminiBL:       "boq_gemini-web-uiserver_20261007.01_p0",
		DefaultModel:   "gemini-3.6-flash",
		LogRequests:    true,
		CookieFile:     "",
		Proxy:          "",
		Impersonate:    "chrome_146",
		DBPath:         "./data/gemini.db",
		AdminToken:     "",
		AdminEnabled:   true,
		RetentionDays:  30,
		// 单出口实测 80-180 次不等（连接策略和出口质量决定），静态 IP 上 188。
		// RPH 取下沿 80 保守留量；按低速率跑的部署可以调高很多 ——
		// 10 次/分钟连打 800 次、跨 110 分钟一次没被拦。
		PerIPConcurrent: 5,
		PerIPRPM:        30,
		PerIPRPH:        80,
		// 实测被拦的出口 106-121 分钟自动恢复，冷却取 120 分钟。
		ProxyCooldownMin: 120,
		FallbackDirect:   false,
		FallbackAnon:     false,
		GeminiBLAuto:     true,
		// 实测上游的墙在约 13 万 UTF-8 字节：129,950 字节中英文各 3/3 过，
		// 135,990 字节各 1/3，141,920 字节各 1/3。取 128000 留一点余量。
		MaxPromptBytes:         128000,
		MultiTurn:              false,
		AutoDeleteConversation: false,
		AnonFirst:              false,
		TemporaryChat:          false,
		ImageUpscale:           false,
		WebChannel:             false,
		FirefoxFingerprint:     false,
		StrictModel:            false,
		StrictModelCooldownMin: 300,

		// Security defaults (M1)
		CORSAllowOrigins:       nil,
		CSRFStrictOrigin:       false,
		LoginRateLimit:         5,
		LoginRateWindowSec:     900,
		MaxChatBodyBytes:       32 << 20,  // 32 MiB
		MaxVideoBodyBytes:      96 << 20,  // 96 MiB
		MaxAdminBodyBytes:      8 << 20,   // 8 MiB
		MaxMCPBodyBytes:        8 << 20,   // 8 MiB
		MaxMediaDownloadBytes:  256 << 20, // 256 MiB
		MaxTotalUploadBytes:    64 << 20,  // 64 MiB
		DisableURLFetch:        false,
		DisableMediaGeneration: false,
		DisableAccountPool:     false,
		DisableProxyPool:       false,
	}
}

func loadConfig(path string) error {
	cfg = defaultConfig()
	if path == "" {
		for _, p := range []string{"./config.json", os.ExpandEnv("$HOME/.config/gemini-web2api/config.json")} {
			if _, err := os.Stat(p); err == nil {
				path = p
				break
			}
		}
	}
	if path == "" {
		normalizeSecurityConfig(&cfg)
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	normalizeSecurityConfig(&cfg)
	return nil
}

// normalizeSecurityConfig 把 0/负数恢复成安全默认值。
// 请求体上限为 0 会让所有请求都 413（或完全放开，取决于实现），必须兜底。
func normalizeSecurityConfig(c *Config) {
	if c.MaxChatBodyBytes <= 0 {
		c.MaxChatBodyBytes = 32 << 20
	}
	if c.MaxVideoBodyBytes <= 0 {
		c.MaxVideoBodyBytes = 96 << 20
	}
	if c.MaxAdminBodyBytes <= 0 {
		c.MaxAdminBodyBytes = 8 << 20
	}
	if c.MaxMCPBodyBytes <= 0 {
		c.MaxMCPBodyBytes = 8 << 20
	}
	if c.MaxMediaDownloadBytes <= 0 {
		c.MaxMediaDownloadBytes = 256 << 20
	}
	if c.MaxTotalUploadBytes <= 0 {
		c.MaxTotalUploadBytes = 64 << 20
	}
	if c.LoginRateLimit < 0 {
		c.LoginRateLimit = 0
	}
	if c.LoginRateWindowSec <= 0 {
		c.LoginRateWindowSec = 900
	}
}
