package openrouter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
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
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

// The trial cap and the enterprise tier amount are literals on purpose.
// Asserting against the production constants would make these tests pass for
// any value of them, and the value is what this suite exists to pin.
const (
	wantTrialLimit      = 50
	wantEnterpriseLimit = 100
)

// trialCapFixture provisions against an upstream stub that records the credit
// limit each create-key request asks for.
type trialCapFixture struct {
	// provisioner is wired to the stub upstream and the cloned database.
	provisioner *OpenRouter

	// conn is the cloned database the fixture seeded its organizations into.
	conn *pgxpool.Pool

	// orgID names the organization seeded at construction, which sits on the
	// enterprise tier and holds no trial row.
	orgID string

	// recorder collects the limits the stub upstream received.
	recorder *limitRecorder
}

// limitRecorder collects the credit limit of every create-key request.
type limitRecorder struct {
	mu sync.Mutex

	// created holds the limit from each create-key request, in order.
	created []float64
}

// createdLimits returns the limit each create-key request carried.
func (r *limitRecorder) createdLimits() []float64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.created)
}

func (r *limitRecorder) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		// GetCreditsUsed reads usage before it reports the ceiling, and
		// RefreshAPIKeyLimit patches the ceiling upstream. Neither carries a
		// limit this suite asserts on, so both get a canned reply.
		case req.Method == http.MethodGet && req.URL.Path == "/v1/key":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"limit": 0.0, "usage_monthly": 0.0},
			})

		case req.Method == http.MethodPatch:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"limit": 0.0, "hash": "hash-1"},
			})

		case req.Method == http.MethodPost && req.URL.Path == "/v1/keys":
			var body struct {
				Limit *float64 `json:"limit"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Limit == nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			r.mu.Lock()
			r.created = append(r.created, *body.Limit)
			r.mu.Unlock()

			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"limit": *body.Limit, "hash": "hash-1"},
				"key":  "sk-or-trial-cap-1",
			})

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

// newTrialCapFixture seeds an enterprise-tier organization with no trial. A
// caller that wants a trial inserts the row itself, so each test states the
// lifecycle state it exercises.
func newTrialCapFixture(t *testing.T) *trialCapFixture {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "ortrialcap")
	require.NoError(t, err)

	recorder := &limitRecorder{mu: sync.Mutex{}, created: nil}

	upstream := httptest.NewServer(recorder.handler())
	t.Cleanup(upstream.Close)

	guardianPolicy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)

	provisioner := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), guardianPolicy, conn, "test", "provisioning-key", nil, nil, nil, testenv.NewEncryptionClient(t))
	provisioner.baseURL = upstream.URL

	return &trialCapFixture{
		provisioner: provisioner,
		conn:        conn,
		orgID:       seedOrg(t, conn),
		recorder:    recorder,
	}
}

// seedOrg adds an enterprise-tier organization with no trial row. A test that
// covers several lifecycle states calls it once per state.
func seedOrg(t *testing.T, conn *pgxpool.Pool) string {
	t.Helper()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	queries := orgRepo.New(conn)

	_, err := queries.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Trial Cap Org",
		Slug:        orgID,
		WorkosID:    pgtype.Text{String: "", Valid: false},
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	// Arming a trial sets the enterprise tier, so a trial organization and a
	// paying enterprise customer are indistinguishable by account type alone.
	require.NoError(t, queries.SetAccountType(ctx, orgRepo.SetAccountTypeParams{
		ID:              orgID,
		GramAccountType: string(billing.TierEnterprise),
	}))

	return orgID
}

// insertTrial adds a trial row in the lifecycle state the caller names.
func insertTrial(t *testing.T, conn *pgxpool.Pool, orgID string, endsAt time.Time, convertedAt, demotedAt *time.Time) {
	t.Helper()

	require.NoError(t, trialsRepo.New(conn).InsertTrialFixture(t.Context(), trialsRepo.InsertTrialFixtureParams{
		OrganizationID: orgID,
		CreatedAt:      conv.ToPGTimestamptz(time.Now().UTC().Add(-24 * time.Hour)),
		EndsAt:         conv.ToPGTimestamptz(endsAt),
		ConvertedAt:    conv.PtrToPGTimestamptz(convertedAt),
		DemotedAt:      conv.PtrToPGTimestamptz(demotedAt),
	}))
}

// TestProvisionAPIKey_ActiveTrialCapsEveryKeyType pins the trial ceiling and
// its blast radius. An organization holds one key per type, so a cap that
// reached the chat key alone would leave the rest of its spend at the paid
// amount.
func TestProvisionAPIKey_ActiveTrialCapsEveryKeyType(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newTrialCapFixture(t)
	insertTrial(t, fixture.conn, fixture.orgID, time.Now().UTC().Add(14*24*time.Hour), nil, nil)

	for _, keyType := range AllKeyTypes {
		_, err := fixture.provisioner.ProvisionAPIKey(ctx, fixture.orgID, keyType)
		require.NoError(t, err, "provisioning a %s key", keyType)
	}

	require.Equal(t, slices.Repeat([]float64{wantTrialLimit}, len(AllKeyTypes)), fixture.recorder.createdLimits(),
		"every key type an organization can hold must carry the trial cap")
}

// TestProvisionAPIKey_PaidEnterpriseKeepsTierCap guards the other side of the
// trial ceiling: an enterprise customer with no trial row keeps the full
// account-type amount.
func TestProvisionAPIKey_PaidEnterpriseKeepsTierCap(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newTrialCapFixture(t)

	_, err := fixture.provisioner.ProvisionAPIKey(ctx, fixture.orgID, KeyTypeChat)
	require.NoError(t, err)

	require.Equal(t, []float64{wantEnterpriseLimit}, fixture.recorder.createdLimits())
}

// TestProvisionAPIKey_InactiveTrialKeepsTierCap pins the three lifecycle states
// that end a trial. Each one must release the cap, because the organization has
// either paid or lost the trial. A query that dropped any of these filters
// would cap a paying customer forever.
func TestProvisionAPIKey_InactiveTrialKeepsTierCap(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newTrialCapFixture(t)

	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	future := now.Add(14 * 24 * time.Hour)

	converted := seedOrg(t, fixture.conn)
	insertTrial(t, fixture.conn, converted, future, &past, nil)

	demoted := seedOrg(t, fixture.conn)
	insertTrial(t, fixture.conn, demoted, future, nil, &past)

	expired := seedOrg(t, fixture.conn)
	insertTrial(t, fixture.conn, expired, past, nil, nil)

	orgIDs := []string{converted, demoted, expired}
	for _, orgID := range orgIDs {
		_, err := fixture.provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
		require.NoError(t, err)
	}

	require.Equal(t, slices.Repeat([]float64{wantEnterpriseLimit}, len(orgIDs)), fixture.recorder.createdLimits(),
		"a converted, demoted, or expired trial must not cap the key")
}

// TestGetCreditsUsed_ReportsKeyLimitOverTierDefault covers the organizations
// whose ceiling was moved by hand. Recomputing the amount from the account type
// would report the tier default and hide the raise the customer was given, so
// the reported ceiling has to come from the key.
func TestGetCreditsUsed_ReportsKeyLimitOverTierDefault(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newTrialCapFixture(t)

	_, err := fixture.provisioner.ProvisionAPIKey(ctx, fixture.orgID, KeyTypeChat)
	require.NoError(t, err)

	const raised = wantEnterpriseLimit + 150
	limit := raised
	refreshed, err := fixture.provisioner.RefreshAPIKeyLimit(ctx, fixture.orgID, KeyTypeChat, &limit)
	require.NoError(t, err)
	require.Equal(t, raised, refreshed)

	_, reported, err := fixture.provisioner.GetCreditsUsed(ctx, fixture.orgID, KeyTypeChat)
	require.NoError(t, err)
	require.Equal(t, raised, reported, "the reported ceiling must follow the key, not the account type")
}

// TestGetCreditsUsed_ZeroKeyLimitFallsBackToPolicy covers the keys minted
// before the monthly_credits column carried a value. Those rows hold zero, and
// reporting it would tell the customer they have no credits at all.
func TestGetCreditsUsed_ZeroKeyLimitFallsBackToPolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newTrialCapFixture(t)

	_, err := repo.New(fixture.conn).CreateOpenRouterAPIKey(ctx, repo.CreateOpenRouterAPIKeyParams{
		OrganizationID: fixture.orgID,
		KeyType:        string(KeyTypeChat),
		Key:            conv.ToPGText("sk-or-legacy-zero"),
		KeyHash:        "hash-legacy",
		MonthlyCredits: 0,
	})
	require.NoError(t, err)

	_, reported, err := fixture.provisioner.GetCreditsUsed(ctx, fixture.orgID, KeyTypeChat)
	require.NoError(t, err)
	require.Equal(t, wantEnterpriseLimit, reported,
		"a key with no recorded ceiling must report the account-type amount")
}
