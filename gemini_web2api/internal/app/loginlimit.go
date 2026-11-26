package app

import (
	"sync"
	"time"
)

// 管理端登录限流：按客户端 IP 统计窗口期内的失败次数，超限后锁定到窗口结束。
//
// 目的不是防分布式撞库（那需要上游/WAF），而是挡住单机暴力猜 admin token。
// 成功登录会清零该 IP 的计数。

type loginAttempt struct {
	failures    []int64 // unix 秒
	lockedUntil int64
}

type loginLimiter struct {
	mu   sync.Mutex
	byIP map[string]*loginAttempt
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{byIP: map[string]*loginAttempt{}}
}

var loginGuard = newLoginLimiter()

// allow 返回是否放行；被锁时第二个返回值是剩余秒数。
func (l *loginLimiter) allow(ip string, now time.Time, limit int, window time.Duration) (bool, int) {
	if limit <= 0 {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.byIP[ip]
	if a == nil {
		return true, 0
	}
	nowSec := now.Unix()
	if a.lockedUntil > nowSec {
		return false, int(a.lockedUntil - nowSec)
	}
	return true, 0
}

// fail 记一次失败；达到上限时锁定一个窗口。
func (l *loginLimiter) fail(ip string, now time.Time, limit int, window time.Duration) {
	if limit <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.byIP[ip]
	if a == nil {
		a = &loginAttempt{}
		l.byIP[ip] = a
	}
	nowSec := now.Unix()
	cut := nowSec - int64(window.Seconds())
	a.failures = pruneTimestamps(a.failures, cut)
	a.failures = append(a.failures, nowSec)
	if len(a.failures) >= limit {
		a.lockedUntil = nowSec + int64(window.Seconds())
		a.failures = nil
	}
}

// success 清掉该 IP 的失败记录。
func (l *loginLimiter) success(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.byIP, ip)
}

// resetLoginLimiter 只给测试用。
func resetLoginLimiter() {
	loginGuard = newLoginLimiter()
}
