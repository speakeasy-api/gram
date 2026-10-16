package adminmcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newFeatureWriterFixture(t *testing.T, f proposalFixture) (*featureWriter, *writeTools, *productfeatures.Client) {
	t.Helper()
	redisContainer, newRedisClient, err := testenv.NewTestRedis(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, redisContainer.Terminate(context.WithoutCancel(t.Context()))) })
	redisClient, err := newRedisClient(t, 0)
	require.NoError(t, err)
	features := productfeatures.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), f.db, redisClient)
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationSetOrganizationFeature: true}} //nolint:exhaustive // Only the tested feature operation is enabled.
	writer := &featureWriter{store: f.store, mutator: productfeatures.NewMutator(features, audit.NewLogger()), writes: writes, baseURL: "https://staff.example.test" + Path}
	tools := newWriteTools(f.store, writes, writer.baseURL, map[WriteOperation]operationWriter{OperationSetOrganizationFeature: writer}) //nolint:exhaustive // Only the tested feature operation is dispatched.
	return writer, tools, features
}

func TestFeatureWriteRefreshEnablesDisablesAndReplays(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_refresh_feature")
	writer, tools, features := newFeatureWriterFixture(t, f)
	ctx := writeContext(t, f)
	q := featurerepo.New(f.db)
	_, err := q.EnableFeature(t.Context(), featurerepo.EnableFeatureParams{OrganizationID: f.orgA, FeatureName: string(productfeatures.FeatureConsentToolFiltering)})
	require.NoError(t, err)
	for _, feature := range []productfeatures.Feature{productfeatures.FeatureRemoteSessionAutoRefresh, productfeatures.FeatureRemoteSessionAutoRefreshEnforced} {
		cached, err := features.IsFeatureEnabled(ctx, f.orgA, feature)
		require.NoError(t, err)
		require.False(t, cached)
	}

	for _, enabled := range []bool{true, false} {
		prepared, err := writer.prepare(ctx, PrepareFeatureInput{OrganizationID: f.orgA, Feature: string(productfeatures.FeatureRemoteSessionAutoRefresh), Enabled: enabled, RetryKey: "refresh-" + enabledLabel(enabled)})
		require.NoError(t, err)
		var preview featurePreview
		require.NoError(t, json.Unmarshal(prepared.Preview, &preview))
		require.Equal(t, !enabled, preview.Before)
		require.Equal(t, enabled, preview.After)
		require.Contains(t, preview.SideEffects, "Stored per-session choices are unchanged")
		require.Contains(t, preview.SideEffects, "enforcement is not changed")
		id := uuid.MustParse(prepared.ProposalID)
		proposal, err := f.store.GetForOwner(t.Context(), id, f.owner)
		require.NoError(t, err)
		view, err := writer.view(proposal)
		require.NoError(t, err)
		require.Equal(t, preview.SideEffects, view.SideEffects)
		_, err = f.store.Approve(t.Context(), id, f.owner.SubjectURN, proposal.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
		require.NoError(t, err)
		result, err := tools.execute(ctx, ProposalIDInput{ProposalID: id.String()})
		require.NoError(t, err)
		require.JSONEq(t, `{"changed":true}`, string(result.Result))
		cached, err := features.IsFeatureEnabled(ctx, f.orgA, productfeatures.FeatureRemoteSessionAutoRefresh)
		require.NoError(t, err)
		require.Equal(t, enabled, cached)
		for _, feature := range []productfeatures.Feature{productfeatures.FeatureRemoteSessionAutoRefresh, productfeatures.FeatureRemoteSessionAutoRefreshEnforced} {
			actual, err := q.IsFeatureEnabled(t.Context(), featurerepo.IsFeatureEnabledParams{OrganizationID: f.orgA, FeatureName: string(feature)})
			require.NoError(t, err)
			require.Equal(t, enabled && feature == productfeatures.FeatureRemoteSessionAutoRefresh, actual)
			other, err := q.IsFeatureEnabled(t.Context(), featurerepo.IsFeatureEnabledParams{OrganizationID: f.orgB, FeatureName: string(feature)})
			require.NoError(t, err)
			require.False(t, other)
		}
		enforcedCache, err := features.IsFeatureEnabled(ctx, f.orgA, productfeatures.FeatureRemoteSessionAutoRefreshEnforced)
		require.NoError(t, err)
		require.False(t, enforcedCache)
		untouched, err := q.IsFeatureEnabled(t.Context(), featurerepo.IsFeatureEnabledParams{OrganizationID: f.orgA, FeatureName: string(productfeatures.FeatureConsentToolFiltering)})
		require.NoError(t, err)
		require.True(t, untouched)
		action := audit.ActionOrganizationProductFeatureDisabled
		if enabled {
			action = audit.ActionOrganizationProductFeatureEnabled
		}
		entry, err := audittest.LatestAuditLogByAction(t.Context(), f.db, action)
		require.NoError(t, err)
		require.Equal(t, f.orgA, entry.OrganizationID)
		require.Equal(t, "staff-subject", entry.ActorID)
		require.Equal(t, string(audit.SurfaceAdminMCP), *entry.ActingSurface)
		require.Equal(t, "test-client", *entry.ActingClientID)
		result, err = tools.execute(ctx, ProposalIDInput{ProposalID: id.String()})
		require.NoError(t, err)
		require.True(t, result.Replay)
		require.Equal(t, 1, countWriteEvents(t, f.db, id, "executed"))
		require.EqualValues(t, 1, auditCount(t, f, action))
	}
}

func TestFeatureWriteRefreshRefusesEnforcedOrganizations(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_refresh_enforced")
	writer := &featureWriter{store: f.store, writes: WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationSetOrganizationFeature: true}}} //nolint:exhaustive // Only the tested feature operation is enabled.
	_, err := featurerepo.New(f.db).EnableFeature(t.Context(), featurerepo.EnableFeatureParams{OrganizationID: f.orgA, FeatureName: string(productfeatures.FeatureRemoteSessionAutoRefreshEnforced)})
	require.NoError(t, err)
	for _, enabled := range []bool{true, false} {
		_, err := writer.prepare(writeContext(t, f), PrepareFeatureInput{OrganizationID: f.orgA, Feature: string(productfeatures.FeatureRemoteSessionAutoRefresh), Enabled: enabled, RetryKey: "enforced-" + enabledLabel(enabled)})
		require.ErrorContains(t, err, "cannot change enforcement")
	}
	enforced, err := featurerepo.New(f.db).IsFeatureEnabled(t.Context(), featurerepo.IsFeatureEnabledParams{OrganizationID: f.orgA, FeatureName: string(productfeatures.FeatureRemoteSessionAutoRefreshEnforced)})
	require.NoError(t, err)
	require.True(t, enforced)
}

func TestFeatureWriteRefreshRejectsEnforcementDriftAtApproval(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_refresh_stale_approval")
	writer := &featureWriter{store: f.store, writes: WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationSetOrganizationFeature: true}}} //nolint:exhaustive // Only the tested feature operation is enabled.
	prepared, err := writer.prepare(writeContext(t, f), PrepareFeatureInput{OrganizationID: f.orgA, Feature: string(productfeatures.FeatureRemoteSessionAutoRefresh), Enabled: true, RetryKey: "stale-enforcement"})
	require.NoError(t, err)
	id := uuid.MustParse(prepared.ProposalID)
	proposal, err := f.store.GetForOwner(t.Context(), id, f.owner)
	require.NoError(t, err)
	_, err = featurerepo.New(f.db).EnableFeature(t.Context(), featurerepo.EnableFeatureParams{OrganizationID: f.orgA, FeatureName: string(productfeatures.FeatureRemoteSessionAutoRefreshEnforced)})
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), id, f.owner.SubjectURN, proposal.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.ErrorIs(t, err, ErrStaleState)
	proposal, err = f.store.GetForOwner(t.Context(), id, f.owner)
	require.NoError(t, err)
	require.Equal(t, ProposalInvalidated, proposal.Status)
	require.Equal(t, reasonStaleState, proposal.InvalidationReason)
}

func TestFeatureWriteRefreshRejectsEnforcementDriftAtExecution(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_refresh_stale_execution")
	writer, tools, _ := newFeatureWriterFixture(t, f)
	ctx := writeContext(t, f)
	prepared, err := writer.prepare(ctx, PrepareFeatureInput{OrganizationID: f.orgA, Feature: string(productfeatures.FeatureRemoteSessionAutoRefresh), Enabled: true, RetryKey: "stale-enforcement"})
	require.NoError(t, err)
	id := uuid.MustParse(prepared.ProposalID)
	proposal, err := f.store.GetForOwner(t.Context(), id, f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), id, f.owner.SubjectURN, proposal.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.NoError(t, err)
	_, err = featurerepo.New(f.db).EnableFeature(t.Context(), featurerepo.EnableFeatureParams{OrganizationID: f.orgA, FeatureName: string(productfeatures.FeatureRemoteSessionAutoRefreshEnforced)})
	require.NoError(t, err)
	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: id.String()})
	require.ErrorIs(t, err, ErrStaleState)
	proposal, err = f.store.GetForOwner(t.Context(), id, f.owner)
	require.NoError(t, err)
	require.Equal(t, ProposalInvalidated, proposal.Status)
	require.Equal(t, 1, countWriteEvents(t, f.db, id, string(ProposalInvalidated)))
	require.Zero(t, countWriteEvents(t, f.db, id, "executed"))
	enforced, err := featurerepo.New(f.db).IsFeatureEnabled(t.Context(), featurerepo.IsFeatureEnabledParams{OrganizationID: f.orgA, FeatureName: string(productfeatures.FeatureRemoteSessionAutoRefreshEnforced)})
	require.NoError(t, err)
	require.True(t, enforced, "stale execution must not clear enforcement")
	require.Zero(t, auditCount(t, f, audit.ActionOrganizationProductFeatureEnabled))
}

func TestFeatureStateRetainsExistingFeatureEncoding(t *testing.T) {
	t.Parallel()
	state := featureState{OrganizationID: "synthetic-org", Name: "Synthetic", Slug: "synthetic", Enabled: true}
	encoded, err := json.Marshal(state)
	require.NoError(t, err)
	legacy := []byte(`{"organization_id":"synthetic-org","name":"Synthetic","slug":"synthetic","enabled":true}`)
	before, err := stateDigest(legacy)
	require.NoError(t, err)
	after, err := stateDigest(encoded)
	require.NoError(t, err)
	require.Equal(t, before, after, "existing logs and consent proposals retain their expected-state digest")
	state.AutoRefreshEnforced = new(false)
	encoded, err = json.Marshal(state)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"auto_refresh_enforced":false`)
}
