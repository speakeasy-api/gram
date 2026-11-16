package adminmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
)

func seedAssistantAgentWithGeneratedName(t *testing.T, f writeFixture, organizationID string) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	project, assistant := seedLegacyAssistant(t, f, organizationID)
	identities := &assistantIdentityMigrationWriter{runner: f.runner, identities: assistantidentity.New("https://platform.example.invalid", audit.NewLogger()), writes: WriteConfig{Enabled: true}}
	_, err := identities.apply(writeContext(t, f), MigrateAssistantIdentitiesInput{OrganizationID: organizationID})
	require.NoError(t, err)
	var agent uuid.UUID
	err = f.db.QueryRow(t.Context(), `UPDATE agents AS g SET name = 'Assistant ' || $1::text FROM assistant_agent_bindings AS b WHERE b.assistant_id = $1::uuid AND b.agent_id = g.id RETURNING g.id`, assistant.String()).Scan(&agent) //nolint:glint // notestingrawsql: legacy-name fixture and exact post-mutation assertion.
	require.NoError(t, err)
	return project, assistant, agent
}

func TestAssistantAgentNameRepairApplies(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_assistant_names")
	project, assistant, agent := seedAssistantAgentWithGeneratedName(t, f, f.orgA)
	_, foreign, foreignAgent := seedAssistantAgentWithGeneratedName(t, f, f.orgB)
	writer := &assistantAgentNameRepairWriter{runner: f.runner, audit: audit.NewLogger(), writes: WriteConfig{Enabled: true}}
	ctx := writeContext(t, f)
	receipt, err := writer.apply(ctx, RepairAssistantAgentNamesInput{OrganizationID: f.orgA})
	require.NoError(t, err)
	require.Equal(t, f.orgA, receipt.OrganizationID)
	require.Equal(t, int64(1), receipt.Repaired)
	require.Zero(t, receipt.Conflicts)
	require.Zero(t, receipt.Failed)
	require.Equal(t, []assistantAgentNameRepairResult{{AssistantID: assistant.String(), ProjectID: project.String(), Name: "Legacy", Outcome: "repaired"}}, receipt.Results)
	var name string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT name FROM agents WHERE id = $1`, agent).Scan(&name)) //nolint:glint // notestingrawsql: legacy-name fixture and exact post-mutation assertion.
	require.Equal(t, "Legacy", name)
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT name FROM agents WHERE id = $1`, foreignAgent).Scan(&name)) //nolint:glint // notestingrawsql: legacy-name fixture and exact post-mutation assertion.
	require.Equal(t, "Assistant "+foreign.String(), name)
	entry, err := audittest.LatestAuditLogByActionAndSubject(t.Context(), f.db, audit.ActionAgentRename, agent.String())
	require.NoError(t, err)
	require.Equal(t, "staff-subject", entry.ActorID)
	require.Equal(t, new(string(audit.SurfaceAdminMCP)), entry.ActingSurface)
	require.Equal(t, new("test-client"), entry.ActingClientID)
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationRepairAssistantAgentNames, f.orgA, writeEventExecuted))
	again, err := writer.apply(ctx, RepairAssistantAgentNamesInput{OrganizationID: f.orgA})
	require.NoError(t, err)
	require.Zero(t, again.Repaired)
	require.Empty(t, again.Results)
	_, err = writer.apply(ctx, RepairAssistantAgentNamesInput{OrganizationID: "org_missing_" + uuid.NewString()})
	require.ErrorContains(t, err, "no organization with that ID")
}

func TestAssistantAgentNameRepairReportsConflict(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_assistant_name_conflict")
	_, assistant, agent := seedAssistantAgentWithGeneratedName(t, f, f.orgA)
	_, err := f.db.Exec(t.Context(), `INSERT INTO agents (organization_id, project_id, owner_user_id, name) SELECT organization_id, project_id, owner_user_id, 'Legacy' FROM agents WHERE id = $1`, agent) //nolint:glint // notestingrawsql: legacy-name fixture and exact post-mutation assertion.
	require.NoError(t, err)
	writer := &assistantAgentNameRepairWriter{runner: f.runner, audit: audit.NewLogger(), writes: WriteConfig{Enabled: true}}
	receipt, err := writer.apply(writeContext(t, f), RepairAssistantAgentNamesInput{OrganizationID: f.orgA})
	require.NoError(t, err)
	require.Zero(t, receipt.Repaired)
	require.Equal(t, int64(1), receipt.Conflicts)
	require.Zero(t, receipt.Failed)
	require.Len(t, receipt.Results, 1)
	require.Equal(t, assistant.String(), receipt.Results[0].AssistantID)
	var name string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT name FROM agents WHERE id = $1`, agent).Scan(&name)) //nolint:glint // notestingrawsql: legacy-name fixture and exact post-mutation assertion.
	require.Equal(t, "Assistant "+assistant.String(), name)
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationRepairAssistantAgentNames, f.orgA, writeEventExecuted))
}

func TestAssistantAgentNameRepairRequiresAuthorityAndExactTarget(t *testing.T) {
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
			writer := &assistantAgentNameRepairWriter{writes: tc.writes}
			_, err := writer.apply(tc.ctx, RepairAssistantAgentNamesInput{OrganizationID: tc.organizationID})
			require.ErrorIs(t, err, tc.want)
		})
	}
	writer := &assistantAgentNameRepairWriter{writes: WriteConfig{Enabled: true}}
	for _, id := range []string{"", " org_synthetic", "org_synthetic "} {
		_, err := writer.apply(writePrincipalContext(t, []string{ScopeRead, ScopeWrite}), RepairAssistantAgentNamesInput{OrganizationID: id})
		require.ErrorContains(t, err, "provide an exact organization ID")
	}
	require.Contains(t, adminServerInstructions, string(OperationRepairAssistantAgentNames))
	require.Contains(t, adminServerInstructions, "does not migrate identities or runtimes")
}

func TestAssistantAgentNameRepairReceiptBounded(t *testing.T) {
	t.Parallel()
	for _, count := range []int{0, 100, 101, 125} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			t.Parallel()
			run := &gen.AdminAssistantAgentNameRepairRun{OrganizationID: "org_synthetic", Repaired: 80, Conflicts: 30, Failed: 15, Results: make([]*gen.AdminAssistantAgentNameRepairResult, 0, count)}
			for i := range count {
				run.Results = append(run.Results, &gen.AdminAssistantAgentNameRepairResult{AssistantID: fmt.Sprint(i), ProjectID: "project-synthetic", Name: "Assistant name", Outcome: "repaired"})
			}
			receipt := assistantAgentNameRepairReceiptFromRun(run)
			require.Equal(t, run.OrganizationID, receipt.OrganizationID)
			require.Equal(t, run.Repaired, receipt.Repaired)
			require.Equal(t, run.Conflicts, receipt.Conflicts)
			require.Equal(t, run.Failed, receipt.Failed)
			require.Len(t, receipt.Results, min(count, 100))
			require.Equal(t, count > 100, receipt.PossiblyIncomplete)
			require.Len(t, run.Results, count, "receipt truncation must not alter the run")
			for i, result := range receipt.Results {
				require.Equal(t, fmt.Sprint(i), result.AssistantID)
				require.Equal(t, "project-synthetic", result.ProjectID)
				require.Equal(t, "Assistant name", result.Name)
				require.Equal(t, "repaired", result.Outcome)
			}
			encoded, err := json.Marshal(receipt)
			require.NoError(t, err)
			require.Contains(t, string(encoded), fmt.Sprintf(`"possibly_incomplete":%t`, count > 100))
			if count == 0 {
				require.Contains(t, string(encoded), `"results":[]`)
			}
		})
	}
}
