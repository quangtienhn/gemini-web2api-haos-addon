package app

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Admin password storage uses a single authoritative state file:
//   /data/admin_auth.json  {"v":1,"locked":true,"hash":"$argon2id$..."}
// This eliminates multi-file tx/lock/recovery marker consistency problems.
// If the state file exists but is unreadable/invalid/unknown => fail closed.
// Legacy hash+lock files are migrated on init.

const adminPasswordMinLen = 8

const (
	argon2SupportedVersion   = 19
	argon2MinMemory          = 8 * 1024
	argon2MaxMemory          = 128 * 1024
	argon2MinTime            = 1
	argon2MaxTime            = 10
	argon2MinParallelism     = 1
	argon2MaxParallelism     = 4
	argon2MinSaltLen         = 16
	argon2MaxSaltLen         = 32
	argon2MinHashLen         = 16
	argon2MaxHashLen         = 64
	argon2DefaultMemory      = 64 * 1024
	argon2DefaultTime        = 3
	argon2DefaultParallelism = 4
	argon2DefaultSaltLen     = 16
	argon2DefaultHashLen     = 32
)

type phcParams struct {
	memory      uint32
	time        uint32
	parallelism uint8
	salt        []byte
	hash        []byte
}

type adminAuthState struct {
	V      int    `json:"v"`
	Locked bool   `json:"locked"`
	Hash   string `json:"hash,omitempty"`
}

func adminDataDir() string {
	dir := filepath.Dir(cfg.DBPath)
	if dir == "" || dir == "." {
		dir = "./data"
	}
	return dir
}

func adminAuthStatePath() string     { return filepath.Join(adminDataDir(), "admin_auth.json") }
func adminPasswordHashPath() string  { return filepath.Join(adminDataDir(), "admin_password.hash") } // legacy
func adminPasswordLockPath() string  { return filepath.Join(adminDataDir(), "admin_password.lock") } // legacy
func adminPasswordResetPath() string { return filepath.Join(adminDataDir(), "admin_password.reset") }
func adminPasswordRecoveryPath() string {
	return filepath.Join(adminDataDir(), "admin_password.recovery_required")
}                                 // legacy
func adminPasswordTxPath() string { return filepath.Join(adminDataDir(), "admin_password.tx") } // legacy

// --- PHC parsing/verification ---

func parsePHCHash(s string) (*phcParams, string) {
	if strings.TrimSpace(s) == "" {
		return nil, "empty"
	}
	parts := strings.Split(s, "$")
	if len(parts) != 6 || parts[0] != "" {
		return nil, "corrupt"
	}
	algo, verPart, paramPart, saltB64, hashB64 := parts[1], parts[2], parts[3], parts[4], parts[5]
	if algo == "" || verPart == "" || paramPart == "" || saltB64 == "" || hashB64 == "" {
		return nil, "corrupt"
	}
	if algo != "argon2id" {
		if algo == "argon2i" || algo == "argon2d" {
			return nil, "unsupported"
		}
		return nil, "corrupt"
	}
	if !strings.HasPrefix(verPart, "v=") {
		return nil, "corrupt"
	}
	verStr := strings.TrimPrefix(verPart, "v=")
	if verStr == "" || len(verStr) > 10 {
		return nil, "corrupt"
	}
	ver, err := strconv.Atoi(verStr)
	if err != nil || ver <= 0 {
		return nil, "corrupt"
	}
	if ver != argon2SupportedVersion {
		return nil, "unsupported"
	}
	params, ok := parsePHCParams(paramPart)
	if !ok {
		return nil, "corrupt"
	}
	if params.memory < argon2MinMemory || params.memory > argon2MaxMemory ||
		params.time < argon2MinTime || params.time > argon2MaxTime ||
		params.parallelism < argon2MinParallelism || params.parallelism > argon2MaxParallelism {
		return nil, "unsupported"
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(saltB64)
	if err != nil {
		return nil, "corrupt"
	}
	h, err := base64.RawStdEncoding.Strict().DecodeString(hashB64)
	if err != nil {
		return nil, "corrupt"
	}
	if len(salt) < argon2MinSaltLen || len(salt) > argon2MaxSaltLen {
		return nil, "unsupported"
	}
	if len(h) < argon2MinHashLen || len(h) > argon2MaxHashLen {
		return nil, "unsupported"
	}
	params.salt = salt
	params.hash = h
	return params, "valid"
}

func parsePHCParams(s string) (*phcParams, bool) {
	if s == "" {
		return nil, false
	}
	seen := map[string]bool{}
	var m, t uint32
	var p uint8
	var hasM, hasT, hasP bool
	for _, part := range strings.Split(s, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 || kv[0] == "" || kv[1] == "" || seen[kv[0]] {
			return nil, false
		}
		seen[kv[0]] = true
		switch kv[0] {
		case "m":
			n, err := strconv.ParseUint(kv[1], 10, 32)
			if err != nil || n == 0 {
				return nil, false
			}
			m, hasM = uint32(n), true
		case "t":
			n, err := strconv.ParseUint(kv[1], 10, 32)
			if err != nil || n == 0 {
				return nil, false
			}
			t, hasT = uint32(n), true
		case "p":
			n, err := strconv.ParseUint(kv[1], 10, 8)
			if err != nil || n == 0 {
				return nil, false
			}
			p, hasP = uint8(n), true
		default:
			return nil, false
		}
	}
	if !hasM || !hasT || !hasP {
		return nil, false
	}
	return &phcParams{memory: m, time: t, parallelism: p}, true
}

func isValidAdminPasswordHash(s string) bool {
	_, status := parsePHCHash(strings.TrimSpace(s))
	return status == "valid"
}

func argon2idHash(password string) (string, error) {
	salt := make([]byte, argon2DefaultSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	h := argon2.IDKey([]byte(password), salt, argon2DefaultTime, argon2DefaultMemory, argon2DefaultParallelism, argon2DefaultHashLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2SupportedVersion,
		argon2DefaultMemory, argon2DefaultTime, argon2DefaultParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(h)), nil
}

func verifyArgon2id(password, encoded string) bool {
	params, status := parsePHCHash(strings.TrimSpace(encoded))
	if status != "valid" || params == nil {
		return false
	}
	got := argon2.IDKey([]byte(password), params.salt, params.time, params.memory, params.parallelism, uint32(len(params.hash)))
	if len(got) != len(params.hash) {
		return false
	}
	var diff byte
	for i := range got {
		diff |= got[i] ^ params.hash[i]
	}
	return diff == 0
}

// --- Atomic state file ---

func syncDirOS(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Sync(); err != nil {
		if runtime.GOOS == "windows" {
			return nil
		}
		return err
	}
	return nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = tmp.Close(); _ = os.Remove(tmpName) }
	if err := tmp.Chmod(perm); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := syncDirOS(dir); err != nil {
		return err
	}
	return nil
}

func loadAdminAuthState() (*adminAuthState, string, error) {
	data, err := os.ReadFile(adminAuthStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "missing", nil
		}
		return nil, "unreadable", err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, "empty", nil
	}
	var st adminAuthState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, "corrupt", err
	}
	if st.V != 1 {
		return nil, "unsupported", fmt.Errorf("unsupported state version %d", st.V)
	}
	if !st.Locked {
		// Unlocked state is only valid as explicit non-password mode; we don't write it normally.
		return &st, "valid", nil
	}
	// Locked: hash must be valid.
	if strings.TrimSpace(st.Hash) == "" {
		return &st, "corrupt", fmt.Errorf("locked state missing hash")
	}
	if _, hs := parsePHCHash(strings.TrimSpace(st.Hash)); hs != "valid" {
		if hs == "unsupported" {
			return &st, "unsupported", fmt.Errorf("unsupported hash")
		}
		return &st, "corrupt", fmt.Errorf("invalid hash")
	}
	return &st, "valid", nil
}

var adminAuthWriteFile = writeFileAtomic

func saveAdminAuthState(st *adminAuthState) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return adminAuthWriteFile(adminAuthStatePath(), b, 0o600)
}

// --- Legacy migration ---

func writeFailClosedState(reason string) error {
	if err := saveAdminAuthState(&adminAuthState{V: 1, Locked: true, Hash: ""}); err != nil {
		logf("[auth] CRITICAL: failed to write fail-closed auth state (%s): %v", reason, err)
		return err
	}
	logf("[auth] wrote fail-closed auth state: %s", reason)
	return nil
}

func migrateLegacyAuth() {
	statePath := adminAuthStatePath()
	if _, err := os.Stat(statePath); err == nil {
		return // new state already exists
	} else if !os.IsNotExist(err) {
		// Cannot determine state file existence - fail closed.
		if werr := writeFailClosedState("state stat unknown"); werr != nil {
			logf("[auth] migration aborted; passwordModeLocked will fail closed")
		}
		return
	}

	legacyHashPath := adminPasswordHashPath()
	legacyLockPath := adminPasswordLockPath()
	legacyTxPath := adminPasswordTxPath()
	legacyRecoveryPath := adminPasswordRecoveryPath()

	lockExists, lockUnknown := statExistsUnknown(legacyLockPath)
	txExists, txUnknown := statExistsUnknown(legacyTxPath)
	recoveryExists, recoveryUnknown := statExistsUnknown(legacyRecoveryPath)

	hashData, hashErr := os.ReadFile(legacyHashPath)
	hashExists := hashErr == nil
	hashMissing := os.IsNotExist(hashErr)
	hashUnknown := hashErr != nil && !hashMissing

	// Any unknown legacy state => fail closed.
	if lockUnknown || txUnknown || recoveryUnknown || hashUnknown {
		_ = writeFailClosedState("legacy state unknown")
		return
	}

	// Legacy tx/recovery markers indicate unfinished or recovery-required state.
	if txExists || recoveryExists {
		_ = writeFailClosedState("legacy tx/recovery marker present")
		return
	}

	if hashExists {
		trimmed := strings.TrimSpace(string(hashData))
		if _, hs := parsePHCHash(trimmed); hs == "valid" {
			if err := saveAdminAuthState(&adminAuthState{V: 1, Locked: true, Hash: trimmed}); err != nil {
				logf("[auth] CRITICAL: failed to migrate valid legacy hash: %v", err)
				_ = writeFailClosedState("legacy migration write failed")
				return
			}
			logf("[auth] migrated legacy password hash to admin_auth.json")
			return
		}
		// Hash exists but invalid/corrupt/unsupported/empty => fail closed.
		_ = writeFailClosedState("legacy hash invalid")
		return
	}

	if lockExists {
		_ = writeFailClosedState("legacy lock exists without hash")
		return
	}

	// No legacy artifacts confirmed missing.
	_ = hashMissing
	// No legacy state - nothing to migrate.
}

func statExistsUnknown(path string) (exists bool, unknown bool) {
	_, err := os.Stat(path)
	if err == nil {
		return true, false
	}
	if os.IsNotExist(err) {
		return false, false
	}
	return false, true
}

// --- Public auth API ---

func passwordHashStatus() (string, error) {
	st, status, err := loadAdminAuthState()
	if status == "missing" {
		// Legacy compat: check old hash file
		data, rerr := os.ReadFile(adminPasswordHashPath())
		if rerr != nil {
			if os.IsNotExist(rerr) {
				return "missing", nil
			}
			return "unreadable", rerr
		}
		trimmed := strings.TrimSpace(string(data))
		if trimmed == "" {
			return "empty", nil
		}
		_, hs := parsePHCHash(trimmed)
		return hs, nil
	}
	if status == "valid" && st != nil && st.Locked {
		if strings.TrimSpace(st.Hash) == "" {
			return "corrupt", nil
		}
		_, hs := parsePHCHash(st.Hash)
		return hs, nil
	}
	return status, err
}

func hasAdminPassword() bool {
	st, status, _ := loadAdminAuthState()
	return status == "valid" && st != nil && st.Locked && strings.TrimSpace(st.Hash) != ""
}

func legacyAuthArtifactsPresentOrUnknown() (present bool, unknown bool, why string) {
	paths := []string{
		adminPasswordHashPath(),
		adminPasswordLockPath(),
		adminPasswordTxPath(),
		adminPasswordRecoveryPath(),
	}
	for _, p := range paths {
		_, err := os.Stat(p)
		if err == nil {
			return true, false, p
		}
		if err != nil && !os.IsNotExist(err) {
			return false, true, p + ": " + err.Error()
		}
	}
	return false, false, ""
}

func passwordModeLocked() bool {
	// Reset marker present => fail closed until reset completed with new password.
	if _, err := os.Stat(adminPasswordResetPath()); err == nil {
		return true
	} else if err != nil && !os.IsNotExist(err) {
		logf("[auth] reset marker state unknown (%v); failing closed", err)
		return true
	}
	// Legacy recovery/tx markers => fail closed.
	for _, p := range []string{adminPasswordRecoveryPath(), adminPasswordTxPath()} {
		if _, err := os.Stat(p); err == nil {
			return true
		} else if err != nil && !os.IsNotExist(err) {
			logf("[auth] legacy marker state unknown (%s: %v); failing closed", p, err)
			return true
		}
	}

	st, status, err := loadAdminAuthState()
	if status == "missing" {
		// Invariant: only allow legacy token mode when certain no password state,
		// legacy hash, lock, tx, or recovery marker exists.
		present, unknown, why := legacyAuthArtifactsPresentOrUnknown()
		if present || unknown {
			logf("[auth] auth state missing but legacy artifacts present/unknown (%s); failing closed", why)
			return true
		}
		return false
	}
	if status == "valid" {
		return st != nil && st.Locked
	}
	// empty/corrupt/unsupported/unreadable/unknown => fail closed
	logf("[auth] auth state %s (%v); failing closed", status, err)
	return true
}

func setAdminPassword(password string) error {
	if len(password) < adminPasswordMinLen {
		return fmt.Errorf("password too short (min %d)", adminPasswordMinLen)
	}
	hash, err := argon2idHash(password)
	if err != nil {
		return err
	}
	if err := saveAdminAuthState(&adminAuthState{V: 1, Locked: true, Hash: hash}); err != nil {
		return err
	}
	// Clear reset marker after successful password set. Do not silently ignore failure:
	// a lingering reset marker can cause unintended reset on next restart.
	if err := os.Remove(adminPasswordResetPath()); err != nil && !os.IsNotExist(err) {
		logf("[auth] CRITICAL: password state saved but failed to remove reset marker: %v", err)
		return fmt.Errorf("password saved but reset marker cleanup failed: %w", err)
	}
	// Clean up legacy markers/files best-effort.
	for _, p := range []string{adminPasswordRecoveryPath(), adminPasswordTxPath()} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			logf("[auth] warning: failed to remove legacy marker %s: %v", p, err)
		}
	}
	return nil
}

func verifyAdminPassword(password string) bool {
	st, status, _ := loadAdminAuthState()
	if status != "valid" || st == nil || !st.Locked || strings.TrimSpace(st.Hash) == "" {
		return false
	}
	return verifyArgon2id(password, st.Hash)
}

func ensureAdminPassword() {
	migrateLegacyAuth()

	// Recovery/reset flow: if reset marker exists, only apply env reset when no valid
	// locked password state exists. This prevents repeated resets when marker cleanup failed.
	if _, err := os.Stat(adminPasswordResetPath()); err == nil {
		if hasAdminPassword() {
			logf("[auth] reset marker present but valid password state exists; skipping env reset")
			if err := os.Remove(adminPasswordResetPath()); err != nil && !os.IsNotExist(err) {
				logf("[auth] CRITICAL: failed to remove stale reset marker: %v", err)
			}
			return
		}
		pw := strings.TrimSpace(os.Getenv("WEB2API_ADMIN_PASSWORD"))
		if pw == "" {
			logf("[auth] password reset marker present; waiting for WEB2API_ADMIN_PASSWORD to be set")
			return
		}
		if len(pw) < adminPasswordMinLen {
			logf("[auth] WEB2API_ADMIN_PASSWORD too short for reset (min %d)", adminPasswordMinLen)
			return
		}
		if err := setAdminPassword(pw); err != nil {
			logf("[auth] failed to apply password reset: %v", err)
			return
		}
		logf("[auth] password reset applied from environment")
		return
	} else if err != nil && !os.IsNotExist(err) {
		logf("[auth] reset marker state unknown (%v); fail closed", err)
		return
	}

	if hasAdminPassword() || passwordModeLocked() {
		return
	}
	pw := strings.TrimSpace(os.Getenv("WEB2API_ADMIN_PASSWORD"))
	if pw == "" {
		return
	}
	if len(pw) < adminPasswordMinLen {
		logf("[auth] WEB2API_ADMIN_PASSWORD too short (min %d), ignoring", adminPasswordMinLen)
		return
	}
	if err := setAdminPassword(pw); err != nil {
		logf("[auth] failed to persist admin password: %v", err)
		return
	}
	logf("[auth] admin password configured from environment")
}

func initAdminPassword() {
	ensureAdminPassword()
}
