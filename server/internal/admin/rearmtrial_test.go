package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	srv "github.com/speakeasy-api/gram/server/gen/http/admin/server"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	audittestrepo "github.com/speakeasy-api/gram/server/internal/audit/audittest/repo"
	activitiesrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	orrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

// keyRevival is one call the handler made, with what the database looked like
// from outside its transaction then. The "seen" fields read through the pool.
type keyRevival struct {
	orgID   string
	keyType openrouter.KeyType
	limit   *int

	accountTypeSeen string
	demotedSeen     bool
}

// rearmProvisioner stands in for OpenRouter and can be made to fail on one key.
type rearmProvisioner struct {
	conn *pgxpool.Pool

	mu                       sync.Mutex
	revivals                 []keyRevival
	reconcileAttempts        []openrouter.KeyType
	conversionPolicyAttempts []openrouter.KeyType

	failOn openrouter.KeyType
	// failAfter is how a test reaches the post-commit recap.
	failAfter int
	failWith  error
}

var _ TrialKeyReviver = (*rearmProvisioner)(nil)

func (p *rearmProvisioner) RefreshAPIKeyLimit(ctx context.Context, orgID string, keyType openrouter.KeyType, limit *int) (int, error) {
	return p.refreshAPIKeyLimit(ctx, orgID, keyType, limit)
}

func (p *rearmProvisioner) ReinstateAPIKeyLimit(ctx context.Context, orgID string, keyType openrouter.KeyType, limit *int) (int, error) {
	return p.refreshAPIKeyLimit(ctx, orgID, keyType, limit)
}

func (p *rearmProvisioner) ReinstateAPIKeyLimitWithDB(ctx context.Context, _ openrouter.DBTX, orgID string, keyType openrouter.KeyType, limit *int) (int, error) {
	return p.refreshAPIKeyLimit(ctx, orgID, keyType, limit)
}

func (p *rearmProvisioner) RemoveAPIKeyDisableCauseWithDB(ctx context.Context, db openrouter.DBTX, orgID string, keyType openrouter.KeyType, cause openrouter.DisableCause, limit *int) (int, openrouter.DisableCauseChange, error) {
	q := orrepo.New(db)
	row, err := q.GetOpenRouterAPIKey(ctx, orrepo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(keyType)})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, openrouter.DisableCauseChange{}, nil
	}
	if err != nil {
		return 0, openrouter.DisableCauseChange{}, fmt.Errorf("get OpenRouter API key: %w", err)
	}
	if row.DisableCauses == nil {
		return 0, openrouter.DisableCauseChange{}, errors.New("unclassified key")
	}
	if !slices.Contains(row.DisableCauses, string(cause)) {
		return int(row.MonthlyCredits), openrouter.DisableCauseChange{}, nil
	}
	accessChanged := len(row.DisableCauses) == 1
	keyLimit := int(row.MonthlyCredits)
	if accessChanged && limit != nil {
		keyLimit = *limit
	}
	_, err = q.RemoveOpenRouterAPIKeyDisableCause(ctx, orrepo.RemoveOpenRouterAPIKeyDisableCauseParams{
		OrganizationID: orgID, KeyType: string(keyType), KeyHash: row.KeyHash, DisableCause: string(cause),
		MonthlyCredits: int64(keyLimit), UpdateMonthlyCredits: accessChanged && int64(keyLimit) != row.MonthlyCredits,
	})
	change := openrouter.DisableCauseChange{CauseChanged: true, KeyAccessChanged: accessChanged}
	if err != nil {
		return keyLimit, change, fmt.Errorf("remove OpenRouter API key disable cause: %w", err)
	}
	return keyLimit, change, nil
}

func (p *rearmProvisioner) PrepareEnterpriseTrialConversionKeyWithDB(ctx context.Context, db openrouter.DBTX, orgID string, keyType openrouter.KeyType, floor int64) (openrouter.EnterpriseTrialConversionKeyChange, error) {
	change, err := new(openrouter.OpenRouter).PrepareEnterpriseTrialConversionKeyWithDB(ctx, db, orgID, keyType, floor)
	if err != nil {
		return openrouter.EnterpriseTrialConversionKeyChange{}, fmt.Errorf("prepare enterprise conversion key: %w", err)
	}
	return change, nil

}

func (p *rearmProvisioner) ReconcileAPIKeyDisabled(ctx context.Context, orgID string, keyType openrouter.KeyType) error {
	p.mu.Lock()
	p.reconcileAttempts = append(p.reconcileAttempts, keyType)
	p.mu.Unlock()
	return p.reconcileAPIKey(ctx, orgID, keyType)
}

func (p *rearmProvisioner) ReconcileAPIKeyConversionPolicy(ctx context.Context, orgID string, keyType openrouter.KeyType) error {
	p.mu.Lock()
	p.conversionPolicyAttempts = append(p.conversionPolicyAttempts, keyType)
	p.mu.Unlock()
	return p.reconcileAPIKey(ctx, orgID, keyType)
}

func (p *rearmProvisioner) reconcileAPIKey(ctx context.Context, orgID string, keyType openrouter.KeyType) error {
	row, err := orrepo.New(p.conn).GetOpenRouterAPIKey(ctx, orrepo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(keyType)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get OpenRouter API key for reconciliation: %w", err)
	}
	_, err = p.refreshAPIKeyLimit(ctx, orgID, keyType, conv.PtrEmpty(int(row.MonthlyCredits)))
	return err
}

func (p *rearmProvisioner) refreshAPIKeyLimit(ctx context.Context, orgID string, keyType openrouter.KeyType, limit *int) (int, error) {
	accountType, demoted := "", false
	if p.conn != nil {
		accountType, demoted = readRearmState(ctx, p.conn, orgID)
	}

	p.mu.Lock()
	p.revivals = append(p.revivals, keyRevival{
		orgID:           orgID,
		keyType:         keyType,
		limit:           limit,
		accountTypeSeen: accountType,
		demotedSeen:     demoted,
	})
	calls := len(p.revivals)
	p.mu.Unlock()

	if p.failWith != nil && calls > p.failAfter && (p.failOn == "" || p.failOn == keyType) {
		return 0, p.failWith
	}

	// What the real RefreshAPIKeyLimit does locally.
	if p.conn != nil {
		if _, err := orrepo.New(p.conn).UpdateOpenRouterKey(ctx, orrepo.UpdateOpenRouterKeyParams{
			OrganizationID: orgID,
			KeyType:        string(keyType),
			MonthlyCredits: int64(conv.PtrValOr(limit, 0)),
			KeyHash:        "hash-" + orgID + "-" + string(keyType),
			Reinstate:      true,
		}); err != nil {
			return 0, fmt.Errorf("reinstate %s key: %w", keyType, err)
		}
	}

	return conv.PtrValOr(limit, 0), nil
}

// A nil ceiling is not a zero one: nil asks for the policy default.
func (p *rearmProvisioner) revivedLimits() map[openrouter.KeyType]*int {
	p.mu.Lock()
	defer p.mu.Unlock()

	limits := make(map[openrouter.KeyType]*int, len(p.revivals))
	for _, r := range p.revivals {
		limits[r.keyType] = r.limit
	}

	return limits
}

// readRearmState runs inside the handler's own goroutine, so it cannot use the
// require helpers: it answers zero values and lets the caller's assertions fail.
func readRearmState(ctx context.Context, conn *pgxpool.Pool, orgID string) (accountType string, demoted bool) {
	org, err := testrepo.New(conn).GetOrganizationMetadataStateFixture(ctx, orgID)
	if err != nil {
		return "", false
	}

	trial, err := trialsRepo.New(conn).GetTrial(ctx, orgID)
	if err != nil {
		return org.GramAccountType, false
	}

	return org.GramAccountType, trial.DemotedAt.Valid
}

func newTestAdminServiceWithOpenRouter(t *testing.T, provisioner TrialKeyReviver) (context.Context, *Service, *pgxpool.Pool) {
	t.Helper()

	ctx, svc, conn := newTestAdminService(t)
	svc.openRouter = provisioner

	return ctx, svc, conn
}

func newRearmService(t *testing.T) (context.Context, *Service, *pgxpool.Pool, *rearmProvisioner) {
	t.Helper()

	ctx, svc, conn := newTestAdminService(t)
	provisioner := &rearmProvisioner{conn: conn, mu: sync.Mutex{}, revivals: nil, failOn: "", failAfter: 0, failWith: nil}
	svc.openRouter = provisioner

	return ctx, svc, conn, provisioner
}

type keyFixture struct {
	keyType        openrouter.KeyType
	monthlyCredits int64
	disabled       bool
}

func seedOpenRouterKey(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, f keyFixture) {
	t.Helper()

	enc := testenv.NewEncryptionClient(t)
	ciphertext, err := enc.Encrypt([]byte("sk-test-" + orgID + "-" + string(f.keyType)))
	require.NoError(t, err)

	keys := orrepo.New(conn)
	_, err = keys.CreateOpenRouterAPIKey(ctx, orrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(f.keyType),
		KeyEncrypted:   conv.ToPGText(ciphertext),
		KeyHash:        "hash-" + orgID + "-" + string(f.keyType),
		MonthlyCredits: f.monthlyCredits,
	})
	require.NoError(t, err)

	if f.disabled {
		require.NoError(t, testrepo.New(conn).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{
			OrganizationID: orgID, KeyType: string(f.keyType), Disabled: true,
			DisableCauses: []string{string(openrouter.DisableCauseTrialDemotion)},
		}))
	}
}

func readOpenRouterKey(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, keyType openrouter.KeyType) orrepo.OpenrouterApiKey {
	t.Helper()

	row, err := orrepo.New(conn).GetOpenRouterAPIKey(ctx, orrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(keyType),
	})
	require.NoError(t, err)

	return row
}

// seedDemotedTrial leaves the organization where the demotion sweeper leaves it.
// The two key ceilings differ so a revival that passed the wrong one is visible.
func seedDemotedTrial(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, tier string) time.Time {
	t.Helper()

	endsAt := time.Now().UTC().Add(-10 * 24 * time.Hour)
	demotedAt := time.Now().UTC().Add(-9 * 24 * time.Hour)

	// id, name and slug all differ, so an assertion on one cannot pass on another.
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID + " Name", slug: orgID + "-slug", accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: tier, endsAt: endsAt, demotedAt: &demotedAt})
	seedArmAudit(t, ctx, conn, orgID)
	require.NoError(t, testrepo.New(conn).SeedTrialDemotionAuditFixture(ctx, orgID))
	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeChat, monthlyCredits: 50, disabled: true})
	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeInternal, monthlyCredits: 37, disabled: true})
	classifyRearmKey(t, ctx, conn, orgID, openrouter.KeyTypeChat, []string{string(openrouter.DisableCauseTrialDemotion)})
	classifyRearmKey(t, ctx, conn, orgID, openrouter.KeyTypeInternal, []string{string(openrouter.DisableCauseTrialDemotion)})

	return endsAt
}

func seedDisabledTrialRuntimeFeatures(t *testing.T, ctx context.Context, svc *Service, conn *pgxpool.Pool, orgID string) {
	t.Helper()
	q := featurerepo.New(conn)
	for _, feature := range productfeatures.TrialRuntimeFeatures {
		_, err := q.EnableFeature(ctx, featurerepo.EnableFeatureParams{
			OrganizationID: orgID,
			FeatureName:    string(feature),
		})
		require.NoError(t, err)
		_, err = q.DeleteFeature(ctx, featurerepo.DeleteFeatureParams{
			OrganizationID: orgID,
			FeatureName:    string(feature),
		})
		require.NoError(t, err)
		enabled, err := svc.productFeatures.IsFeatureEnabled(ctx, orgID, feature)
		require.NoError(t, err)
		require.False(t, enabled)
	}
}

type rearmUpstream struct {
	mu         sync.Mutex
	patches    []string
	fail       bool
	handlerErr error
}

func (u *rearmUpstream) record(body string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.patches = append(u.patches, body)
}

func (u *rearmUpstream) recordHandlerError(err error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.handlerErr == nil {
		u.handlerErr = err
	}
}

func (u *rearmUpstream) getHandlerError() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.handlerErr
}

func (u *rearmUpstream) setFail(fail bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.fail = fail
}

func (u *rearmUpstream) count() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.patches)
}

func newProductionRearmService(t *testing.T) (context.Context, *Service, *pgxpool.Pool, *rearmUpstream) {
	t.Helper()

	ctx, svc, conn := newTestAdminService(t)
	upstream := &rearmUpstream{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			upstream.recordHandlerError(fmt.Errorf("unexpected request method: %s", r.Method))
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			upstream.recordHandlerError(fmt.Errorf("read request body: %w", err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		upstream.record(string(body))
		upstream.mu.Lock()
		fail := upstream.fail
		upstream.mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		hash := r.URL.Path[len("/v1/keys/"):]
		if err := json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"hash": hash, "limit": 50}}); err != nil {
			upstream.recordHandlerError(fmt.Errorf("encode response: %w", err))
		}
	}))
	t.Cleanup(func() {
		server.Close()
		require.NoError(t, upstream.getHandlerError())
	})

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	option, err := openrouter.WithTestBaseURL(server.URL)
	require.NoError(t, err)
	svc.openRouter = openrouter.New(
		testenv.NewLogger(t), testenv.NewTracerProvider(t), policy, conn, "test", "provisioning-key",
		nil, nil, nil, testenv.NewEncryptionClient(t), option,
	)

	return ctx, svc, conn, upstream
}

func classifyRearmKey(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, keyType openrouter.KeyType, causes []string) {
	t.Helper()
	require.NoError(t, testrepo.New(conn).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{
		OrganizationID: orgID, KeyType: string(keyType), Disabled: len(causes) > 0, DisableCauses: causes,
	}))
}

func TestRearmTrial_RemovesOnlyTrialDemotionForBothKeyTypes(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, upstream := newProductionRearmService(t)
	const orgID = "org_rearm_causes"
	seedDemotedTrial(t, ctx, conn, orgID, "enterprise")
	for _, keyType := range openrouter.AllKeyTypes {
		classifyRearmKey(t, ctx, conn, orgID, keyType, []string{string(openrouter.DisableCauseTrialDemotion)})
	}

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	require.NoError(t, err)

	for _, keyType := range openrouter.AllKeyTypes {
		row := readOpenRouterKey(t, ctx, conn, orgID, keyType)
		require.Empty(t, row.DisableCauses)
		require.False(t, row.Disabled)
		require.EqualValues(t, 50, row.MonthlyCredits, "last-cause removal restores active-trial policy for %s", keyType)
	}
	require.Equal(t, len(openrouter.AllKeyTypes), upstream.count())
	entry, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialRearmed)
	require.NoError(t, err)
	var metadata map[string]any
	require.NoError(t, json.Unmarshal(entry.Metadata, &metadata))
	require.Equal(t, true, metadata["key_access_changed"])
}

func TestRearmTrial_PreservesLayeredProtectionAndCaps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		cause openrouter.DisableCause
		limit int64
	}{
		{name: "admin lock preserves zero cap", cause: openrouter.DisableCauseAdminLock, limit: 0},
		{name: "billing inactive preserves cap", cause: openrouter.DisableCauseBillingInactive, limit: 23},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, svc, conn, upstream := newProductionRearmService(t)
			orgID := "org_rearm_layered_" + string(tt.cause)
			seedDemotedTrial(t, ctx, conn, orgID, "enterprise")
			for _, keyType := range openrouter.AllKeyTypes {
				classifyRearmKey(t, ctx, conn, orgID, keyType, []string{string(tt.cause), string(openrouter.DisableCauseTrialDemotion)})
				row := readOpenRouterKey(t, ctx, conn, orgID, keyType)
				_, err := orrepo.New(conn).UpdateOpenRouterKey(ctx, orrepo.UpdateOpenRouterKeyParams{OrganizationID: orgID, KeyType: string(keyType), KeyHash: row.KeyHash, MonthlyCredits: tt.limit, Reinstate: false})
				require.NoError(t, err)
			}

			_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
			require.NoError(t, err)
			for _, keyType := range openrouter.AllKeyTypes {
				row := readOpenRouterKey(t, ctx, conn, orgID, keyType)
				require.Equal(t, []string{string(tt.cause)}, row.DisableCauses)
				require.True(t, row.Disabled)
				require.Equal(t, tt.limit, row.MonthlyCredits)
			}
			require.Zero(t, upstream.count(), "a remaining cause must keep protected local state without HTTP")
		})
	}
}

func TestRearmTrial_AbsentCauseHasNoKeySideEffectOrInventedAccessAudit(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, upstream := newProductionRearmService(t)
	const orgID = "org_rearm_absent_cause"
	seedDemotedTrial(t, ctx, conn, orgID, "enterprise")
	for _, keyType := range openrouter.AllKeyTypes {
		classifyRearmKey(t, ctx, conn, orgID, keyType, []string{string(openrouter.DisableCauseAdminLock)})
	}

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	require.NoError(t, err)
	require.Zero(t, upstream.count())
	for _, keyType := range openrouter.AllKeyTypes {
		row := readOpenRouterKey(t, ctx, conn, orgID, keyType)
		require.Equal(t, []string{string(openrouter.DisableCauseAdminLock)}, row.DisableCauses)
		require.True(t, row.Disabled)
	}

	entry, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialRearmed)
	require.NoError(t, err)
	var metadata map[string]any
	require.NoError(t, json.Unmarshal(entry.Metadata, &metadata))
	require.Equal(t, false, metadata["key_access_changed"])
}

func TestRearmTrial_UnclassifiedKeyRollsBackLifecycleAndAllKeyChanges(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, upstream := newProductionRearmService(t)
	const orgID = "org_rearm_null_causes"
	seedDemotedTrial(t, ctx, conn, orgID, "enterprise")
	seedDisabledTrialRuntimeFeatures(t, ctx, svc, conn, orgID)
	classifyRearmKey(t, ctx, conn, orgID, openrouter.KeyTypeChat, []string{string(openrouter.DisableCauseTrialDemotion)})
	classifyRearmKey(t, ctx, conn, orgID, openrouter.KeyTypeInternal, nil)
	beforeTrial := readTrial(t, ctx, conn, orgID)
	beforeAudit, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialRearmed)
	require.NoError(t, err)
	beforeOutbox, err := testrepo.New(conn).CountPublishOutboxRows(ctx)
	require.NoError(t, err)

	_, err = svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	requireOopsCode(t, err, oops.CodeUnexpected)
	require.Zero(t, upstream.count(), "no HTTP belongs inside the transaction or after rollback")
	afterTrial := readTrial(t, ctx, conn, orgID)
	require.Equal(t, beforeTrial.DemotedAt, afterTrial.DemotedAt)
	require.Equal(t, beforeTrial.EndsAt, afterTrial.EndsAt)
	require.Equal(t, []string{string(openrouter.DisableCauseTrialDemotion)}, readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat).DisableCauses)
	require.Nil(t, readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeInternal).DisableCauses)
	require.Equal(t, "free", readOrgState(t, ctx, conn, orgID).GramAccountType)
	for _, feature := range productfeatures.TrialRuntimeFeatures {
		enabled, featureErr := svc.productFeatures.IsFeatureEnabled(ctx, orgID, feature)
		require.NoError(t, featureErr)
		require.False(t, enabled)
	}
	afterAudit, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialRearmed)
	require.NoError(t, err)
	require.Equal(t, beforeAudit, afterAudit)
	afterOutbox, err := testrepo.New(conn).CountPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Equal(t, beforeOutbox, afterOutbox)
}

func seedArmAudit(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string) string {
	t.Helper()
	operationID, err := testrepo.New(conn).SeedTrialArmAuditFixture(ctx, orgID)
	require.NoError(t, err)
	return operationID
}

func latestArmAuditID(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string) string {
	t.Helper()
	operationID, err := testrepo.New(conn).GetLatestTrialArmAuditIDFixture(ctx, orgID)
	require.NoError(t, err)
	return operationID
}

func seedRearmAuditMetadata(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, metadata string) {
	t.Helper()
	err := testrepo.New(conn).SeedRearmAuditMetadataFixture(ctx, testrepo.SeedRearmAuditMetadataFixtureParams{
		OrganizationID: orgID,
		Metadata:       []byte(metadata),
	})
	require.NoError(t, err)
}

func TestRearmTrial_PriorCycleRearmAuditDoesNotAuthorizeCurrentCycleRetry(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, upstream := newProductionRearmService(t)
	const orgID = "org_rearm_old_generation"
	currentEndsAt := time.Now().UTC().Add(21 * 24 * time.Hour).Truncate(time.Microsecond)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "enterprise", whitelisted: true})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: "enterprise", endsAt: currentEndsAt})
	for _, keyType := range openrouter.AllKeyTypes {
		seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: keyType, monthlyCredits: 50, disabled: false})
	}
	armOperationID := seedArmAudit(t, ctx, conn, orgID)
	require.NoError(t, testrepo.New(conn).SeedTrialDemotionAuditFixture(ctx, orgID))
	seedRearmAuditMetadata(t, ctx, conn, orgID, fmt.Sprintf(`{"arm_operation_id":%q}`, armOperationID))
	require.NoError(t, testrepo.New(conn).SeedTrialDemotionAuditFixture(ctx, orgID))

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	requireOopsCode(t, err, oops.CodeConflict)
	require.Zero(t, upstream.count(), "an unrelated generation must not reconcile keys")
	require.True(t, currentEndsAt.Equal(readTrial(t, ctx, conn, orgID).EndsAt.Time))
}

func TestRearmTrial_MalformedOrMissingRetryMetadataIsRejected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metadata string
	}{
		{name: "missing arm operation", metadata: `{}`},
		{name: "malformed arm operation", metadata: `{"arm_operation_id":"not-a-uuid"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, svc, conn, upstream := newProductionRearmService(t)
			orgID := "org_rearm_bad_metadata_" + strings.ReplaceAll(tt.name, " ", "_")
			endsAt := time.Now().UTC().Add(14 * 24 * time.Hour).Truncate(time.Microsecond)
			seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "enterprise", whitelisted: true})
			seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: "enterprise", endsAt: endsAt})
			for _, keyType := range openrouter.AllKeyTypes {
				seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: keyType, monthlyCredits: 50, disabled: false})
			}
			seedArmAudit(t, ctx, conn, orgID)
			require.NoError(t, testrepo.New(conn).SeedTrialDemotionAuditFixture(ctx, orgID))
			seedRearmAuditMetadata(t, ctx, conn, orgID, tt.metadata)

			_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
			requireOopsCode(t, err, oops.CodeConflict)
			require.Zero(t, upstream.count())
			require.True(t, endsAt.Equal(readTrial(t, ctx, conn, orgID).EndsAt.Time))
		})
	}
}

func TestRearmTrial_MissingArmAuditIsRejected(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, upstream := newProductionRearmService(t)
	const orgID = "org_rearm_missing_arm_audit"
	endsAt := time.Now().UTC().Add(14 * 24 * time.Hour).Truncate(time.Microsecond)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "enterprise", whitelisted: true})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: "enterprise", endsAt: endsAt})
	seedRearmAuditMetadata(t, ctx, conn, orgID, `{"arm_operation_id":"00000000-0000-0000-0000-000000000001"}`)

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	requireOopsCode(t, err, oops.CodeConflict)
	require.Zero(t, upstream.count())
}

func TestRearmTrial_AmbiguousRetryAuditIsRejected(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, upstream := newProductionRearmService(t)
	const orgID = "org_rearm_ambiguous_audit"
	endsAt := time.Now().UTC().Add(14 * 24 * time.Hour).Truncate(time.Microsecond)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "enterprise", whitelisted: true})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: "enterprise", endsAt: endsAt})
	armOperationID := seedArmAudit(t, ctx, conn, orgID)
	require.NoError(t, testrepo.New(conn).SeedTrialDemotionAuditFixture(ctx, orgID))
	metadata := fmt.Sprintf(`{"arm_operation_id":%q}`, armOperationID)
	seedRearmAuditMetadata(t, ctx, conn, orgID, metadata)
	seedRearmAuditMetadata(t, ctx, conn, orgID, metadata)

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	requireOopsCode(t, err, oops.CodeConflict)
	require.Zero(t, upstream.count())
	require.True(t, endsAt.Equal(readTrial(t, ctx, conn, orgID).EndsAt.Time))
}

func TestRearmTrial_PostCommitReconcileFailureConvergesOnRequestRetry(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, upstream := newProductionRearmService(t)
	const orgID = "org_rearm_reconcile_retry"
	seedDemotedTrial(t, ctx, conn, orgID, "enterprise")
	upstream.setFail(true)
	beforeAudit, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialRearmed)
	require.NoError(t, err)
	beforeOutbox, err := testrepo.New(conn).CountPublishOutboxRows(ctx)
	require.NoError(t, err)

	_, err = svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	requireOopsCode(t, err, oops.CodeGatewayError)
	require.False(t, readTrial(t, ctx, conn, orgID).DemotedAt.Valid, "the local transaction commits before HTTP reconciliation")
	for _, keyType := range openrouter.AllKeyTypes {
		require.Empty(t, readOpenRouterKey(t, ctx, conn, orgID, keyType).DisableCauses)
	}

	committedEndsAt := readTrial(t, ctx, conn, orgID).EndsAt
	rearmAudit, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialRearmed)
	require.NoError(t, err)
	var retryMetadata struct {
		ArmOperationID string `json:"arm_operation_id"`
	}
	require.NoError(t, json.Unmarshal(rearmAudit.Metadata, &retryMetadata))
	require.Equal(t, latestArmAuditID(t, ctx, conn, orgID), retryMetadata.ArmOperationID)

	_, err = svc.ExtendTrial(ctx, &gen.ExtendTrialPayload{ID: orgID, Days: 2})
	require.NoError(t, err)
	extendedEndsAt := readTrial(t, ctx, conn, orgID).EndsAt
	require.True(t, extendedEndsAt.Time.After(committedEndsAt.Time), "extension must move the same trial generation forward")

	upstream.setFail(false)
	result, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 365})
	require.NoError(t, err)
	require.Equal(t, orgID, result.ID)
	require.Equal(t, extendedEndsAt, readTrial(t, ctx, conn, orgID).EndsAt, "retry must not replay the lifecycle window")
	afterAudit, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialRearmed)
	require.NoError(t, err)
	require.Equal(t, beforeAudit+1, afterAudit, "retry must not invent another lifecycle audit event")
	afterOutbox, err := testrepo.New(conn).CountPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Equal(t, beforeOutbox+2, afterOutbox, "extension and initial re-arm each emit once; retry must not emit")
}

func TestRearmTrial_RecreatedGenerationWithSameCreatedAtRejectsStaleRetry(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, upstream := newProductionRearmService(t)
	const orgID = "org_rearm_recreated_generation"
	seedDemotedTrial(t, ctx, conn, orgID, "enterprise")
	upstream.setFail(true)

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	requireOopsCode(t, err, oops.CodeGatewayError)
	committed := readTrial(t, ctx, conn, orgID)
	staleArmOperationID := latestArmAuditID(t, ctx, conn, orgID)

	err = testrepo.New(conn).RecreateTrialGenerationFixture(ctx, testrepo.RecreateTrialGenerationFixtureParams{
		TargetOrganizationID: orgID,
		Tier:                 "enterprise",
		CreatedAt:            committed.CreatedAt,
		EndsAt:               committed.EndsAt,
	})
	require.NoError(t, err)
	currentArmOperationID := seedArmAudit(t, ctx, conn, orgID)
	require.NotEqual(t, staleArmOperationID, currentArmOperationID)
	require.Equal(t, committed.CreatedAt, readTrial(t, ctx, conn, orgID).CreatedAt, "fixture must hold timestamp precision constant")

	requestsBeforeRetry := upstream.count()
	upstream.setFail(false)
	_, err = svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	requireOopsCode(t, err, oops.CodeConflict)
	require.Equal(t, requestsBeforeRetry, upstream.count(), "a stale generation must not reconcile keys")
}

func TestRearmTrial_RetryUsesOnlyCurrentDemotionCycle(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, upstream := newProductionRearmService(t)
	const orgID = "org_rearm_current_demotion_cycle"
	seedDemotedTrial(t, ctx, conn, orgID, "enterprise")

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	require.NoError(t, err)
	firstCyclePatches := upstream.count()
	require.Positive(t, firstCyclePatches)

	require.NoError(t, testrepo.New(conn).RedemoteTrialLifecycleFixture(ctx, orgID))
	require.NoError(t, testrepo.New(conn).SeedTrialDemotionAuditFixture(ctx, orgID))
	for _, keyType := range openrouter.AllKeyTypes {
		classifyRearmKey(t, ctx, conn, orgID, keyType, []string{string(openrouter.DisableCauseTrialDemotion)})
	}

	upstream.setFail(true)
	_, err = svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	requireOopsCode(t, err, oops.CodeGatewayError)
	failedCyclePatches := upstream.count()
	require.Greater(t, failedCyclePatches, firstCyclePatches)
	require.False(t, readTrial(t, ctx, conn, orgID).DemotedAt.Valid, "re-arm commit must survive its post-commit reconciliation failure")

	upstream.setFail(false)
	_, err = svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	require.NoError(t, err)
	require.Greater(t, upstream.count(), failedCyclePatches, "retry must reconcile the committed current cycle")
}

func TestRearmTrial_RestoresTheOrganizationAndRevivesEveryKey(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, provisioner := newRearmService(t)
	seedDemotedTrial(t, ctx, conn, "org_rearm", "enterprise")
	seedDisabledTrialRuntimeFeatures(t, ctx, svc, conn, "org_rearm")
	beforeTrial := readTrial(t, ctx, conn, "org_rearm")
	beforeOrg := readOrgState(t, ctx, conn, "org_rearm")

	res, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: "org_rearm", Days: 14})
	require.NoError(t, err)
	require.Equal(t, "org_rearm", res.ID)

	require.Len(t, provisioner.revivals, len(openrouter.AllKeyTypes))
	require.Equal(t, map[openrouter.KeyType]*int{
		openrouter.KeyTypeChat:     conv.PtrEmpty(50),
		openrouter.KeyTypeInternal: conv.PtrEmpty(50),
	}, provisioner.revivedLimits(), "every key type the demotion disables must come back up on its own ceiling")
	require.False(t, readOpenRouterKey(t, ctx, conn, "org_rearm", openrouter.KeyTypeChat).Disabled)
	require.False(t, readOpenRouterKey(t, ctx, conn, "org_rearm", openrouter.KeyTypeInternal).Disabled)

	// Demotion cleared both; the signup arming path only ever writes the first.
	state := readOrgState(t, ctx, conn, "org_rearm")
	require.Equal(t, "enterprise", state.GramAccountType)
	require.True(t, state.Whitelisted, "a re-armed organization must be whitelisted again")
	for _, feature := range productfeatures.TrialRuntimeFeatures {
		enabled, err := svc.productFeatures.IsFeatureEnabled(ctx, "org_rearm", feature)
		require.NoError(t, err)
		require.Truef(t, enabled, "re-arm should restore %s", feature)
	}

	after := readTrial(t, ctx, conn, "org_rearm")
	require.False(t, after.DemotedAt.Valid, "re-arming must clear demoted_at")
	require.False(t, after.ConvertedAt.Valid)
	require.WithinDuration(t, time.Now().UTC().Add(14*24*time.Hour), after.EndsAt.Time, time.Minute)

	// The rejection tests assert updated_at holds still; this is the other half.
	require.True(t, after.UpdatedAt.Time.After(beforeTrial.UpdatedAt.Time),
		"a re-arm must stamp the trial's updated_at: was %s, now %s", beforeTrial.UpdatedAt.Time, after.UpdatedAt.Time)
	require.True(t, state.UpdatedAt.Time.After(beforeOrg.UpdatedAt.Time),
		"restoring the organization must stamp its updated_at: was %s, now %s", beforeOrg.UpdatedAt.Time, state.UpdatedAt.Time)

	require.NotNil(t, res.TrialEndsAt)
	detail, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: "org_rearm"})
	require.NoError(t, err)
	require.Equal(t, "running", *detail.TrialState)
	require.Equal(t, *res.TrialEndsAt, *detail.TrialEndsAt)
	require.Equal(t, "enterprise", detail.AccountType)
}

func TestRearmTrial_DoesNotRestartLoopsSequence(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	notifier := &fakeTrialNotifier{}
	svc.trial = notifier
	seedDemotedTrial(t, ctx, conn, "org_rearm_loops", "enterprise")

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: "org_rearm_loops", Days: 14})
	require.NoError(t, err)
	require.Empty(t, notifier.started)
	require.Empty(t, notifier.inactive)
}

func TestOpenRouterKeyLockProbeIgnoresSoftDeletedRows(t *testing.T) {
	t.Parallel()

	ctx, _, conn, _ := newRearmService(t)
	const orgID = "org_rearm_probe_deleted"
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "enterprise", whitelisted: true})
	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeChat, disabled: true})
	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeInternal, disabled: true})
	fixtures := testrepo.New(conn)
	require.NoError(t, fixtures.SoftDeleteOpenRouterAPIKeyFixture(ctx, testrepo.SoftDeleteOpenRouterAPIKeyFixtureParams{
		OrganizationID: orgID, KeyType: string(openrouter.KeyTypeInternal),
	}))

	deletedRowLock := testenv.BeginTx(t, ctx, conn)
	_, err := testrepo.New(deletedRowLock).LockOpenRouterAPIKeyForUpdateFixture(ctx, testrepo.LockOpenRouterAPIKeyForUpdateFixtureParams{
		OrganizationID: orgID, KeyType: string(openrouter.KeyTypeInternal),
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, deletedRowLock.Rollback(ctx)) }()

	activeCauses, err := testrepo.New(conn).ListOpenRouterAPIKeyDisableCausesForUpdateNowaitFixture(ctx, orgID)
	require.NoError(t, err, "soft-deleted key must not participate in the lock probe")
	require.Equal(t, [][]string{{"trial_demotion"}}, activeCauses)
}

func TestRearmTrial_LocksLifecycleBeforeAllKeyLocksAndRows(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newProductionRearmService(t)
	const orgID = "org_rearm_lock_order"
	seedDemotedTrial(t, ctx, conn, orgID, "enterprise")

	rowLock := testenv.BeginTx(t, ctx, conn)
	_, err := trialsRepo.New(rowLock).LockTrialLifecycle(ctx, orgID)
	require.NoError(t, err)

	rearmed := make(chan error, 1)
	go func() {
		_, callErr := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
		rearmed <- callErr
	}()

	waitCtx, cancelWait := context.WithTimeout(ctx, 2*time.Second)
	defer cancelWait()
	requireAdminCondition(t, waitCtx, conn, func(check context.Context) (bool, error) {
		blocked, err := testrepo.New(conn).IsQueryBlockedOnLockFixture(check, "%SELECT tier, ends_at, converted_at, demoted_at%")
		if err != nil {
			return false, fmt.Errorf("check blocked trial re-arm query: %w", err)
		}
		return blocked, nil
	}, "re-arm did not block on the lifecycle row")

	probe, err := conn.Acquire(ctx)
	require.NoError(t, err)
	defer probe.Release()
	for _, keyType := range openrouter.AllKeyTypes {
		acquired, err := testrepo.New(probe).TryAcquireOpenRouterKeyBillingLockFixture(ctx, testrepo.TryAcquireOpenRouterKeyBillingLockFixtureParams{
			KeyType: string(keyType), OrganizationID: orgID,
		})
		require.NoError(t, err)
		require.Truef(t, acquired, "%s lock must remain available while lifecycle is blocked", keyType)
		unlocked, err := activitiesrepo.New(probe).ReleaseOpenRouterKeyBillingLock(ctx, activitiesrepo.ReleaseOpenRouterKeyBillingLockParams{
			OrganizationID: orgID, KeyType: string(keyType),
		})
		require.NoError(t, err)
		require.True(t, unlocked)
	}

	internalLock, err := conn.Acquire(ctx)
	require.NoError(t, err)
	defer internalLock.Release()
	internalParams := activitiesrepo.AcquireOpenRouterKeyBillingLockParams{OrganizationID: orgID, KeyType: string(openrouter.KeyTypeInternal)}
	err = activitiesrepo.New(internalLock).AcquireOpenRouterKeyBillingLock(ctx, internalParams)
	require.NoError(t, err)
	internalHeld := true
	defer func() {
		if internalHeld {
			_, _ = activitiesrepo.New(internalLock).ReleaseOpenRouterKeyBillingLock(context.WithoutCancel(ctx), activitiesrepo.ReleaseOpenRouterKeyBillingLockParams(internalParams))
		}
	}()

	require.NoError(t, rowLock.Commit(ctx))
	chatCtx, cancelChat := context.WithTimeout(ctx, 2*time.Second)
	defer cancelChat()
	requireAdminCondition(t, chatCtx, conn, func(check context.Context) (bool, error) {
		acquired, err := testrepo.New(probe).TryAcquireOpenRouterKeyBillingLockFixture(check, testrepo.TryAcquireOpenRouterKeyBillingLockFixtureParams{
			KeyType: string(openrouter.KeyTypeChat), OrganizationID: orgID,
		})
		if err != nil {
			return false, fmt.Errorf("probe chat billing lock: %w", err)
		}
		if !acquired {
			return true, nil
		}
		_, err = activitiesrepo.New(probe).ReleaseOpenRouterKeyBillingLock(check, activitiesrepo.ReleaseOpenRouterKeyBillingLockParams{
			OrganizationID: orgID, KeyType: string(openrouter.KeyTypeChat),
		})
		if err != nil {
			return false, fmt.Errorf("release chat billing lock probe: %w", err)
		}
		return false, nil
	}, "chat lock was not acquired before the blocked internal lock")

	keyProbe := testenv.BeginTx(t, ctx, conn)
	causesByKey, err := testrepo.New(keyProbe).ListOpenRouterAPIKeyDisableCausesForUpdateNowaitFixture(ctx, orgID)
	require.NoError(t, err, "key rows must remain unlocked until every advisory lock is held")
	for _, causes := range causesByKey {
		require.Equal(t, []string{string(openrouter.DisableCauseTrialDemotion)}, causes)
	}
	require.Len(t, causesByKey, len(openrouter.AllKeyTypes))
	require.NoError(t, keyProbe.Rollback(ctx))

	unlocked, err := activitiesrepo.New(internalLock).ReleaseOpenRouterKeyBillingLock(ctx, activitiesrepo.ReleaseOpenRouterKeyBillingLockParams(internalParams))
	require.NoError(t, err)
	require.True(t, unlocked)
	internalHeld = false

	select {
	case err := <-rearmed:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		require.FailNow(t, "re-arm did not finish after releasing locks")
	}
}

func requireAdminCondition(t *testing.T, ctx context.Context, _ *pgxpool.Pool, condition func(context.Context) (bool, error), message string) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		met, err := condition(ctx)
		require.NoError(t, err)
		if met {
			return
		}
		select {
		case <-ctx.Done():
			require.FailNow(t, message, ctx.Err().Error())
		case <-ticker.C:
		}
	}
}

// MarkTrialDemoted only demotes an already-past ends_at, so a re-arm that
// cleared the stamp and left the date would be re-demoted on the next sweep.
// The trial here ended 100 days ago, so a date computed from it is still past.
func TestRearmTrial_MovesEndsAtIntoTheFuture(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)

	orgID := "org_rearm_stale"
	longExpired := time.Now().UTC().Add(-100 * 24 * time.Hour)
	demotedAt := time.Now().UTC().Add(-99 * 24 * time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, endsAt: longExpired, demotedAt: &demotedAt})
	seedArmAudit(t, ctx, conn, orgID)

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 3})
	require.NoError(t, err)

	after := readTrial(t, ctx, conn, orgID)
	require.True(t, after.EndsAt.Time.After(time.Now().UTC()),
		"a re-armed trial must end in the future or the next sweep demotes it again: ends %s", after.EndsAt.Time)
	require.WithinDuration(t, time.Now().UTC().Add(3*24*time.Hour), after.EndsAt.Time, time.Minute,
		"the new end date is counted from now, not added to the old one")

	expired, err := trialsRepo.New(conn).ListExpiredTrials(ctx)
	require.NoError(t, err)
	require.NotContains(t, expired, orgID, "a re-armed trial must not be due for demotion again")

	_, err = svc.ExtendTrial(ctx, &gen.ExtendTrialPayload{ID: orgID, Days: 5})
	require.NoError(t, err, "a re-armed trial must be extendable")
}

func TestRearmTrial_OnlyADemotedTrialCanBeRearmed(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)

	now := time.Now().UTC()
	convertedAt := now.Add(-96 * time.Hour)
	demotedAt := now.Add(-72 * time.Hour)

	cases := []struct {
		name      string
		orgID     string
		expired   bool
		converted bool
		demoted   bool
		wantRearm bool
	}{
		{name: "demoted and expired", orgID: "org_re_demoted_expired", demoted: true, expired: true, wantRearm: true},
		{name: "demoted", orgID: "org_re_demoted", demoted: true, wantRearm: true},

		// A conversion can land after a demotion, so this is not implied.
		{name: "converted after demotion", orgID: "org_re_both", converted: true, demoted: true},
		{name: "converted after demotion and expired", orgID: "org_re_both_expired", converted: true, demoted: true, expired: true},

		{name: "running", orgID: "org_re_running"},
		{name: "expired not yet demoted", orgID: "org_re_expired", expired: true},
		{name: "converted", orgID: "org_re_converted", converted: true},
		{name: "converted and expired", orgID: "org_re_converted_expired", converted: true, expired: true},
	}

	for _, tc := range cases {
		endsAt := now.Add(10 * 24 * time.Hour)
		if tc.expired {
			endsAt = now.Add(-10 * 24 * time.Hour)
		}
		f := trialFixture{orgID: tc.orgID, endsAt: endsAt}
		if tc.converted {
			f.convertedAt = &convertedAt
		}
		if tc.demoted {
			f.demotedAt = &demotedAt
		}

		seedOrg(t, ctx, conn, orgFixture{id: tc.orgID, name: tc.orgID, slug: tc.orgID, accountType: "free", whitelisted: false})
		seedTrial(t, ctx, conn, f)
		seedArmAudit(t, ctx, conn, tc.orgID)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			before := readTrial(t, ctx, conn, tc.orgID)

			_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: tc.orgID, Days: 14})

			after := readTrial(t, ctx, conn, tc.orgID)
			if tc.wantRearm {
				require.NoError(t, err)
				require.False(t, after.DemotedAt.Valid)
				require.True(t, after.EndsAt.Time.After(now))
				return
			}

			requireOopsCode(t, err, oops.CodeConflict)
			require.Equal(t, before.EndsAt.Time, after.EndsAt.Time,
				"a trial that is not demoted must not be re-armed: ends_at was %s, now %s", before.EndsAt.Time, after.EndsAt.Time)
			require.Equal(t, before.DemotedAt.Time, after.DemotedAt.Time)
			require.Equal(t, before.ConvertedAt.Time, after.ConvertedAt.Time)
			require.Equal(t, before.UpdatedAt.Time, after.UpdatedAt.Time, "a rejected re-arm must not touch updated_at")

			// A rejection must not whitelist a converted customer.
			state := readOrgState(t, ctx, conn, tc.orgID)
			require.Equal(t, "free", state.GramAccountType)
			require.False(t, state.Whitelisted)
		})
	}
}

// The state-space test above has no key rows. Hoisting the revival above the
// UPDATE would switch a converted customer's keys on before answering 409.
func TestRearmTrial_ARejectedRearmRevivesNoKeys(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, provisioner := newRearmService(t)

	now := time.Now().UTC()
	convertedAt := now.Add(-96 * time.Hour)
	demotedAt := now.Add(-72 * time.Hour)

	// Including the running trial, which is not a state the application reaches.
	seedRejectedRearm := func(orgID string, f trialFixture) {
		t.Helper()

		f.orgID = orgID
		seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID + " Name", slug: orgID + "-slug", accountType: "free", whitelisted: false})
		seedTrial(t, ctx, conn, f)
		seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeChat, monthlyCredits: 50, disabled: true})
		seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeInternal, monthlyCredits: 37, disabled: true})
	}

	seedRejectedRearm("org_rearm_reject_converted", trialFixture{endsAt: now.Add(-10 * 24 * time.Hour), convertedAt: &convertedAt, demotedAt: &demotedAt})
	seedRejectedRearm("org_rearm_reject_running", trialFixture{endsAt: now.Add(10 * 24 * time.Hour)})

	cases := []struct {
		name  string
		orgID string
		want  oops.Code
	}{
		{name: "converted after demotion", orgID: "org_rearm_reject_converted", want: oops.CodeConflict},
		{name: "running trial", orgID: "org_rearm_reject_running", want: oops.CodeConflict},
		{name: "unknown organization", orgID: "org_rearm_reject_missing", want: oops.CodeNotFound},
	}

	// Not subtests: a parallel sibling would still be writing the fake's history.
	for _, tc := range cases {
		_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: tc.orgID, Days: 14})
		requireOopsCode(t, err, tc.want)
		require.Empty(t, provisioner.revivedLimits(),
			"re-arming %s was rejected, so it must not have touched any model provider key", tc.name)
	}

	for _, orgID := range []string{"org_rearm_reject_converted", "org_rearm_reject_running"} {
		for _, keyType := range openrouter.AllKeyTypes {
			require.True(t, readOpenRouterKey(t, ctx, conn, orgID, keyType).Disabled,
				"%s must still have its %s key switched off after a rejected re-arm", orgID, keyType)
		}
	}
}

// A hardcoded 'enterprise' account type passes every other test in this file.
func TestRearmTrial_RestoresTheTierTheTrialGrants(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	seedDemotedTrial(t, ctx, conn, "org_rearm_tier", "pro")

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: "org_rearm_tier", Days: 14})
	require.NoError(t, err)

	state := readOrgState(t, ctx, conn, "org_rearm_tier")
	require.Equal(t, "pro", state.GramAccountType,
		"the restored account type must be the trial's tier, not a hardcoded enterprise")
	require.True(t, state.Whitelisted)
}

// This and TestRearmTrial_OrganizationWithNoTrialRow keep the two causes of a
// zero-row update apart: a wrong id reports a missing organization, not a trial.
func TestRearmTrial_UnknownAndMalformedOrganizationIDs(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)

	// Its slug is one of the ids tried below: the lookup must resolve ids only.
	seedDemotedTrial(t, ctx, conn, "org_rearm_bystander", "enterprise")
	before := readTrial(t, ctx, conn, "org_rearm_bystander")

	for _, id := range []string{"org_rearm_missing", "", "org_rearm_bystander-slug", "not a valid id"} {
		_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: id, Days: 14})
		requireOopsCode(t, err, oops.CodeNotFound)
	}

	after := readTrial(t, ctx, conn, "org_rearm_bystander")
	require.True(t, after.DemotedAt.Valid, "an unmatched id must not re-arm anything")
	require.Equal(t, before.EndsAt.Time, after.EndsAt.Time)
	require.Equal(t, before.UpdatedAt.Time, after.UpdatedAt.Time)
}

func TestRearmTrial_OrganizationWithNoTrialRow(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_rearm_no_trial", name: "No Trial", slug: "no-trial-rearm", accountType: "free"})

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: "org_rearm_no_trial", Days: 14})
	requireOopsCode(t, err, oops.CodeConflict)
	// Extend and re-arm share one rejection helper, so each has to name its own
	// message or the two can be swapped without a test noticing.
	require.ErrorContains(t, err, "organization has no demoted enterprise trial to re-arm")

	// Re-arm must never be a way to grant a trial; that is the auth flow's job.
	_, err = trialsRepo.New(conn).GetTrial(ctx, "org_rearm_no_trial")
	require.Error(t, err, "a rejected re-arm must not create a trial row")

	state := readOrgState(t, ctx, conn, "org_rearm_no_trial")
	require.Equal(t, "free", state.GramAccountType)
	require.False(t, state.Whitelisted)
}

func TestRearmTrial_ReleasesRejectedTransactionBeforeClassificationLookup(t *testing.T) { //nolint:paralleltest // The regression deliberately constrains the shared pool to three connections.
	ctx, svc, conn, provisioner := newRearmService(t)
	orgID := "org_rearm_limited_pool"
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: "No Trial", slug: "limited-pool-rearm", accountType: "free"})

	poolConfig := conn.Config().Copy()
	poolConfig.MinConns = 0
	poolConfig.MaxConns = 3 // two key locks plus the trial transaction
	limited, err := pgxpool.NewWithConfig(ctx, poolConfig)
	require.NoError(t, err)
	t.Cleanup(limited.Close)
	svc.db = limited
	provisioner.conn = limited

	requestCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	_, err = svc.RearmTrial(requestCtx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	requireOopsCode(t, err, oops.CodeConflict)
}

// DisableAPIKey no-ops on a missing key row but RefreshAPIKeyLimit errors on
// one, so the demotion's unconditional loop would fail this re-arm outright.
func TestRearmTrial_OrganizationWithNoKeysSucceeds(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newProductionRearmService(t)

	orgID := "org_rearm_nokeys"
	endsAt := time.Now().UTC().Add(-10 * 24 * time.Hour)
	demotedAt := time.Now().UTC().Add(-9 * 24 * time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, endsAt: endsAt, demotedAt: &demotedAt})
	seedArmAudit(t, ctx, conn, orgID)

	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeChat, monthlyCredits: 50, disabled: true})

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	require.NoError(t, err, "an organization with no key of a type must still be re-armable")

	chat := readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat)
	require.Empty(t, chat.DisableCauses)
	require.False(t, chat.Disabled)
	require.EqualValues(t, 50, chat.MonthlyCredits)
	_, err = orrepo.New(conn).GetOpenRouterAPIKey(ctx, orrepo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(openrouter.KeyTypeInternal)})
	require.ErrorIs(t, err, pgx.ErrNoRows, "missing key types remain safe and absent")

	state := readOrgState(t, ctx, conn, orgID)
	require.Equal(t, "enterprise", state.GramAccountType)
	require.True(t, state.Whitelisted)
}

// The sibling test omits the last of AllKeyTypes, so an implementation that
// stopped at the first missing row would still pass it. Here it is the first.
func TestRearmTrial_OrganizationWithOnlyAnInternalKeySucceeds(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newProductionRearmService(t)

	orgID := "org_rearm_internal_only"
	endsAt := time.Now().UTC().Add(-10 * 24 * time.Hour)
	demotedAt := time.Now().UTC().Add(-9 * 24 * time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, endsAt: endsAt, demotedAt: &demotedAt})
	seedArmAudit(t, ctx, conn, orgID)
	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeInternal, monthlyCredits: 37, disabled: true})

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	require.NoError(t, err)

	internal := readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeInternal)
	require.Empty(t, internal.DisableCauses)
	require.False(t, internal.Disabled)
	require.EqualValues(t, 50, internal.MonthlyCredits)
	_, err = orrepo.New(conn).GetOpenRouterAPIKey(ctx, orrepo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(openrouter.KeyTypeChat)})
	require.ErrorIs(t, err, pgx.ErrNoRows, "a missing first key must not stop processing the second")
}

// A live key needs no round trip, which is what makes retrying a re-arm cheap.
func TestRearmTrial_AlreadyEnabledKeyIsLeftAlone(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, provisioner := newRearmService(t)

	orgID := "org_rearm_partial"
	endsAt := time.Now().UTC().Add(-10 * 24 * time.Hour)
	demotedAt := time.Now().UTC().Add(-9 * 24 * time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, endsAt: endsAt, demotedAt: &demotedAt})
	seedArmAudit(t, ctx, conn, orgID)
	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeChat, monthlyCredits: 50, disabled: false})
	seedOpenRouterKey(t, ctx, conn, orgID, keyFixture{keyType: openrouter.KeyTypeInternal, monthlyCredits: 37, disabled: true})

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	require.NoError(t, err)

	//nolint:exhaustive // the omitted key type is the assertion: it was already live
	require.Equal(t, map[openrouter.KeyType]*int{openrouter.KeyTypeInternal: conv.PtrEmpty(50)}, provisioner.revivedLimits(),
		"only the key carrying trial_demotion needs reconciliation")
	require.False(t, readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat).Disabled)
	require.False(t, readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeInternal).Disabled)
}

func TestRearmTrial_WritesAnAuditEntry(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	seedDemotedTrial(t, ctx, conn, "org_rearm_audit", "enterprise")

	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID:   "session-rearm-audit",
		Email:       "operator@example.test",
		OIDCSubject: "oidc-subject-rearm-audit",
		Name:        "Test Operator",
		HD:          "example.test",
	})

	before, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialRearmed)
	require.NoError(t, err)

	_, err = svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: "org_rearm_audit", Days: 14})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialRearmed)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	entry, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialRearmed)
	require.NoError(t, err)
	require.Equal(t, "organization", entry.SubjectType)

	// id, name and slug all differ in the fixture, so these pin the right one.
	require.Equal(t, "org_rearm_audit Name", entry.SubjectDisplay)
	require.Equal(t, "org_rearm_audit-slug", entry.SubjectSlug)
	require.NotNil(t, entry.ActorDisplayName, "the entry must name who re-armed the trial")
	require.Equal(t, "Test Operator", *entry.ActorDisplayName)
	require.NotNil(t, entry.ActingSurface)
	require.Equal(t, string(audit.SurfaceAdmin), *entry.ActingSurface)

	// Comparing this with the demotion's entry shows whether the tier came back.
	var metadata struct {
		AccountType string    `json:"account_type"`
		TrialEndsAt time.Time `json:"trial_ends_at"`
	}
	require.NoError(t, json.Unmarshal(entry.Metadata, &metadata))
	require.Equal(t, "enterprise", metadata.AccountType)

	// The date the database wrote, not one recomputed from a second clock.
	require.WithinDuration(t, readTrial(t, ctx, conn, "org_rearm_audit").EndsAt.Time, metadata.TrialEndsAt, 0)

	// The trial lifecycle event; any other would deliver this to nobody.
	_, err = audittestrepo.New(conn).GetLatestOutboxPayloadByOrg(ctx, audittestrepo.GetLatestOutboxPayloadByOrgParams{
		OrganizationID: "org_rearm_audit",
		EventType:      string(events.OrganizationEnterpriseTrialV1.EventType()),
	})
	require.NoError(t, err, "a re-arm must enqueue an outbox entry on the enterprise trial event")
}

func TestRearmTrial_AuditEntryNamesTheOperator(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	seedDemotedTrial(t, ctx, conn, "org_rearm_actor", "enterprise")

	const operatorEmail = "operator@example.test"
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID:   "session-rearm-actor",
		Email:       operatorEmail,
		OIDCSubject: "oidc-subject-rearm-actor",
		Name:        "Test Operator",
		HD:          "example.test",
	})

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: "org_rearm_actor", Days: 14})
	require.NoError(t, err)

	entry, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialRearmed)
	require.NoError(t, err)

	require.NotNil(t, entry.ActorDisplayName)
	require.Equal(t, "Test Operator", *entry.ActorDisplayName)
	require.NotNil(t, entry.ActingSurface)
	require.Equal(t, string(audit.SurfaceAdmin), *entry.ActingSurface)

	// Without this, an entry naming nobody at all would satisfy the one above.
	require.Equal(t, "oidc-subject-rearm-actor", entry.ActorID,
		"the entry must still record which operator acted, in the field the customer's feed does not render")
	require.Equal(t, "user", entry.ActorType)

	// The subject is opaque, so it is not the email in another shape.
	for name, field := range map[string]string{
		"actor display name": conv.PtrValOr(entry.ActorDisplayName, ""),
		"actor slug":         entry.ActorSlug,
		"actor id":           entry.ActorID,
		"subject display":    entry.SubjectDisplay,
		"subject slug":       entry.SubjectSlug,
		"metadata":           string(entry.Metadata),
		"before snapshot":    string(entry.BeforeSnapshot),
		"after snapshot":     string(entry.AfterSnapshot),
	} {
		require.NotContains(t, field, operatorEmail, "the operator's email must not reach the customer's audit feed through the %s", name)
	}
}

func TestRearmTrial_DayCountBounds(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)

	cases := []struct {
		name    string
		days    int
		wantErr bool
	}{
		// A zero or negative count would re-arm a trial that is already over.
		{name: "large negative", days: -365, wantErr: true},
		{name: "minus one", days: -1, wantErr: true},
		{name: "zero", days: 0, wantErr: true},

		{name: "minimum", days: constants.MinTrialRearmDays},
		{name: "maximum", days: constants.MaxTrialRearmDays},

		{name: "one past the maximum", days: constants.MaxTrialRearmDays + 1, wantErr: true},
		{name: "far past the maximum", days: 100000, wantErr: true},

		// 1<<32 + 1 narrows to exactly 1, so a handler that narrowed before
		// checking would accept it and re-arm for a day.
		{name: "int32 overflow to a negative", days: math.MaxInt32 + 1, wantErr: true},
		{name: "int32 overflow to a valid day count", days: math.MaxUint32 + 2, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			orgID := "org_rearm_bound_" + tc.name
			seededEndsAt := seedDemotedTrial(t, ctx, conn, orgID, "enterprise")

			_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: tc.days})

			after := readTrial(t, ctx, conn, orgID)
			if tc.wantErr {
				requireOopsCode(t, err, oops.CodeInvalid)
				require.True(t, after.DemotedAt.Valid, "a rejected day count must leave the trial demoted")
				require.WithinDuration(t, seededEndsAt, after.EndsAt.Time, time.Second)
				require.Equal(t, "free", readOrgState(t, ctx, conn, orgID).GramAccountType)
				return
			}

			require.NoError(t, err)
			require.WithinDuration(t, time.Now().UTC().Add(time.Duration(tc.days)*24*time.Hour), after.EndsAt.Time, time.Minute)
		})
	}
}

// The other copy of the bounds. Every other test calls svc.RearmTrial directly,
// so deleting Minimum, Maximum or MinLength(1) leaves all of them green.
func TestRearmTrialRequestBody_DesignBoundsAreEnforced(t *testing.T) {
	t.Parallel()

	id := "org_rearm_validate"
	minDays := constants.MinTrialRearmDays
	maxDays := constants.MaxTrialRearmDays

	cases := []struct {
		name    string
		id      *string
		days    *int
		wantErr bool
	}{
		{name: "at the minimum", id: &id, days: &minDays},
		{name: "at the maximum", id: &id, days: &maxDays},

		{name: "below the minimum", id: &id, days: new(minDays - 1), wantErr: true},
		{name: "above the maximum", id: &id, days: new(maxDays + 1), wantErr: true},
		{name: "negative", id: &id, days: new(-1), wantErr: true},

		{name: "empty id", id: new(""), days: &minDays, wantErr: true},
		{name: "missing id", id: nil, days: &minDays, wantErr: true},
		{name: "missing days", id: &id, days: nil, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := srv.ValidateRearmTrialRequestBody(&srv.RearmTrialRequestBody{ID: tc.id, Days: tc.days})
			if tc.wantErr {
				require.Error(t, err, "the request decoder must reject this before the handler runs")
				return
			}
			require.NoError(t, err)
		})
	}
}

// Below 500 on purpose: the admin app trusts a response body only below 500,
// and this one names the deployment setting the operator has to fix.
func TestRearmTrial_WithoutOpenRouterConfiguration(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminServiceWithOpenRouter(t, TrialKeysUnavailable{})
	seedDemotedTrial(t, ctx, conn, "org_rearm_unconfigured", "enterprise")

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: "org_rearm_unconfigured", Days: 14})
	requireOopsCode(t, err, oops.CodeInvalid)

	require.True(t, readTrial(t, ctx, conn, "org_rearm_unconfigured").DemotedAt.Valid,
		"a re-arm that could not revive the keys must leave the trial demoted")
	require.Equal(t, "free", readOrgState(t, ctx, conn, "org_rearm_unconfigured").GramAccountType)
}

func TestRearmTrial_TouchesOnlyTheTargetRow(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, provisioner := newRearmService(t)

	seedDemotedTrial(t, ctx, conn, "org_rearm_target", "enterprise")
	seedDemotedTrial(t, ctx, conn, "org_rearm_neighbour", "enterprise")
	neighbourBefore := readTrial(t, ctx, conn, "org_rearm_neighbour")

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: "org_rearm_target", Days: 14})
	require.NoError(t, err)

	neighbourAfter := readTrial(t, ctx, conn, "org_rearm_neighbour")
	require.True(t, neighbourAfter.DemotedAt.Valid, "re-arming must not spill onto other trials")
	require.Equal(t, neighbourBefore.EndsAt.Time, neighbourAfter.EndsAt.Time)
	require.Equal(t, neighbourBefore.UpdatedAt.Time, neighbourAfter.UpdatedAt.Time)

	neighbourState := readOrgState(t, ctx, conn, "org_rearm_neighbour")
	require.Equal(t, "free", neighbourState.GramAccountType)
	require.False(t, neighbourState.Whitelisted)

	for _, revival := range provisioner.revivals {
		require.Equal(t, "org_rearm_target", revival.orgID, "only the target organization's keys may be revived")
	}
	require.True(t, readOpenRouterKey(t, ctx, conn, "org_rearm_neighbour", openrouter.KeyTypeChat).Disabled)
}

// A half-failed re-arm ends with the operator pressing the button again. The
// second call must be a conflict, or re-arm becomes an unbounded extend.
func TestRearmTrial_RestoresADisabledOrganizationsTrialWithoutEnablingIt(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)

	// Disabled and trial state are independent axes, as they are for extend.
	orgID := "org_rearm_disabled"
	disabledAt := time.Now().UTC().Add(-time.Hour)
	endsAt := time.Now().UTC().Add(-10 * 24 * time.Hour)
	demotedAt := time.Now().UTC().Add(-9 * 24 * time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "free", disabledAt: &disabledAt})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, endsAt: endsAt, demotedAt: &demotedAt})
	seedArmAudit(t, ctx, conn, orgID)

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	require.NoError(t, err)

	state := readOrgState(t, ctx, conn, orgID)
	require.Equal(t, "enterprise", state.GramAccountType)
	require.True(t, state.Whitelisted)
	require.True(t, state.DisabledAt.Valid, "re-arming a trial must not enable a disabled organization")
}

// A sequence, not concurrency: neither order may leave the organization half
// restored.
func TestRearmTrial_SurvivesTheSweeperReachingTheOrganizationFirst(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	seedDemotedTrial(t, ctx, conn, "org_rearm_sweep", "enterprise")

	_, err := svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: "org_rearm_sweep", Days: 14})
	require.NoError(t, err)

	// The sweeper's own selection query, run after the re-arm committed.
	expired, err := trialsRepo.New(conn).ListExpiredTrials(ctx)
	require.NoError(t, err)
	require.NotContains(t, expired, "org_rearm_sweep")

	// And its write, which must find nothing to do.
	_, err = trialsRepo.New(conn).MarkTrialDemoted(ctx, "org_rearm_sweep")
	require.Error(t, err, "a sweep arriving after a re-arm must demote nothing")

	require.True(t, readOrgState(t, ctx, conn, "org_rearm_sweep").Whitelisted)
}
