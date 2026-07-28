package openrouter

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
)

// TestProvisionAPIKey_ConcurrentFirstProvision pins the provisioning race fix:
// N concurrent completions racing on an org's very first key of a given type
// must mint exactly one upstream OpenRouter key and all resolve to it. Without
// per-(org, key type) serialization, the losers fail on the composite primary
// key after already creating an upstream key, orphaning it.
func TestProvisionAPIKey_ConcurrentFirstProvision(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "orprovisionrace")
	require.NoError(t, err)

	orgID := "org-" + uuid.NewString()[:8]
	_, err = orgRepo.New(conn).UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Provision Race Org",
		Slug:        orgID,
		WorkosID:    pgtype.Text{String: "", Valid: false},
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	var mu sync.Mutex
	upstreamCreates := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/keys" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		mu.Lock()
		upstreamCreates++
		n := upstreamCreates
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		body := map[string]any{
			"data": map[string]any{"limit": 5.0, "hash": fmt.Sprintf("hash-%d", n)},
			"key":  fmt.Sprintf("sk-or-race-%d", n),
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(upstream.Close)

	guardianPolicy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)

	provisioner := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), guardianPolicy, conn, "test", "provisioning-key", nil, nil, nil)
	provisioner.baseURL = upstream.URL

	const workers = 8
	keys := make([]string, workers)
	var eg errgroup.Group
	for i := range workers {
		eg.Go(func() error {
			key, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
			if err != nil {
				return err
			}
			keys[i] = key
			return nil
		})
	}
	require.NoError(t, eg.Wait())

	require.Equal(t, 1, upstreamCreates, "exactly one upstream key must be created")
	for _, key := range keys {
		require.Equal(t, "sk-or-race-1", key, "every caller must resolve to the single provisioned key")
	}

	row, err := repo.New(conn).GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeInternal),
	})
	require.NoError(t, err)
	require.Equal(t, "sk-or-race-1", row.Key)
	require.Equal(t, "hash-1", row.KeyHash)
}
