package app

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func setupPWTestDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "pwtest-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	cfg.DBPath = filepath.Join(dir, "test.db")
	return dir
}

func TestArgon2idHashRoundTrip(t *testing.T) {
	setupPWTestDir(t)
	hash, err := argon2idHash("Str0ng!Pass")
	if err != nil {
		t.Fatalf("argon2idHash failed: %v", err)
	}
	if hash == "" || !isValidAdminPasswordHash(hash) {
		t.Fatalf("bad hash format: %q", hash)
	}
	if !verifyArgon2id("Str0ng!Pass", hash) {
		t.Fatal("verifyArgon2id failed for correct password")
	}
	if verifyArgon2id("WrongPass", hash) {
		t.Fatal("verifyArgon2id accepted wrong password")
	}
}

func TestSetAndVerifyAdminPassword(t *testing.T) {
	dir := setupPWTestDir(t)
	if err := setAdminPassword("S3cr3t!2026"); err != nil {
		t.Fatalf("setAdminPassword failed: %v", err)
	}
	if !hasAdminPassword() {
		t.Fatal("expected password hash to persist")
	}
	if !verifyAdminPassword("S3cr3t!2026") {
		t.Fatal("verifyAdminPassword failed after persist")
	}
	if verifyAdminPassword("wrong") {
		t.Fatal("verifyAdminPassword accepted wrong password")
	}
	if err := setAdminPassword("short"); err == nil {
		t.Fatal("expected error for short password")
	}
	// hash file should have 0600 permissions on unix
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, "admin_password.hash"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("expected 0600, got %o", info.Mode().Perm())
		}
	}
}

func TestInitAdminPasswordFromEnv(t *testing.T) {
	setupPWTestDir(t)
	os.Setenv("WEB2API_ADMIN_PASSWORD", "Env!Pass2026")
	defer os.Unsetenv("WEB2API_ADMIN_PASSWORD")
	initAdminPassword()
	if !hasAdminPassword() {
		t.Fatal("expected password hash created from env")
	}
	if !verifyAdminPassword("Env!Pass2026") {
		t.Fatal("env password not verified")
	}
}

func TestPasswordHashStatusClassification(t *testing.T) {
	dir := setupPWTestDir(t)
	hashPath := filepath.Join(dir, "admin_password.hash")

	// missing
	status, _ := passwordHashStatus()
	if status != "missing" {
		t.Fatalf("expected missing, got %s", status)
	}

	// empty
	if err := os.WriteFile(hashPath, []byte("   \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, _ = passwordHashStatus()
	if status != "empty" {
		t.Fatalf("expected empty, got %s", status)
	}

	// corrupt - malformed PHC
	if err := os.WriteFile(hashPath, []byte("not-a-hash"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, _ = passwordHashStatus()
	if status != "corrupt" {
		t.Fatalf("expected corrupt, got %s", status)
	}

	// valid
	hash, err := argon2idHash("Valid!Pass123")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hashPath, []byte(hash), 0o600); err != nil {
		t.Fatal(err)
	}
	status, _ = passwordHashStatus()
	if status != "valid" {
		t.Fatalf("expected valid, got %s", status)
	}
}

func TestPHCValidationCases(t *testing.T) {
	setupPWTestDir(t)
	salt := base64.RawStdEncoding.EncodeToString([]byte("0123456789abcdef"))
	h := base64.RawStdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	good := fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=4$%s$%s", salt, h)

	cases := []struct {
		name   string
		hash   string
		want   string // valid/corrupt/unsupported
		verify bool   // if valid, verify should not panic; if invalid, should be false
	}{
		{"valid", good, "valid", false},
		{"wrong_version", fmt.Sprintf("$argon2id$v=18$m=65536,t=3,p=4$%s$%s", salt, h), "unsupported", false},
		{"wrong_algo_i", fmt.Sprintf("$argon2i$v=19$m=65536,t=3,p=4$%s$%s", salt, h), "unsupported", false},
		{"wrong_algo_d", fmt.Sprintf("$argon2d$v=19$m=65536,t=3,p=4$%s$%s", salt, h), "unsupported", false},
		{"unknown_algo", fmt.Sprintf("$argon2x$v=19$m=65536,t=3,p=4$%s$%s", salt, h), "corrupt", false},
		{"missing_param_t", fmt.Sprintf("$argon2id$v=19$m=65536,p=4$%s$%s", salt, h), "corrupt", false},
		{"duplicate_param", fmt.Sprintf("$argon2id$v=19$m=65536,m=65536,t=3,p=4$%s$%s", salt, h), "corrupt", false},
		{"m_too_large", fmt.Sprintf("$argon2id$v=19$m=1048576,t=3,p=4$%s$%s", salt, h), "unsupported", false},
		{"t_too_large", fmt.Sprintf("$argon2id$v=19$m=65536,t=1000,p=4$%s$%s", salt, h), "unsupported", false},
		{"p_too_large", fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=255$%s$%s", salt, h), "unsupported", false},
		{"salt_too_short", fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=4$%s$%s", base64.RawStdEncoding.EncodeToString([]byte("short")), h), "unsupported", false},
		{"hash_too_short", fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=4$%s$%s", salt, base64.RawStdEncoding.EncodeToString([]byte("short"))), "unsupported", false},
		{"bad_base64_salt", fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=4$%%%s$%s", salt, h), "corrupt", false},
		{"bad_base64_hash", fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=4$%s$%%%s", salt, h), "corrupt", false},
		{"truncated", "$argon2id$v=19$m=65536,t=3,p=4$" + salt, "corrupt", false},
		{"empty", "", "empty", false},
		{"extra_part", fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=4$%s$%s$extra", salt, h), "corrupt", false},
		{"unknown_param", fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=4,x=1$%s$%s", salt, h), "corrupt", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, got := parsePHCHash(tc.hash)
			if got != tc.want {
				t.Fatalf("parsePHCHash(%q) status=%s want=%s", tc.name, got, tc.want)
			}
			if verifyArgon2id("AnyPass!123", tc.hash) {
				t.Fatalf("verifyArgon2id should return false for %s", tc.name)
			}
		})
	}
}

func TestArgon2RejectsMaliciousInputBeforeKDF(t *testing.T) {
	setupPWTestDir(t)
	salt := base64.RawStdEncoding.EncodeToString([]byte("0123456789abcdef"))
	h := base64.RawStdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	// Very large memory/time within uint32 parse range but beyond supported bounds.
	bad := fmt.Sprintf("$argon2id$v=19$m=4294967295,t=4294967295,p=255$%s$%s", salt, h)

	start := time.Now()
	_, status := parsePHCHash(bad)
	elapsed := time.Since(start)
	if status != "unsupported" && status != "corrupt" {
		t.Fatalf("expected unsupported/corrupt, got %s", status)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("parsing/validation took too long: %v (Argon2 may have been called)", elapsed)
	}

	start = time.Now()
	ok := verifyArgon2id("password", bad)
	elapsed = time.Since(start)
	if ok {
		t.Fatal("verifyArgon2id should reject malicious input")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("verifyArgon2id took too long: %v (Argon2 may have been called)", elapsed)
	}
}

func TestPasswordLockBehavior(t *testing.T) {
	dir := setupPWTestDir(t)
	statePath := filepath.Join(dir, "admin_auth.json")

	if err := setAdminPassword("Lock!Pass1234"); err != nil {
		t.Fatal(err)
	}
	if !hasAdminPassword() || !passwordModeLocked() {
		t.Fatal("expected active+locked")
	}

	// corrupt hash in state while locked
	if err := os.WriteFile(statePath, []byte(`{"v":1,"locked":true,"hash":"corrupt"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if hasAdminPassword() {
		t.Fatal("corrupt hash should not be active")
	}
	if !passwordModeLocked() {
		t.Fatal("must remain locked on corrupt hash")
	}

	// missing state file after being locked? - missing means unknown legacy; but without legacy lock files, missing returns unlocked.
	// However a missing state file with no legacy markers is "missing" - for safety, passwordModeLocked returns false only if confirmed missing and no legacy lock. That’s acceptable.
	// For our single-state design, corrupt/unreadable is fail-closed.
}

func TestSetAdminPasswordAtomicPreservesOldOnFailure(t *testing.T) {
	dir := setupPWTestDir(t)
	if err := setAdminPassword("Orig!Pass1234"); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, "admin_auth.json")
	oldState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !verifyAdminPassword("Orig!Pass1234") {
		t.Fatal("original password should verify")
	}

	if runtime.GOOS != "windows" {
		dir2, err := os.MkdirTemp("", "pwfail-")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(dir2)
		cfg.DBPath = filepath.Join(dir2, "test.db")
		if err := setAdminPassword("First!Pass123"); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir2, 0o500); err != nil {
			t.Fatal(err)
		}
		err = setAdminPassword("New!Pass12345")
		_ = os.Chmod(dir2, 0o700)
		if err == nil {
			t.Fatal("expected failure when dir is not writable")
		}
		if !verifyAdminPassword("First!Pass123") {
			t.Fatal("old password should still verify after failed update")
		}
	}
	_ = oldState
}

func TestPasswordModeLockedFailClosedSingleState(t *testing.T) {
	dir := setupPWTestDir(t)
	statePath := filepath.Join(dir, "admin_auth.json")
	if passwordModeLocked() {
		t.Fatal("expected unlocked when missing")
	}
	if err := setAdminPassword("Strong!Pass1"); err != nil {
		t.Fatal(err)
	}
	if !passwordModeLocked() || !hasAdminPassword() {
		t.Fatal("expected locked+active")
	}
	if err := os.WriteFile(statePath, []byte(`{"v":1,"locked":true,"hash":""}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if !passwordModeLocked() {
		t.Fatal("locked corrupt state must fail closed")
	}
	if hasAdminPassword() {
		t.Fatal("invalid hash must not be active")
	}
	if err := os.WriteFile(statePath, []byte(`{broken`), 0o600); err != nil {
		t.Fatal(err)
	}
	if !passwordModeLocked() {
		t.Fatal("corrupt state must fail closed")
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(statePath, 0o000); err != nil {
			t.Fatal(err)
		}
		if !passwordModeLocked() {
			t.Fatal("unreadable state must fail closed")
		}
		_ = os.Chmod(statePath, 0o600)
	}
	if err := os.WriteFile(filepath.Join(dir, "admin_password.reset"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !passwordModeLocked() {
		t.Fatal("reset marker must fail closed")
	}
	_ = os.Remove(filepath.Join(dir, "admin_password.reset"))
}

func TestNoTokenBypassWhenStateUnknown(t *testing.T) {
	dir := setupPWTestDir(t)
	statePath := filepath.Join(dir, "admin_auth.json")
	if err := os.WriteFile(statePath, []byte(`{"v":1,"locked":true,"hash":"bad"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if hasAdminPassword() {
		t.Fatal("invalid hash must not be active")
	}
	if !passwordModeLocked() {
		t.Fatal("must fail closed")
	}
	if !(hasAdminPassword() || passwordModeLocked()) {
		t.Fatal("token bypass possible")
	}
	_ = dir
}

func withAuthWriteFail(t *testing.T, fail bool) {
	t.Helper()
	old := adminAuthWriteFile
	if fail {
		adminAuthWriteFile = func(path string, data []byte, perm os.FileMode) error { return fmt.Errorf("injected write failure") }
	}
	t.Cleanup(func() { adminAuthWriteFile = old })
}

func TestLegacyHashCorruptFailClosed(t *testing.T) {
	dir := setupPWTestDir(t)
	if err := os.WriteFile(filepath.Join(dir, "admin_password.hash"), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	migrateLegacyAuth()
	if !passwordModeLocked() {
		t.Fatal("must fail closed")
	}
	if hasAdminPassword() {
		t.Fatal("must not be active")
	}
}

func TestStateMissingLegacyHashExistsFailClosed(t *testing.T) {
	dir := setupPWTestDir(t)
	hash, err := argon2idHash("Legacy!Pass1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "admin_password.hash"), []byte(hash), 0o600); err != nil {
		t.Fatal(err)
	}
	if !passwordModeLocked() {
		t.Fatal("must fail closed when legacy hash exists and state missing")
	}
}

func TestMigrationFailureNoTokenBypass(t *testing.T) {
	dir := setupPWTestDir(t)
	hash, err := argon2idHash("Legacy!Pass1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "admin_password.hash"), []byte(hash), 0o600); err != nil {
		t.Fatal(err)
	}
	withAuthWriteFail(t, true)
	migrateLegacyAuth()
	if !passwordModeLocked() {
		t.Fatal("must fail closed when migration cannot persist state")
	}
	oldTok := cfg.AdminToken
	cfg.AdminToken = "admintokentest12345678901234567890"
	defer func() { cfg.AdminToken = oldTok }()
	r, _ := http.NewRequest("GET", "/admin/api/status", nil)
	r.Header.Set("Authorization", "Bearer admintokentest12345678901234567890")
	if authOK(r) {
		t.Fatal("token bypass must not be possible")
	}
	if bearerAuthOK(r) {
		t.Fatal("bearer token bypass must not be possible")
	}
	lr, _ := http.NewRequest("POST", "/admin/api/login", strings.NewReader(`{"token":"admintokentest12345678901234567890"}`))
	rr := httptest.NewRecorder()
	handleAdminLogin(rr, lr)
	if rr.Code == 200 {
		t.Fatal("login token bypass must not be possible")
	}
}

func TestResetMarkerNotRepeatedWhenValidPasswordExists(t *testing.T) {
	dir := setupPWTestDir(t)
	if err := setAdminPassword("AdminUI!Pass1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "admin_password.reset"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	os.Setenv("WEB2API_ADMIN_PASSWORD", "Env!Different1")
	defer os.Unsetenv("WEB2API_ADMIN_PASSWORD")
	ensureAdminPassword()
	if !verifyAdminPassword("AdminUI!Pass1") {
		t.Fatal("should not reset to env password when valid state exists")
	}
	if verifyAdminPassword("Env!Different1") {
		t.Fatal("env password must not overwrite")
	}
}

func TestResetMarkerStateWriteFailKeepsMarker(t *testing.T) {
	dir := setupPWTestDir(t)
	resetPath := filepath.Join(dir, "admin_password.reset")
	if err := os.WriteFile(resetPath, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	withAuthWriteFail(t, true)
	os.Setenv("WEB2API_ADMIN_PASSWORD", "NewReset!Pass1")
	defer os.Unsetenv("WEB2API_ADMIN_PASSWORD")
	ensureAdminPassword()
	if _, err := os.Stat(resetPath); err != nil {
		t.Fatal("reset marker should remain on write failure")
	}
	if hasAdminPassword() {
		t.Fatal("password should not be active when write failed")
	}
}

func TestLegacyHashUnreadableFailClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod semantics unreliable on windows")
	}
	dir := setupPWTestDir(t)
	p := filepath.Join(dir, "admin_password.hash")
	if err := os.WriteFile(p, []byte("$argon2id$v=19$m=65536,t=3,p=4$c29tZXNhbHQ$c29tZWhhc2g"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o600) })
	migrateLegacyAuth()
	if !passwordModeLocked() {
		t.Fatal("must fail closed on unreadable legacy hash")
	}
}

func TestLegacyLockUnreadableFailClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod semantics unreliable on windows")
	}
	dir := setupPWTestDir(t)
	p := filepath.Join(dir, "admin_password.lock")
	if err := os.WriteFile(p, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o600) })
	migrateLegacyAuth()
	if !passwordModeLocked() {
		t.Fatal("must fail closed on unreadable legacy lock")
	}
}
