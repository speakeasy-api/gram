package openrouter

import (
	"encoding/json"
	"io"
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
	server  *httptest.Server
	mu      sync.Mutex
	patches []string
}

// recorded returns the raw patch bodies. They stay raw because the field a
// limit-only patch must NOT carry cannot be told apart from a null one after
// decoding.
func (u *disableTestUpstream) recorded() []string {
	u.mu.Lock()
	defer u.mu.Unlock()

	return append([]string(nil), u.patches...)
}

func decodePatch(t *testing.T, body string) updateKeyRequest {
	t.Helper()

	var decoded updateKeyRequest
	require.NoError(t, json.Unmarshal([]byte(body), &decoded))

	return decoded
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

	upstream := &disableTestUpstream{server: nil, mu: sync.Mutex{}, patches: nil}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.Method {
		case http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"limit": 100.0, "hash": "hash-1"},
				"key":  "sk-or-disable-1",
			})
		case http.MethodPatch:
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			var body updateKeyRequest
			if err := json.Unmarshal(raw, &body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			upstream.mu.Lock()
			upstream.patches = append(upstream.patches, string(raw))
			upstream.mu.Unlock()

			limit := 100.0
			if body.Limit != nil {
				limit = *body.Limit
			}

			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"limit": limit, "hash": "hash-1"},
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

func TestDisableAPIKey_DisablesKeyUpstream(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)

	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)

	require.NoError(t, provisioner.DisableAPIKey(ctx, orgID, KeyTypeInternal))

	patches := upstream.recorded()
	require.Len(t, patches, 1)

	// Both halves ride in one patch. The zero ceiling keeps the upstream
	// rejection a 402, which chat and resolution analysis both branch on.
	lockdown := decodePatch(t, patches[0])
	require.NotNil(t, lockdown.Disabled)
	require.True(t, *lockdown.Disabled)
	require.NotNil(t, lockdown.Limit)
	require.Zero(t, *lockdown.Limit)

	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeInternal),
	})
	require.NoError(t, err)
	require.True(t, row.Disabled)
	require.Equal(t, int64(0), row.MonthlyCredits)
}

// A retried demotion must not fail on a key it already turned off, so
// disabling twice has to stay safe.
func TestDisableAPIKey_IsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)

	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)

	require.NoError(t, provisioner.DisableAPIKey(ctx, orgID, KeyTypeInternal))
	require.NoError(t, provisioner.DisableAPIKey(ctx, orgID, KeyTypeInternal))

	require.Len(t, upstream.recorded(), 2)

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
	require.Empty(t, upstream.recorded())
}

// Sales reinstate a demoted organization by raising its limit, so the refresh
// path has to clear the flag on both sides.
func TestRefreshAPIKeyLimit_ReinstatesDisabledKey(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)

	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)
	require.NoError(t, provisioner.DisableAPIKey(ctx, orgID, KeyTypeInternal))

	limit := 42
	refreshed, err := provisioner.RefreshAPIKeyLimit(ctx, orgID, KeyTypeInternal, &limit)
	require.NoError(t, err)
	require.Equal(t, 42, refreshed)

	patches := upstream.recorded()
	require.Len(t, patches, 2)

	reinstate := decodePatch(t, patches[1])
	require.NotNil(t, reinstate.Disabled)
	require.False(t, *reinstate.Disabled, "a limit alone does not bring a disabled key back")

	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeInternal),
	})
	require.NoError(t, err)
	require.False(t, row.Disabled, "a stale flag keeps the key out of credit-usage polling")
	require.Equal(t, int64(42), row.MonthlyCredits)

	// Refreshing an enabled key must send the body it sent before the disabled
	// field existed. Carrying disabled=false on every refresh would revive a
	// key an operator turned off on the OpenRouter dashboard.
	_, err = provisioner.RefreshAPIKeyLimit(ctx, orgID, KeyTypeInternal, &limit)
	require.NoError(t, err)

	patches = upstream.recorded()
	require.Len(t, patches, 3)
	require.JSONEq(t, `{"limit":42,"limit_reset":"monthly"}`, patches[2])
}
