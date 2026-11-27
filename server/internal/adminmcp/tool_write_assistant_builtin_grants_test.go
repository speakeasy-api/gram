package adminmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
)

func seedAssistantMissingBuiltinGrant(t *testing.T, f writeFixture, organizationID string) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	project, assistant := seedLegacyAssistant(t, f, organizationID)
	identities := &assistantIdentityMigrationWriter{
		runner:     f.runner,
		identities: assistantidentity.New("https://platform.example.invalid", audit.NewLogger()),
		writes:     WriteConfig{Enabled: true},
	}
	_, err := identities.apply(writeContext(t, f), MigrateAssistantIdentitiesInput{OrganizationID: organizationID})
	require.NoError(t, err)
	states, err := assistantidentity.States(t.Context(), f.db, project, []uuid.UUID{assistant})
	require.NoError(t, err)
	agentID := *states[assistant].AgentID
	agents := agentrepo.New(f.db)
	rows, err := agents.ListAgentPolicyGrants(t.Context(), agentrepo.ListAgentPolicyGrantsParams{
		OrganizationID: organizationID,
		AgentID:        agentID,
	})
	require.NoError(t, err)
	want := authz.NewSelector(authz.ScopeMCPConnect, platformtools.PlatformToolsetID(platformtools.AssistantsPlatformToolsetSlug).String())
	want[authz.SelectorKeyProjectID] = project.String()
	for _, row := range rows {
		if row.Scope != string(authz.ScopeMCPConnect) {
			continue
		}
		got, err := authz.SelectorFromRow(row.Selectors)
		require.NoError(t, err)
		if maps.Equal(got, want) {
			_, err = agents.DeleteAgentPolicyGrant(t.Context(), agentrepo.DeleteAgentPolicyGrantParams{
				OrganizationID: organizationID,
				AgentID:        agentID,
				GrantID:        row.ID,
			})
			require.NoError(t, err)
		}
	}
	return project, assistant, agentID
}

func hasExplicitAssistantsGrant(t *testing.T, f writeFixture, organizationID string, project, agentID uuid.UUID) bool {
	t.Helper()
	rows, err := agentrepo.New(f.db).ListAgentPolicyGrants(t.Context(), agentrepo.ListAgentPolicyGrantsParams{
		OrganizationID: organizationID,
		AgentID:        agentID,
	})
	require.NoError(t, err)
	want := authz.NewSelector(authz.ScopeMCPConnect, platformtools.PlatformToolsetID(platformtools.AssistantsPlatformToolsetSlug).String())
	want[authz.SelectorKeyProjectID] = project.String()
	for _, row := range rows {
		if row.Scope != string(authz.ScopeMCPConnect) || row.ExpiresAt.Valid {
			continue
		}
		got, err := authz.SelectorFromRow(row.Selectors)
		require.NoError(t, err)
		if maps.Equal(got, want) {
			return true
		}
	}
	return false
}

func TestAssistantBuiltinGrantsBackfillApplies(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_assistant_builtin_grants")
	project, assistant, agentID := seedAssistantMissingBuiltinGrant(t, f, f.orgA)
	foreignProject, _, foreignAgent := seedAssistantMissingBuiltinGrant(t, f, f.orgB)

	writer := &assistantBuiltinGrantsBackfillWriter{runner: f.runner, audit: audit.NewLogger(), writes: WriteConfig{Enabled: true}}
	ctx := writeContext(t, f)
	receipt, err := writer.apply(ctx, BackfillAssistantBuiltinGrantsInput{OrganizationID: f.orgA})
	require.NoError(t, err)
	require.Equal(t, f.orgA, receipt.OrganizationID)
	require.Equal(t, int64(1), receipt.Granted)
	require.Zero(t, receipt.AlreadyGranted)
	require.Zero(t, receipt.Failed)
	require.Equal(t, []assistantBuiltinGrantBackfillResult{{
		AssistantID: assistant.String(),
		ProjectID:   project.String(),
		Name:        "Legacy",
		Outcome:     "granted",
		GrantsAdded: 1,
	}}, receipt.Results)

	require.True(t, hasExplicitAssistantsGrant(t, f, f.orgA, project, agentID))
	require.False(t, hasExplicitAssistantsGrant(t, f, f.orgB, foreignProject, foreignAgent))

	entry, err := audittest.LatestAuditLogByActionAndSubject(t.Context(), f.db, audit.ActionAgentPolicyGrantCreate, agentID.String())
	require.NoError(t, err)
	require.Equal(t, "staff-subject", entry.ActorID)
	require.Equal(t, new(string(audit.SurfaceAdminMCP)), entry.ActingSurface)
	require.Equal(t, new("test-client"), entry.ActingClientID)
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationBackfillAssistantBuiltinGrants, f.orgA, writeEventExecuted))

	again, err := writer.apply(ctx, BackfillAssistantBuiltinGrantsInput{OrganizationID: f.orgA})
	require.NoError(t, err)
	require.Zero(t, again.Granted)
	require.Equal(t, int64(1), again.AlreadyGranted)
	require.Equal(t, []assistantBuiltinGrantBackfillResult{{
		AssistantID: assistant.String(),
		ProjectID:   project.String(),
		Name:        "Legacy",
		Outcome:     "already_granted",
		GrantsAdded: 0,
	}}, again.Results)

	_, err = writer.apply(ctx, BackfillAssistantBuiltinGrantsInput{OrganizationID: "org_missing_" + uuid.NewString()})
	require.ErrorContains(t, err, "no organization with that ID")
}

func TestAssistantBuiltinGrantsBackfillRequiresAuthorityAndExactTarget(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name           string
		writes         WriteConfig
		ctx            context.Context //nolint:containedctx // Table-driven authority contexts are scoped to this test.
		organizationID string
		want           error
	}{
		{"disabled", WriteConfig{}, t.Context(), "org_synthetic", ErrWriteDisabled},
		{"unauthenticated", WriteConfig{Enabled: true}, t.Context(), "org_synthetic", ErrWriteIdentity},
		{"read only", WriteConfig{Enabled: true}, writePrincipalContext(t, []string{ScopeRead}), "org_synthetic", ErrWriteScope},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			writer := &assistantBuiltinGrantsBackfillWriter{writes: tc.writes}
			_, err := writer.apply(tc.ctx, BackfillAssistantBuiltinGrantsInput{OrganizationID: tc.organizationID})
			require.ErrorIs(t, err, tc.want)
		})
	}
	writer := &assistantBuiltinGrantsBackfillWriter{writes: WriteConfig{Enabled: true}}
	for _, id := range []string{"", " org_synthetic", "org_synthetic "} {
		_, err := writer.apply(writePrincipalContext(t, []string{ScopeRead, ScopeWrite}), BackfillAssistantBuiltinGrantsInput{OrganizationID: id})
		require.ErrorContains(t, err, "provide an exact organization ID")
	}
	require.Contains(t, adminServerInstructions, string(OperationBackfillAssistantBuiltinGrants))
}

func TestAssistantBuiltinGrantsBackfillReceiptBounded(t *testing.T) {
	t.Parallel()
	for _, count := range []int{0, 100, 101, 125} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			t.Parallel()
			run := &gen.AdminAssistantBuiltinGrantBackfillRun{
				OrganizationID: "org_synthetic",
				Granted:        80,
				AlreadyGranted: 30,
				Failed:         15,
				Results:        make([]*gen.AdminAssistantBuiltinGrantBackfillResult, 0, count),
			}
			for i := range count {
				run.Results = append(run.Results, &gen.AdminAssistantBuiltinGrantBackfillResult{
					AssistantID: fmt.Sprint(i),
					ProjectID:   "project-synthetic",
					Name:        "Assistant name",
					Outcome:     "granted",
					GrantsAdded: 1,
				})
			}
			receipt := assistantBuiltinGrantBackfillReceiptFromRun(run)
			require.Equal(t, run.OrganizationID, receipt.OrganizationID)
			require.Equal(t, run.Granted, receipt.Granted)
			require.Equal(t, run.AlreadyGranted, receipt.AlreadyGranted)
			require.Equal(t, run.Failed, receipt.Failed)
			require.Len(t, receipt.Results, min(count, 100))
			require.Equal(t, count > 100, receipt.PossiblyIncomplete)
			require.Len(t, run.Results, count, "receipt truncation must not alter the run")
			encoded, err := json.Marshal(receipt)
			require.NoError(t, err)
			require.Contains(t, string(encoded), fmt.Sprintf(`"possibly_incomplete":%t`, count > 100))
			if count == 0 {
				require.Contains(t, string(encoded), `"results":[]`)
			}
		})
	}
}
