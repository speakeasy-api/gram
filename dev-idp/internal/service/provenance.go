package service

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"

	gen "github.com/speakeasy-api/gram/dev-idp/gen/dev_idp"
)

// discoverWorktreeRoot uses process origin, never a caller-supplied claimed root.
// A .git directory (checkout) or file (linked worktree) marks the nearest root.
func discoverWorktreeRoot(cwd string) string {
	if cwd == "" {
		return ""
	}
	root, err := filepath.EvalSymlinks(cwd)
	if err != nil || !filepath.IsAbs(root) {
		return ""
	}
	for {
		marker, err := os.Stat(filepath.Join(root, ".git"))
		if err == nil && (marker.IsDir() || marker.Mode().IsRegular()) {
			return root
		}
		if err != nil && !os.IsNotExist(err) {
			return ""
		}
		parent := filepath.Dir(root)
		if parent == root {
			return ""
		}
		root = parent
	}
}

// Read the opened main database on the same connection as the selected user.
// In-memory SQLite returns an empty filename. Any uncertainty omits the entire
// proof without changing the identity API's success or response mode.
func (s *DevIdpService) currentUserProvenance(ctx context.Context, conn *sql.Conn) *gen.CurrentUserProvenance {
	if s.worktreeRoot == "" {
		return nil
	}
	var path string
	if err := conn.QueryRowContext(ctx, "SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&path); err != nil || !filepath.IsAbs(path) {
		return nil
	}
	path, err := filepath.EvalSymlinks(path)
	if err != nil || !filepath.IsAbs(path) {
		return nil
	}
	return &gen.CurrentUserProvenance{Backend: s.backend.String(), WorktreeRoot: s.worktreeRoot, DatabasePath: path}
}
