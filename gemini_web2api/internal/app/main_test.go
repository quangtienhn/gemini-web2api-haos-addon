package app

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain 把 DB 指向临时目录再跑测试。
//
// hasCookie() 现在只看 cookie 池，也就是要查 DB；不接管 cfg.DBPath 的话
// getDB() 会去开真实的 ./data/gemini.db，测试就会读写生产数据。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gw2a-test-")
	if err != nil {
		panic(err)
	}
	cfg.DBPath = filepath.Join(dir, "test.db")
	// Security defaults mirror loadConfig + normalizeSecurityConfig so that
	// handlers using cfg.Max*BodyBytes / cfg.MaxMediaDownloadBytes behave.
	cfg.MaxChatBodyBytes = 32 << 20
	cfg.MaxVideoBodyBytes = 96 << 20
	cfg.MaxAdminBodyBytes = 8 << 20
	cfg.MaxMCPBodyBytes = 8 << 20
	cfg.MaxMediaDownloadBytes = 256 << 20
	cfg.MaxTotalUploadBytes = 64 << 20
	cfg.LoginRateLimit = 5
	cfg.LoginRateWindowSec = 900
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// withPoolCookie 往 cookie 池塞一条测试账号，用完自动删掉。
// 取代原来的 cookieRuntime.Store —— 那条单 cookie 路径已经取消。
func withPoolCookie(t *testing.T) {
	t.Helper()
	id, err := accountAdd("test", "SAPISID=dummy; SID=x", "")
	if err != nil {
		t.Fatalf("插测试 cookie 失败: %v", err)
	}
	t.Cleanup(func() { _ = accountDelete(id) })
}
