package openrouter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
)

// disabledKeyUpstream mimics OpenRouter for a disabled key: the key's own
// credential is refused, while the provisioning key can still read it by hash.
type disabledKeyUpstream struct {
	mu       sync.Mutex
	requests []string
}

func (u *disabledKeyUpstream) seen() []string {
	u.mu.Lock()
	defer u.mu.Unlock()

	return append([]string(nil), u.requests...)
}

func (u *disabledKeyUpstream) handler(t *testing.T, keyHash string, usageMonthly float64) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, req *http.Request) {
		u.mu.Lock()
		u.requests = append(u.requests, req.Method+" "+req.URL.Path+" "+req.Header.Get("Authorization"))
		u.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/v1/keys/"+keyHash && req.Header.Get("Authorization") == "Bearer provisioning-key":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"hash": keyHash, "disabled": true, "limit": 100.0, "usage_monthly": usageMonthly},
			})
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}
}

func newCreditsUsedProvisioner(t *testing.T, handler http.Handler) (*OpenRouter, string) {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "orcreditsused")
	require.NoError(t, err)

	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)

	guardianPolicy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)

	provisioner := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), guardianPolicy, conn, "test", "provisioning-key", nil, nil, nil, testenv.NewEncryptionClient(t))
	provisioner.baseURL = upstream.URL

	return provisioner, seedOrg(t, conn)
}

func seedDisabledKey(t *testing.T, provisioner *OpenRouter, orgID string, keyHash string) {
	t.Helper()

	ctx := t.Context()
	ciphertext, err := provisioner.enc.Encrypt([]byte("sk-or-disabled"))
	require.NoError(t, err)
	queries := repo.New(provisioner.db)
	_, err = queries.CreateOpenRouterAPIKey(ctx, repo.CreateOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeChat),
		KeyEncrypted:   conv.ToPGText(ciphertext),
		KeyHash:        keyHash,
		MonthlyCredits: 100,
	})
	require.NoError(t, err)
	_, err = queries.AddOpenRouterAPIKeyDisableCause(ctx, repo.AddOpenRouterAPIKeyDisableCauseParams{
		DisableCause:   string(DisableCauseBillingInactive),
		OrganizationID: orgID,
		KeyType:        string(KeyTypeChat),
		KeyHash:        keyHash,
	})
	require.NoError(t, err)
}

// TestGetCreditsUsed_DisabledKeyReadsUsageByHash covers an organization whose
// key was disabled at its cap or by a billing lifecycle. OpenRouter refuses the
// key's own credential then, so usage must come from the provisioning key.
func TestGetCreditsUsed_DisabledKeyReadsUsageByHash(t *testing.T) {
	t.Parallel()

	const keyHash = "hash-disabled"
	upstream := &disabledKeyUpstream{mu: sync.Mutex{}, requests: nil}
	provisioner, orgID := newCreditsUsedProvisioner(t, upstream.handler(t, keyHash, 99.996))
	seedDisabledKey(t, provisioner, orgID, keyHash)

	used, limit, err := provisioner.GetCreditsUsed(t.Context(), orgID, KeyTypeChat)
	require.NoError(t, err)
	require.InDelta(t, 100.0, used, 0.0001, "usage is rounded to cents")
	require.Equal(t, 100, limit)

	require.Equal(t, []string{"GET /v1/keys/" + keyHash + " Bearer provisioning-key"}, upstream.seen(),
		"usage must be read by hash with the provisioning key, never with the org's own key")
}

// TestGetCreditsUsed_RejectsMismatchedKeyHash guards against reporting another
// key's usage if the upstream answers for a different hash.
func TestGetCreditsUsed_RejectsMismatchedKeyHash(t *testing.T) {
	t.Parallel()

	provisioner, orgID := newCreditsUsedProvisioner(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"hash": "hash-other", "usage_monthly": 5.0},
		})
	}))
	seedDisabledKey(t, provisioner, orgID, "hash-mine")

	_, _, err := provisioner.GetCreditsUsed(t.Context(), orgID, KeyTypeChat)
	require.ErrorIs(t, err, ErrAPIKeyIdentityMismatch)
}

// TestGetCreditsUsed_UpstreamFailureIsReported keeps a provisioning-key failure
// visible instead of reporting zero usage.
func TestGetCreditsUsed_UpstreamFailureIsReported(t *testing.T) {
	t.Parallel()

	provisioner, orgID := newCreditsUsedProvisioner(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	seedDisabledKey(t, provisioner, orgID, "hash-mine")

	_, limit, err := provisioner.GetCreditsUsed(t.Context(), orgID, KeyTypeChat)
	require.Error(t, err)
	require.Equal(t, 100, limit)
}
