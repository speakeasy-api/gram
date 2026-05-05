package bootstrap_test

import (
	"path/filepath"
	"testing"

	"github.com/speakeasy-api/gram/dev-idp/internal/bootstrap"
	"github.com/speakeasy-api/gram/dev-idp/internal/config"
)

func TestOpen_Memory(t *testing.T) {
	t.Parallel()
	db, err := bootstrap.Open(t.Context(), config.DB{Mode: config.DBModeMemory, Path: ""})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var n int
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM users").Scan(&n); err != nil {
		t.Fatalf("query users: %v", err)
	}
	if n != 0 {
		t.Fatalf("fresh users count: got %d, want 0", n)
	}
}

func TestOpen_FileIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := config.DB{Mode: config.DBModeFile, Path: filepath.Join(dir, "devidp.db")}

	db1, err := bootstrap.Open(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Open #1: %v", err)
	}
	if _, err := db1.ExecContext(t.Context(),
		"INSERT INTO users (id, email, display_name) VALUES (?, ?, ?)",
		"abc", "a@b", "A"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	_ = db1.Close()

	db2, err := bootstrap.Open(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Open #2 (re-apply schema): %v", err)
	}
	t.Cleanup(func() { _ = db2.Close() })

	var n int
	if err := db2.QueryRowContext(t.Context(), "SELECT count(*) FROM users").Scan(&n); err != nil {
		t.Fatalf("query users: %v", err)
	}
	if n != 1 {
		t.Fatalf("re-opened users count: got %d, want 1 (schema apply must be idempotent)", n)
	}
}

func TestParseDB(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		wantErr bool
	}{
		{"", false},
		{"memory", false},
		{":memory:", false},
		{"file:./x.db", false},
		{"file:/abs/x.db", false},
		{"file:", true},
		{"sqlite://x", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			_, err := config.ParseDB(tt.in)
			gotErr := err != nil
			if gotErr != tt.wantErr {
				t.Fatalf("ParseDB(%q): err=%v, wantErr=%v", tt.in, err, tt.wantErr)
			}
		})
	}
}
