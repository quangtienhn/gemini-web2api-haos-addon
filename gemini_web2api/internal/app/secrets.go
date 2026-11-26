package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// 账号 cookie 的静态加密（AES-256-GCM）。
//
// 存储格式：enc:v1:<base64(nonce||ciphertext)>
// 没有前缀的值按明文处理（兼容老库）；读到时原样返回，下次写回时自动加密。
//
// 密钥来源（按优先级）：
//  1. 环境变量 GEMINI_DB_KEY（32 字节原始值 / base64 32 字节 / 任意口令，取 SHA-256）
//  2. <db 目录>/secret.key（不存在则生成 32 字节随机值，权限 0600）
//
// 拿不到密钥时不会静默降级成明文写库：encryptSecret 返回空串，调用方应拒绝
// 写入；decryptSecret 对带前缀的值返回空串（该账号本次不可用），并记日志。

const encPrefix = "enc:v1:"

var (
	cookieCipherOnce sync.Once
	cookieCipherAEAD cipher.AEAD
	cookieCipherErr  error
)

func ensureCookieCipher() error {
	cookieCipherOnce.Do(func() {
		key, err := loadCookieCipherKey()
		if err != nil {
			cookieCipherErr = err
			return
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			cookieCipherErr = err
			return
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			cookieCipherErr = err
			return
		}
		cookieCipherAEAD = aead
	})
	return cookieCipherErr
}

func loadCookieCipherKey() ([]byte, error) {
	if v := strings.TrimSpace(os.Getenv("GEMINI_DB_KEY")); v != "" {
		if raw, err := base64.StdEncoding.DecodeString(v); err == nil && len(raw) == 32 {
			return raw, nil
		}
		if len(v) == 32 {
			return []byte(v), nil
		}
		sum := sha256.Sum256([]byte(v))
		return sum[:], nil
	}
	dir := filepath.Dir(cfg.DBPath)
	if dir == "" || dir == "." {
		dir = "./data"
	}
	path := filepath.Join(dir, "secret.key")
	if data, err := os.ReadFile(path); err == nil {
		if len(data) == 32 {
			return data, nil
		}
		sum := sha256.Sum256(data)
		return sum[:], nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("cookie cipher: mkdir %s: %w", dir, err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("cookie cipher: rand: %w", err)
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, fmt.Errorf("cookie cipher: write %s: %w", path, err)
	}
	return key, nil
}

// encryptSecret 加密一个值。空串原样返回；密钥不可用时返回空串（调用方拒绝写）。
func encryptSecret(plain string) string {
	if plain == "" {
		return ""
	}
	if strings.HasPrefix(plain, encPrefix) {
		return plain // 已加密
	}
	if err := ensureCookieCipher(); err != nil || cookieCipherAEAD == nil {
		logf("[security] cookie 加密不可用，拒绝写入明文: %v", cookieCipherErr)
		return ""
	}
	nonce := make([]byte, cookieCipherAEAD.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		logf("[security] cookie 加密随机数失败: %v", err)
		return ""
	}
	ct := cookieCipherAEAD.Seal(nil, nonce, []byte(plain), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(append(nonce, ct...))
}

// decryptSecret 解密一个值。没有前缀的按明文（老库）返回。
func decryptSecret(stored string) string {
	if stored == "" {
		return ""
	}
	if !strings.HasPrefix(stored, encPrefix) {
		return stored
	}
	if err := ensureCookieCipher(); err != nil || cookieCipherAEAD == nil {
		logf("[security] cookie 解密不可用: %v", cookieCipherErr)
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, encPrefix))
	if err != nil {
		logf("[security] cookie 密文 base64 解析失败")
		return ""
	}
	ns := cookieCipherAEAD.NonceSize()
	if len(raw) <= ns {
		logf("[security] cookie 密文长度非法")
		return ""
	}
	pt, err := cookieCipherAEAD.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		logf("[security] cookie 解密失败（密钥不匹配？）")
		return ""
	}
	return string(pt)
}

// migrateCookieEncryption 把老库里的明文 cookie 一次性加密。
// 任何一条失败只记日志、继续处理其它行（不阻塞启动）。
func migrateCookieEncryption() {
	if err := ensureCookieCipher(); err != nil {
		logf("[security] cookie 加密初始化失败，跳过迁移: %v", err)
		return
	}
	rows, err := getDB().Query(`SELECT id, cookie FROM accounts`)
	if err != nil {
		return
	}
	type row struct {
		id     int64
		cookie string
	}
	var todo []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.cookie); err != nil {
			continue
		}
		if r.cookie != "" && !strings.HasPrefix(r.cookie, encPrefix) {
			todo = append(todo, r)
		}
	}
	rows.Close()
	for _, r := range todo {
		enc := encryptSecret(r.cookie)
		if enc == "" {
			return
		}
		if _, err := getDB().Exec(`UPDATE accounts SET cookie=? WHERE id=?`, enc, r.id); err != nil {
			logf("[security] 账号 #%d cookie 加密写回失败: %v", r.id, err)
		}
	}
	if len(todo) > 0 {
		logf("[security] 已加密 %d 条账号 cookie", len(todo))
	}
}
