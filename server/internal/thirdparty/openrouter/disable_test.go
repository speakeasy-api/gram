package openrouter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
)

type disableTestUpstream struct {
	server        *httptest.Server
	mu            sync.Mutex
	patchedLimits []float64
}

func (u *disableTestUpstream) limits() []float64 {
	u.mu.Lock()
	defer u.mu.Unlock()

	return append([]float64(nil), u.patchedLimits...)
}

func newDisableTestProvisioner(t *testing.T, orgID string) (*OpenRouter, *disableTestUpstream, *repo.Queries) {
	t.Helper()

	ctx := t.Context()

	conn, err := infra.CloneTestDatabase(t, "ordisablekey")
	require.NoError(t, err)

	_, err = orgRepo.New(conn).UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Disable Key Org",
		Slug:        orgID,
		WorkosID:    pgtype.Text{String: "", Valid: false},
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	upstream := &disableTestUpstream{server: nil, mu: sync.Mutex{}, patchedLimits: nil}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.Method {
		case http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"limit": 100.0, "hash": "hash-1"},
				"key":  "sk-or-disable-1",
			})
		case http.MethodPatch:
			// A patch that carries no limit is rejected rather than recorded, so
			// the caller's assertion on limits() fails instead of a panic on the
			// server goroutine.
			var body updateKeyRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Limit == nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			upstream.mu.Lock()
			upstream.patchedLimits = append(upstream.patchedLimits, *body.Limit)
			upstream.mu.Unlock()

			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"limit": *body.Limit, "hash": "hash-1"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(upstream.server.Close)

	guardianPolicy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)

	provisioner := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), guardianPolicy, conn, "test", "provisioning-key", nil, nil, nil)
	provisioner.baseURL = upstream.server.URL

	return provisioner, upstream, repo.New(conn)
}

func TestDisableAPIKey_ZeroesUpstreamLimit(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)

	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)

	require.NoError(t, provisioner.DisableAPIKey(ctx, orgID, KeyTypeInternal))

	require.Equal(t, []float64{0}, upstream.limits(), "the upstream ceiling is what binds every caller")

	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeInternal),
	})
	require.NoError(t, err)
	require.True(t, row.Disabled)
	require.Equal(t, int64(0), row.MonthlyCredits)
}

// A retried demotion must not fail on a key it already zeroed, so disabling
// twice has to stay safe.
func TestDisableAPIKey_IsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)

	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)

	require.NoError(t, provisioner.DisableAPIKey(ctx, orgID, KeyTypeInternal))
	require.NoError(t, provisioner.DisableAPIKey(ctx, orgID, KeyTypeInternal))

	require.Equal(t, []float64{0, 0}, upstream.limits())

	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeInternal),
	})
	require.NoError(t, err)
	require.True(t, row.Disabled)
}

// An organization that never provisioned a key of this type has nothing to
// lock down, and the sweeper must not fail on it.
func TestDisableAPIKey_NoKeyIsNoop(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, _ := newDisableTestProvisioner(t, orgID)

	require.NoError(t, provisioner.DisableAPIKey(ctx, orgID, KeyTypeChat))
	require.Empty(t, upstream.limits())
}
