package app

import (
	"bytes"
	"net/http/httptest"
	"testing"
)

func TestHandleAdminBackup(t *testing.T) {
	cfg.AdminToken = "admin-secret"
	// ensure at least one row
	_ = kvSet("backup_test_key", "backup_test_val")
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/admin/api/backup", nil)
	req.Header.Set("Authorization", "Bearer admin-secret")
	handleAdminBackup(rr, req)
	if rr.Code != 200 {
		t.Fatalf("backup status %d: %s", rr.Code, rr.Body.String())
	}
	if rr.Body.Len() == 0 {
		t.Fatal("empty backup body")
	}
	if !bytes.HasPrefix(rr.Body.Bytes(), []byte("SQLite format 3")) {
		t.Fatalf("backup not a sqlite db: %x", rr.Body.Bytes()[:16])
	}
}
