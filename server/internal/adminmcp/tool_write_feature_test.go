package adminmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestFeatureWriteApprovalAndExecution(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_feature_write")
	redisContainer, newRedisClient, err := testenv.NewTestRedis(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, redisContainer.Terminate(context.Background())) })
	redisClient, err := newRedisClient(t, 0)
	require.NoError(t, err)
	features := productfeatures.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), f.db, redisClient)
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationSetOrganizationFeature: true}} //nolint:exhaustive // Only selected write operations are enabled by this test.
	writer := &featureWriter{store: f.store, mutator: productfeatures.NewMutator(features, audit.NewLogger()), writes: writes, baseURL: "https://staff.example.test" + Path}
	tools := newWriteTools(f.store, writes, writer.baseURL, map[WriteOperation]operationWriter{OperationSetOrganizationFeature: writer}) //nolint:exhaustive // Only the implemented operation is dispatched.
	staff := &contextvalues.AdminAuthContext{SessionID: "browser-session", OIDCSubject: "staff-subject", Email: "staff@example.test"}
	principal := Principal{Subject: f.owner.SubjectURN, Email: staff.Email, ClientID: "test-client", ClientRowID: f.owner.ClientRowID.String(), ConnectionID: f.owner.ConnectionID.String(), Generation: f.owner.Generation.String(), Scopes: []string{ScopeRead, ScopeWrite}, staff: staff}
	ctx := contextvalues.SetAdminAuthContext(context.WithValue(t.Context(), principalKey{}, principal), staff)
	state := func(org string, feature productfeatures.Feature) bool {
		t.Helper()
		enabled, err := featurerepo.New(f.db).IsFeatureEnabled(t.Context(), featurerepo.IsFeatureEnabledParams{OrganizationID: org, FeatureName: string(feature)})
		require.NoError(t, err)
		return enabled
	}

	_, err = writer.prepare(ctx, PrepareFeatureInput{OrganizationID: f.orgA, Feature: "logs", Enabled: true})
	require.Error(t, err, "retry keys are mandatory")
	for _, unreviewed := range []string{"sso", "remote_session_auto_refresh_enforced", "session_portability", "platform_mcp", "skills", ""} {
		_, err = writer.prepare(ctx, PrepareFeatureInput{OrganizationID: f.orgA, Feature: unreviewed, Enabled: true, RetryKey: "not-supported-" + unreviewed})
		require.Error(t, err, "unreviewed feature %q stays disabled", unreviewed)
	}
	_, err = writer.prepare(ctx, PrepareFeatureInput{OrganizationID: "synthetic-a", Feature: "logs", Enabled: true, RetryKey: "slug"})
	require.Error(t, err, "a slug is not a canonical target")

	verifier := &fakeAdminVerifier{result: staff}
	memory := testenv.NewMemoryCache()
	approval := newStaffProposalApproval(f.store, NewStaffOAuthAuthorization(nil, nil, memory, verifier, f.cipher, staffAudience), memory, writes)
	approval.operations = map[WriteOperation]approvableOperation{OperationSetOrganizationFeature: writer} //nolint:exhaustive // Only selected write operations are enabled by this test.
	handler := middleware.AdminOriginCheck(nil)(approval.Handler())

	// Sequential, not subtests: the features share one organisation so each
	// run can check that the other features are left alone.
	writable := []productfeatures.Feature{productfeatures.FeatureLogs, productfeatures.FeatureConsentToolFiltering, productfeatures.FeatureRemoteSessionAutoRefresh}
	for _, feature := range writable {
		func() {
			t.Logf("feature %s", feature)

			input := PrepareFeatureInput{OrganizationID: f.orgA, Feature: string(feature), Enabled: true, RetryKey: "feature-" + string(feature)}
			prepared, err := writer.prepare(ctx, input)
			require.NoError(t, err)
			require.Equal(t, "https://staff.example.test"+Path+"/proposals/"+prepared.ProposalID, prepared.ApprovalURL)
			var preview featurePreview
			require.NoError(t, json.Unmarshal(prepared.Preview, &preview))
			require.Equal(t, string(feature), preview.Feature)
			require.False(t, preview.Before)
			require.True(t, preview.After)
			require.Equal(t, writableFeatures[feature], preview.SideEffects)
			require.False(t, state(f.orgA, feature), "prepare does not mutate the entitlement")
			require.False(t, state(f.orgB, feature))

			replay, err := writer.prepare(ctx, input)
			require.NoError(t, err)
			require.True(t, replay.Replay)
			require.Equal(t, prepared.ProposalID, replay.ProposalID)
			input.Enabled = false
			_, err = writer.prepare(ctx, input)
			require.ErrorIs(t, err, ErrProposalConflict)
			id, err := uuid.Parse(prepared.ProposalID)
			require.NoError(t, err)
			_, err = tools.execute(ctx, ProposalIDInput{ProposalID: id.String()})
			require.ErrorIs(t, err, ErrProposalNotApproved)

			page := httptest.NewRecorder()
			handler.ServeHTTP(page, approvalRequest(http.MethodGet, id, nil, "browser-session"))
			require.Equal(t, http.StatusOK, page.Code)
			require.Contains(t, page.Body.String(), "Synthetic A")
			require.Contains(t, page.Body.String(), "<td>"+string(feature)+" feature</td><td>Off</td><td>On</td>")
			form := approvalForm(t, page.Body.String())
			accepted := httptest.NewRecorder()
			handler.ServeHTTP(accepted, approvalRequest(http.MethodPost, id, form, "browser-session"))
			require.Equal(t, http.StatusOK, accepted.Code)

			result, err := tools.execute(ctx, ProposalIDInput{ProposalID: id.String()})
			require.NoError(t, err)
			require.Equal(t, string(ProposalSucceeded), result.Status)
			require.JSONEq(t, `{"changed":true}`, string(result.Result))
			require.True(t, state(f.orgA, feature))
			require.False(t, state(f.orgB, feature), "execution cannot affect a second tenant")
			for _, untouched := range writable {
				if untouched != feature {
					require.False(t, state(f.orgA, untouched), "execution changes only the stored feature")
				}
			}
			cached, err := features.IsFeatureEnabled(ctx, f.orgA, feature)
			require.NoError(t, err)
			require.True(t, cached)
			require.Equal(t, 1, countWriteEvents(t, f.db, id, "executed"))

			result, err = tools.execute(ctx, ProposalIDInput{ProposalID: id.String()})
			require.NoError(t, err)
			require.True(t, result.Replay)
			require.Equal(t, 1, countWriteEvents(t, f.db, id, "executed"))
			status, err := tools.status(ctx, ProposalIDInput{ProposalID: id.String()})
			require.NoError(t, err)
			require.Equal(t, string(ProposalSucceeded), status.Status)
			require.JSONEq(t, `{"changed":true}`, string(status.Result))

			otherPrincipal := principal
			otherPrincipal.Subject = "user:other-staff"
			other := context.WithValue(ctx, principalKey{}, otherPrincipal)
			_, err = tools.execute(other, ProposalIDInput{ProposalID: id.String()})
			require.ErrorIs(t, err, ErrProposalNotFound)
		}()
		// Reset so the next feature starts from the same off state.
		_, err = featurerepo.New(f.db).DeleteFeature(t.Context(), featurerepo.DeleteFeatureParams{OrganizationID: f.orgA, FeatureName: string(feature)})
		require.NoError(t, err)
	}

	f.reauth()
	_, err = writer.prepare(ctx, PrepareFeatureInput{OrganizationID: f.orgB, Feature: "logs", Enabled: true, RetryKey: "old-connection"})
	require.ErrorIs(t, err, ErrConnectionChanged)
}

func TestFeatureWriteStaleStateAndDisabledSwitch(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_feature_stale")
	writer := &featureWriter{store: f.store, writes: WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationSetOrganizationFeature: true}}} //nolint:exhaustive // Only selected write operations are enabled by this test.
	tx, err := f.db.Begin(t.Context())                                                                                                                       //nolint:glint // notestingrawsql: Exercise the transaction-bound state reader with a synthetic database.
	require.NoError(t, err)
	state, err := writer.readState(t.Context(), tx, f.orgA, productfeatures.FeatureLogs)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	expected, err := json.Marshal(state)
	require.NoError(t, err)
	pInput := featureProposal(f.orgA, "stale-feature", true)
	pInput.ExpectedState = expected
	p, _, err := f.store.Create(t.Context(), f.owner, pInput, time.Now())
	require.NoError(t, err)
	_, err = featurerepo.New(f.db).EnableFeature(t.Context(), featurerepo.EnableFeatureParams{OrganizationID: f.orgA, FeatureName: "logs"})
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), p.ID, f.owner.SubjectURN, p.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.ErrorIs(t, err, ErrStaleState, "approval must reject a changed feature state")
	closed, err := f.store.GetForOwner(t.Context(), p.ID, f.owner)
	require.NoError(t, err)
	require.Equal(t, ProposalInvalidated, closed.Status)
	writer.writes = WriteConfig{}
	tools := newWriteTools(f.store, writer.writes, "", map[WriteOperation]operationWriter{OperationSetOrganizationFeature: writer}) //nolint:exhaustive // Only the implemented operation is dispatched.
	_, err = writer.prepare(t.Context(), PrepareFeatureInput{OrganizationID: f.orgA, Feature: "logs", Enabled: true, RetryKey: "disabled"})
	require.ErrorIs(t, err, ErrWriteDisabled)
	_, err = tools.execute(t.Context(), ProposalIDInput{ProposalID: p.ID.String()})
	require.ErrorIs(t, err, ErrWriteDisabled)
}

// A stored proposal naming a feature outside the allowlist can only come from
// direct database access. Approval refuses it, and execution refuses it even
// when approval was bypassed.
func TestFeatureWriteRejectsUnreviewedStoredFeature(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_feature_unreviewed")
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationSetOrganizationFeature: true}} //nolint:exhaustive // Only selected write operations are enabled by this test.
	writer := &featureWriter{store: f.store, writes: writes}
	tools := newWriteTools(f.store, writes, "", map[WriteOperation]operationWriter{OperationSetOrganizationFeature: writer}) //nolint:exhaustive // Only the implemented operation is dispatched.
	ctx := writeContext(t, f)
	unreviewed := func(key string) NewProposal {
		p := featureProposal(f.orgA, key, true)
		p.Arguments = json.RawMessage(`{"feature":"sso","enabled":true}`)
		return p
	}

	p, _, err := f.store.Create(t.Context(), f.owner, unreviewed("unreviewed-approve"), time.Now())
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), p.ID, f.owner.SubjectURN, p.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.ErrorIs(t, err, ErrProposalInvalidated)

	q, _, err := f.store.Create(t.Context(), f.owner, unreviewed("unreviewed-execute"), time.Now())
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), q.ID, f.owner.SubjectURN, q.ProposalDigest, time.Now(), allowProposalBrowser, allowProposalBrowser)
	require.NoError(t, err)
	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: q.ID.String()})
	require.ErrorIs(t, err, ErrProposalInvalidated)
	require.Equal(t, 0, countWriteEvents(t, f.db, q.ID, "executed"))
	enabled, err := featurerepo.New(f.db).IsFeatureEnabled(t.Context(), featurerepo.IsFeatureEnabledParams{OrganizationID: f.orgA, FeatureName: "sso"})
	require.NoError(t, err)
	require.False(t, enabled)
}
