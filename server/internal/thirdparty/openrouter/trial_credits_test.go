package openrouter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

// trialCapFixture provisions against an upstream stub that records the credit
// limit each create-key request asks for.
type trialCapFixture struct {
	// provisioner is wired to the stub upstream and the cloned database.
	provisioner *OpenRouter

	// conn is the cloned database the fixture seeded the organization into.
	conn *pgxpool.Pool

	// orgID names the seeded organization, which sits on the enterprise tier.
	orgID string

	mu sync.Mutex

	// createLimits collects the limit from every create-key request, in order.
	createLimits []float64
}

// recordedLimits returns the limit each create-key request carried.
func (f *trialCapFixture) recordedLimits() []float64 {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]float64(nil), f.createLimits...)
}

// newTrialCapFixture seeds an enterprise-tier organization with no trial. A
// caller that wants a trial inserts the row itself, so each test states the
// lifecycle state it exercises.
func newTrialCapFixture(t *testing.T, dbName string) *trialCapFixture {
	t.Helper()

	ctx := t.Context()

	conn, err := infra.CloneTestDatabase(t, dbName)
	require.NoError(t, err)

	orgID := "org-" + uuid.NewString()[:8]
	orgQueries := orgRepo.New(conn)
	_, err = orgQueries.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Trial Cap Org",
		Slug:        orgID,
		WorkosID:    pgtype.Text{String: "", Valid: false},
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	// Arming a trial sets the enterprise tier, so a trial organization and a
	// paying enterprise customer are indistinguishable by account type alone.
	require.NoError(t, orgQueries.SetAccountType(ctx, orgRepo.SetAccountTypeParams{
		ID:              orgID,
		GramAccountType: string(billing.TierEnterprise),
	}))

	fixture := &trialCapFixture{
		provisioner:  nil,
		conn:         conn,
		orgID:        orgID,
		mu:           sync.Mutex{},
		createLimits: nil,
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GetCreditsUsed reads usage before it reports the ceiling, so the stub
		// answers that call too.
		if r.Method == http.MethodGet && r.URL.Path == "/v1/key" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"limit": 0.0, "usage_monthly": 0.0},
			})
			return
		}

		// A refresh patches the ceiling upstream before it writes the key row,
		// which is how a limit that no longer matches the tier gets recorded.
		if r.Method == http.MethodPatch {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"limit": 0.0, "hash": "hash-1"},
			})
			return
		}

		if r.Method != http.MethodPost || r.URL.Path != "/v1/keys" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var body struct {
			Limit *float64 `json:"limit"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Limit == nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		fixture.mu.Lock()
		fixture.createLimits = append(fixture.createLimits, *body.Limit)
		fixture.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"limit": *body.Limit, "hash": "hash-1"},
			"key":  "sk-or-trial-cap-1",
		})
	}))
	t.Cleanup(upstream.Close)

	guardianPolicy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)

	fixture.provisioner = New(testenv.NewLogger(t), testenv.NewTracerProvider(t), guardianPolicy, conn, "test", "provisioning-key", nil, nil, nil)
	fixture.provisioner.baseURL = upstream.URL

	return fixture
}

// TestProvisionAPIKey_ActiveTrialTakesTrialCap pins the trial ceiling: an
// organization inside a self-signup enterprise trial must mint its key at the
// trial cap, not at the paid enterprise amount its account type resolves to.
func TestProvisionAPIKey_ActiveTrialTakesTrialCap(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newTrialCapFixture(t, "ortrialcapactive")

	now := time.Now().UTC()
	require.NoError(t, trialsRepo.New(fixture.conn).InsertTrialFixture(ctx, trialsRepo.InsertTrialFixtureParams{
		OrganizationID: fixture.orgID,
		CreatedAt:      conv.ToPGTimestamptz(now),
		EndsAt:         conv.ToPGTimestamptz(now.Add(14 * 24 * time.Hour)),
		ConvertedAt:    conv.PtrToPGTimestamptz(nil),
		DemotedAt:      conv.PtrToPGTimestamptz(nil),
	}))

	_, err := fixture.provisioner.ProvisionAPIKey(ctx, fixture.orgID, KeyTypeChat)
	require.NoError(t, err)

	require.Equal(t, []float64{float64(enterpriseTrialCredits)}, fixture.recordedLimits())

	_, limit, err := fixture.provisioner.GetCreditsUsed(ctx, fixture.orgID, KeyTypeChat)
	require.NoError(t, err)
	require.Equal(t, enterpriseTrialCredits, limit, "the reported ceiling must match the minted key")
}

// TestProvisionAPIKey_PaidEnterpriseKeepsTierCap guards the other side of the
// trial ceiling: an enterprise customer with no trial row keeps the full
// account-type amount.
func TestProvisionAPIKey_PaidEnterpriseKeepsTierCap(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newTrialCapFixture(t, "ortrialcappaid")

	_, err := fixture.provisioner.ProvisionAPIKey(ctx, fixture.orgID, KeyTypeChat)
	require.NoError(t, err)

	require.Equal(t, []float64{float64(creditsAccountTypeMap[string(billing.TierEnterprise)])}, fixture.recordedLimits())
}

// TestGetCreditsUsed_ReportsKeyLimitOverTierDefault covers the organizations
// whose ceiling was moved by hand. Recomputing the amount from the account type
// would report the tier default and hide the raise the customer was given, so
// the reported ceiling has to come from the key.
func TestGetCreditsUsed_ReportsKeyLimitOverTierDefault(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newTrialCapFixture(t, "orcreditskeylimit")

	_, err := fixture.provisioner.ProvisionAPIKey(ctx, fixture.orgID, KeyTypeChat)
	require.NoError(t, err)

	raised := creditsAccountTypeMap[string(billing.TierEnterprise)] + 150
	refreshed, err := fixture.provisioner.RefreshAPIKeyLimit(ctx, fixture.orgID, KeyTypeChat, &raised)
	require.NoError(t, err)
	require.Equal(t, raised, refreshed)

	_, limit, err := fixture.provisioner.GetCreditsUsed(ctx, fixture.orgID, KeyTypeChat)
	require.NoError(t, err)
	require.Equal(t, raised, limit, "the reported ceiling must follow the key, not the account type")
}
