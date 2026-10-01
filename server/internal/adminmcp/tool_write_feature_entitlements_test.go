package adminmcp

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
)

func TestFeatureWriteEntitlementsEnableDisableAndPreserveConnectionState(t *testing.T) {
	t.Parallel()
	for _, feature := range []productfeatures.Feature{productfeatures.FeatureSSO, productfeatures.FeatureSCIM} {
		t.Run(string(feature), func(t *testing.T) {
			t.Parallel()
			f := newProposalFixture(t, "admin_mcp_entitlement_"+string(feature))
			writer, tools, features := newFeatureWriterFixture(t, f)
			ctx := writeContext(t, f)
			orgs := orgrepo.New(f.db)
			workosID := conv.ToPGText("workos_placeholder")
			_, err := orgs.SetOrgWorkosID(ctx, orgrepo.SetOrgWorkosIDParams{OrganizationID: f.orgA, WorkosID: workosID})
			require.NoError(t, err)
			require.NoError(t, orgs.SetSSOEnabled(ctx, orgrepo.SetSSOEnabledParams{WorkosID: workosID, Enabled: conv.PtrToPGBool(new(true)), WorkosLastEventID: conv.ToPGText("event_placeholder")}))
			require.NoError(t, orgs.SetSCIMEnabled(ctx, orgrepo.SetSCIMEnabledParams{WorkosID: workosID, Enabled: conv.PtrToPGBool(new(true)), WorkosLastEventID: conv.ToPGText("event_placeholder")}))
			before, err := orgs.GetOrganizationMetadata(ctx, f.orgA)
			require.NoError(t, err)
			otherBefore, err := orgs.GetOrganizationMetadata(ctx, f.orgB)
			require.NoError(t, err)

			companion := productfeatures.FeatureSCIM
			if feature == productfeatures.FeatureSCIM {
				companion = productfeatures.FeatureSSO
			}
			q := featurerepo.New(f.db)
			_, err = q.EnableFeature(ctx, featurerepo.EnableFeatureParams{OrganizationID: f.orgA, FeatureName: string(companion)})
			require.NoError(t, err)
			cached, err := features.IsFeatureEnabled(ctx, f.orgA, feature)
			require.NoError(t, err)
			require.False(t, cached)

			for _, enabled := range []bool{true, false} {
				prepared, err := writer.prepare(ctx, PrepareFeatureInput{OrganizationID: f.orgA, Feature: string(feature), Enabled: enabled, RetryKey: string(feature) + "-" + enabledLabel(enabled)})
				require.NoError(t, err)
				var preview featurePreview
				require.NoError(t, json.Unmarshal(prepared.Preview, &preview))
				require.Equal(t, !enabled, preview.Before)
				require.Equal(t, enabled, preview.After)
				require.Contains(t, preview.SideEffects, "setup portal links")
				require.Contains(t, preview.SideEffects, "Does not create or remove")
				require.Contains(t, preview.SideEffects, "WorkOS-managed")
				require.Contains(t, preview.SideEffects, "Does not return portal links or change account type, billing or trials")
				id := uuid.MustParse(prepared.ProposalID)
				proposal, err := f.store.GetForOwner(ctx, id, f.owner)
				require.NoError(t, err)
				view, err := writer.view(proposal)
				require.NoError(t, err)
				require.Equal(t, preview.SideEffects, view.SideEffects)
				_, err = f.store.Approve(ctx, id, f.owner.SubjectURN, proposal.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
				require.NoError(t, err)
				result, err := tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
				require.NoError(t, err)
				require.JSONEq(t, `{"changed":true}`, string(result.Result))
				actual, err := q.IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{OrganizationID: f.orgA, FeatureName: string(feature)})
				require.NoError(t, err)
				require.Equal(t, enabled, actual)
				cached, err := features.IsFeatureEnabled(ctx, f.orgA, feature)
				require.NoError(t, err)
				require.Equal(t, enabled, cached)
				untouched, err := q.IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{OrganizationID: f.orgA, FeatureName: string(companion)})
				require.NoError(t, err)
				require.True(t, untouched)
				other, err := q.IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{OrganizationID: f.orgB, FeatureName: string(feature)})
				require.NoError(t, err)
				require.False(t, other)
				after, err := orgs.GetOrganizationMetadata(ctx, f.orgA)
				require.NoError(t, err)
				require.Equal(t, before, after, "entitlements must not change WorkOS connection status or other organisation metadata")
				otherAfter, err := orgs.GetOrganizationMetadata(ctx, f.orgB)
				require.NoError(t, err)
				require.Equal(t, otherBefore, otherAfter)
				action := audit.ActionOrganizationProductFeatureDisabled
				if enabled {
					action = audit.ActionOrganizationProductFeatureEnabled
				}
				entry, err := audittest.LatestAuditLogByAction(ctx, f.db, action)
				require.NoError(t, err)
				require.Equal(t, f.orgA, entry.OrganizationID)
				require.Equal(t, "staff-subject", entry.ActorID)
				require.Equal(t, string(audit.SurfaceAdminMCP), *entry.ActingSurface)
				require.Equal(t, "test-client", *entry.ActingClientID)
				result, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
				require.NoError(t, err)
				require.True(t, result.Replay)
				require.Equal(t, 1, countWriteEvents(t, f.db, id, "executed"))
				require.EqualValues(t, 1, auditCount(t, f, action))
			}
		})
	}
}

func TestFeatureWriteEntitlementsRejectDriftAtApproval(t *testing.T) {
	t.Parallel()
	for _, feature := range []productfeatures.Feature{productfeatures.FeatureSSO, productfeatures.FeatureSCIM} {
		t.Run(string(feature), func(t *testing.T) {
			t.Parallel()
			f := newProposalFixture(t, "admin_mcp_entitlement_approval_"+string(feature))
			writer := &featureWriter{store: f.store, writes: WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationSetOrganizationFeature: true}}} //nolint:exhaustive // Only the tested feature operation is enabled.
			prepared, err := writer.prepare(writeContext(t, f), PrepareFeatureInput{OrganizationID: f.orgA, Feature: string(feature), Enabled: true, RetryKey: "drift"})
			require.NoError(t, err)
			_, err = featurerepo.New(f.db).EnableFeature(t.Context(), featurerepo.EnableFeatureParams{OrganizationID: f.orgA, FeatureName: string(feature)})
			require.NoError(t, err)
			proposal, err := f.store.GetForOwner(t.Context(), uuid.MustParse(prepared.ProposalID), f.owner)
			require.NoError(t, err)
			_, err = f.store.Approve(t.Context(), proposal.ID, f.owner.SubjectURN, proposal.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
			require.ErrorIs(t, err, ErrStaleState)
			require.Zero(t, auditCount(t, f, audit.ActionOrganizationProductFeatureEnabled))
		})
	}
}

func TestFeatureWriteEntitlementsRejectDriftAtExecution(t *testing.T) {
	t.Parallel()
	for _, feature := range []productfeatures.Feature{productfeatures.FeatureSSO, productfeatures.FeatureSCIM} {
		t.Run(string(feature), func(t *testing.T) {
			t.Parallel()
			f := newProposalFixture(t, "admin_mcp_entitlement_execution_"+string(feature))
			writer, tools, _ := newFeatureWriterFixture(t, f)
			ctx := writeContext(t, f)
			prepared, err := writer.prepare(ctx, PrepareFeatureInput{OrganizationID: f.orgA, Feature: string(feature), Enabled: true, RetryKey: "drift"})
			require.NoError(t, err)
			proposal, err := f.store.GetForOwner(ctx, uuid.MustParse(prepared.ProposalID), f.owner)
			require.NoError(t, err)
			_, err = f.store.Approve(ctx, proposal.ID, f.owner.SubjectURN, proposal.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
			require.NoError(t, err)
			_, err = featurerepo.New(f.db).EnableFeature(ctx, featurerepo.EnableFeatureParams{OrganizationID: f.orgA, FeatureName: string(feature)})
			require.NoError(t, err)
			_, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
			require.ErrorIs(t, err, ErrStaleState)
			require.Zero(t, auditCount(t, f, audit.ActionOrganizationProductFeatureEnabled))
			require.Zero(t, countWriteEvents(t, f.db, proposal.ID, "executed"))
		})
	}
}

func TestFeatureWriteEntitlementsAuditFailureRollsBack(t *testing.T) {
	t.Parallel()
	for _, feature := range []productfeatures.Feature{productfeatures.FeatureSSO, productfeatures.FeatureSCIM} {
		t.Run(string(feature), func(t *testing.T) {
			t.Parallel()
			f := newProposalFixture(t, "admin_mcp_entitlement_rollback_"+string(feature))
			writer, tools, _ := newFeatureWriterFixture(t, f)
			ctx := writeContext(t, f)
			prepared, err := writer.prepare(ctx, PrepareFeatureInput{OrganizationID: f.orgA, Feature: string(feature), Enabled: true, RetryKey: "rollback"})
			require.NoError(t, err)
			proposal, err := f.store.GetForOwner(ctx, uuid.MustParse(prepared.ProposalID), f.owner)
			require.NoError(t, err)
			_, err = f.store.Approve(ctx, proposal.ID, f.owner.SubjectURN, proposal.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
			require.NoError(t, err)
			require.NoError(t, audittest.RejectAction(ctx, f.db, audit.ActionOrganizationProductFeatureEnabled))
			_, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
			require.Error(t, err)
			enabled, err := featurerepo.New(f.db).IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{OrganizationID: f.orgA, FeatureName: string(feature)})
			require.NoError(t, err)
			require.False(t, enabled)
			require.Zero(t, auditCount(t, f, audit.ActionOrganizationProductFeatureEnabled))
			require.Zero(t, countWriteEvents(t, f.db, proposal.ID, "executed"))
		})
	}
}
