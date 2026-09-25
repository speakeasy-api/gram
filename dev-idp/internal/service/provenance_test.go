package service

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gen "github.com/speakeasy-api/gram/dev-idp/gen/dev_idp"
	users "github.com/speakeasy-api/gram/dev-idp/gen/users"
	"github.com/speakeasy-api/gram/dev-idp/internal/bootstrap"
	"github.com/speakeasy-api/gram/dev-idp/internal/config"
	workos "github.com/speakeasy-api/gram/dev-idp/internal/modes/workos"
	"github.com/speakeasy-api/gram/plog"
	"github.com/stretchr/testify/require"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	goahttp "goa.design/goa/v3/http"
)

func provenanceService(t *testing.T, db *sql.DB, backend workos.Backend) (*DevIdpService, *gen.CurrentUser) {
	t.Helper()
	logger := plog.NewLogger(io.Discard)
	tp := tracenoop.NewTracerProvider()
	user, err := NewUsersService(logger, tp, db).Create(t.Context(), &users.CreatePayload{Email: "local@example.test", DisplayName: "Local", PhotoURL: nil, GithubHandle: nil, Admin: nil, Whitelisted: nil})
	require.NoError(t, err)
	svc := NewDevIdpService(logger, tp, db, backend)
	selected, err := svc.SetCurrentUser(t.Context(), &gen.SetCurrentUserPayload{Mode: modeOAuth21, UserID: &user.ID, WorkosSub: nil})
	require.NoError(t, err)
	require.Nil(t, selected.Provenance, "set never supplies provenance")
	return svc, selected
}

func diskProvenanceDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	root := t.TempDir()
	real := filepath.Join(root, "real")
	require.NoError(t, os.Mkdir(real, 0o700))
	link := filepath.Join(root, "alias")
	require.NoError(t, os.Symlink(real, link))
	db, err := bootstrap.Open(t.Context(), config.DB{Mode: config.DBModeFile, Path: filepath.Join(link, "identity.db")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	canonical, err := filepath.EvalSymlinks(filepath.Join(real, "identity.db"))
	require.NoError(t, err)
	return db, canonical
}

func TestCurrentUserProvenanceReportsOpenedDatabaseAndProcessOrigin(t *testing.T) {
	t.Parallel()
	db, path := diskProvenanceDB(t)
	svc, selected := provenanceService(t, db, workos.BackendLocal)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	root := discoverWorktreeRoot(cwd)
	require.NotEmpty(t, root)
	require.True(t, filepath.IsAbs(root))
	view, err := svc.GetCurrentUser(t.Context(), &gen.GetCurrentUserPayload{Mode: modeOAuth21})
	require.NoError(t, err)
	require.Equal(t, selected.Mode, view.Mode)
	require.Equal(t, selected.User, view.User)
	require.Nil(t, view.Workos)
	require.Equal(t, &gen.CurrentUserProvenance{Backend: "local", WorktreeRoot: root, DatabasePath: path}, view.Provenance)

	mux := goahttp.NewMuxer()
	AttachDevIdp(mux, svc)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/rpc/devIdp.getCurrentUser", strings.NewReader(`{"mode":"oauth2-1"}`)))
	require.Equal(t, http.StatusOK, response.Code)
	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	var proof map[string]string
	require.NoError(t, json.Unmarshal(body["provenance"], &proof))
	require.Equal(t, map[string]string{"backend": "local", "worktree_root": root, "database_path": path}, proof)
	require.JSONEq(t, `"oauth2-1"`, string(body["mode"]))
	require.Contains(t, string(body["user"]), selected.User.ID)
}

func TestCurrentUserProvenanceOmitsMemory(t *testing.T) {
	t.Parallel()
	svc, selected := provenanceService(t, testDB(t), workos.BackendLocal)
	view, err := svc.GetCurrentUser(t.Context(), &gen.GetCurrentUserPayload{Mode: modeOAuth21})
	require.NoError(t, err)
	require.Equal(t, selected, view)
	mux := goahttp.NewMuxer()
	AttachDevIdp(mux, svc)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/rpc/devIdp.getCurrentUser", strings.NewReader(`{"mode":"oauth2-1"}`)))
	require.Equal(t, http.StatusOK, response.Code)
	require.NotContains(t, response.Body.String(), "provenance")
}

func TestCurrentUserProvenanceOmitsWorkOSBackend(t *testing.T) {
	t.Parallel()
	db, _ := diskProvenanceDB(t)
	svc, selected := provenanceService(t, db, workos.BackendWorkOS)
	view, err := svc.GetCurrentUser(t.Context(), &gen.GetCurrentUserPayload{Mode: modeOAuth21})
	require.NoError(t, err)
	require.Equal(t, selected, view)
}

func TestCurrentUserProvenanceOmitsWorkOSSlot(t *testing.T) {
	t.Parallel()
	db, _ := diskProvenanceDB(t)
	svc, _ := provenanceService(t, db, workos.BackendLocal)
	selected, err := svc.SetCurrentUser(t.Context(), &gen.SetCurrentUserPayload{Mode: modeWorkos, UserID: nil, WorkosSub: new("user_placeholder")})
	require.NoError(t, err)
	view, err := svc.GetCurrentUser(t.Context(), &gen.GetCurrentUserPayload{Mode: modeWorkos})
	require.NoError(t, err)
	require.Equal(t, selected, view)
	require.Nil(t, view.Provenance)
}

func TestCurrentUserProvenanceOmitsUnknownOrigin(t *testing.T) {
	t.Parallel()
	db, _ := diskProvenanceDB(t)
	svc, selected := provenanceService(t, db, workos.BackendLocal)
	svc.worktreeRoot = discoverWorktreeRoot(t.TempDir())
	require.Empty(t, svc.worktreeRoot)
	view, err := svc.GetCurrentUser(t.Context(), &gen.GetCurrentUserPayload{Mode: modeOAuth21})
	require.NoError(t, err)
	require.Equal(t, selected, view)
}

func TestDiscoverWorktreeRootCanonicalizesNestedProcessOrigin(t *testing.T) {
	t.Parallel()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: /unused/test-marker\n"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(root, "dev-idp"), 0o700))
	alias := filepath.Join(t.TempDir(), "alias")
	require.NoError(t, os.Symlink(root, alias))
	require.Equal(t, root, discoverWorktreeRoot(root))
	require.Equal(t, root, discoverWorktreeRoot(filepath.Join(alias, "dev-idp")))
	require.Empty(t, discoverWorktreeRoot(""))
	require.Empty(t, discoverWorktreeRoot(filepath.Join(root, "missing")))
}

func TestCurrentUserProvenanceOmitsUnavailableDatabaseMetadata(t *testing.T) {
	t.Parallel()
	db, _ := diskProvenanceDB(t)
	svc, _ := provenanceService(t, db, workos.BackendLocal)
	conn, err := db.Conn(t.Context())
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	require.Nil(t, svc.currentUserProvenance(t.Context(), conn))
}

func TestCurrentUserProvenanceOmitsUnresolvableDatabasePath(t *testing.T) {
	t.Parallel()
	db, path := diskProvenanceDB(t)
	svc, selected := provenanceService(t, db, workos.BackendLocal)
	db.SetMaxOpenConns(1)
	// SQLite can still serve the opened database after its pathname disappears.
	require.NoError(t, os.Remove(path))
	view, err := svc.GetCurrentUser(t.Context(), &gen.GetCurrentUserPayload{Mode: modeOAuth21})
	require.NoError(t, err)
	require.Equal(t, selected, view)
}
