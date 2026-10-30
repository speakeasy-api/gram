package demoseed

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/associationbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/executionprofiles"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/selectionbackfill"
)

// migrateUpstreamAuth brings the tenant's freshly seeded upstream auth
// configuration to the state the staff upstream OAuth migration leaves it in,
// then checks that nothing is left for that migration to do.
//
// The seed writes the legacy configuration natively: upstream OAuth client
// associations and each sign-in's client choice. The typed auth clients, their
// server links and header copies, the servers marked as requiring upstream
// auth, the mapped credential choices, the default mcp:auth grants and the
// assistant execution profiles are recorded by the same backfills the admin
// migration runs, in the same order, so the seeded organization is exactly a
// migrated organization and cannot drift from what the resolver and the
// readiness checks expect. The project delete at the start of every run
// cascades all of these rows away, so every run records them again. Agent
// attachment grants name upstream_client selectors, whose writes the seed
// treats as rolled out for the seeded organization.
//
// It runs after the Postgres transaction commits, because the backfills take
// their own organization lock and transactions. Both backfills are scoped to
// the tenant's organization and only add or retire rows inside it. A failure
// fails the seed run: the data stays in place, and the next run, or a manual
// migration run, completes it.
func migrateUpstreamAuth(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, organizationID string) error {
	ctx = authz.WithUpstreamSelectorWriteGate(ctx, seededSelectorWrites(organizationID))
	if err := checkLegacyUpstreamAuth(ctx, db, organizationID); err != nil {
		return err
	}

	clients, err := authclientbackfill.Run(ctx, db, authclientbackfill.Options{OrganizationID: organizationID, Apply: true, PageSize: 0})
	if err != nil {
		return fmt.Errorf("record typed auth clients: %w", err)
	}
	if len(clients.PhaseErrors) > 0 {
		return fmt.Errorf("record typed auth clients: failed phases %s", phaseErrors(clients.PhaseErrors))
	}
	// Profiles decide from the typed auth clients, so they come after a run
	// that recorded every auth client phase, as in the admin migration.
	profiles, err := executionprofiles.Backfill(ctx, db, organizationID, true)
	if err != nil {
		return fmt.Errorf("record execution selection profiles: %w", err)
	}
	logger.InfoContext(ctx, fmt.Sprintf(
		"demo seed recorded typed upstream auth: %d auth clients, %d links, %d header copies, %d servers marked, %d choices mapped, %d grants, %d profile versions",
		clients.ClientsWritten, clients.LinksWritten, clients.HeadersWritten, clients.ServersMarked, clients.SelectionsMapped, clients.GrantsWritten, profiles.VersionsWritten,
	), attr.SlogOrganizationID(organizationID))

	return upstreamAuthReadiness(ctx, logger, db, organizationID)
}

// checkLegacyUpstreamAuth fails when the seed left an association or a
// credential choice for the legacy backfills to decide: the seed writes those
// itself, exactly as issuance and the server editor would have.
func checkLegacyUpstreamAuth(ctx context.Context, db *pgxpool.Pool, organizationID string) error {
	var problems []string

	associations, err := associationbackfill.Run(ctx, db, associationbackfill.Options{OrganizationID: organizationID, Apply: false, PageSize: 0})
	if err != nil {
		return fmt.Errorf("check upstream oauth client associations: %w", err)
	}
	if associations.Decided > 0 {
		problems = append(problems, fmt.Sprintf("%d server client associations left to record", associations.Decided))
	}
	for _, result := range associations.Results {
		if !result.Outcome.Covered() {
			problems = append(problems, fmt.Sprintf("server %s association is %s", result.MCPServerID, result.Outcome))
		}
	}
	for _, result := range associations.GatewayResults {
		if !result.Outcome.Covered() {
			problems = append(problems, fmt.Sprintf("gateway server %s association is %s", result.MCPServerID, result.Outcome))
		}
	}

	selections, err := selectionbackfill.Run(ctx, db, selectionbackfill.Options{OrganizationID: organizationID, Apply: false, PageSize: 0})
	if err != nil {
		return fmt.Errorf("check upstream oauth client selections: %w", err)
	}
	if pending := selections.Pairs[selectionbackfill.OutcomeClient] + selections.Pairs[selectionbackfill.OutcomeNoAuth]; pending > 0 {
		problems = append(problems, fmt.Sprintf("%d credential client choices left to record", pending))
	}
	for _, gap := range selections.Gaps {
		problems = append(problems, fmt.Sprintf("credential %s choice for server %s is %s", gap.CredentialID, gap.MCPServerID, gap.Outcome))
	}

	if len(problems) > 0 {
		return fmt.Errorf("demo seed left legacy upstream auth incomplete: %s", strings.Join(problems, "; "))
	}
	return nil
}

// upstreamAuthReadiness fails unless every check the admin upstream OAuth
// migration status makes reads complete: the legacy migration, typed auth
// clients, credential choice mappings, default auth permissions, agent
// attachments and assistant execution profiles. Each check is a dry run of
// the backfill the status itself dry-runs, so a seeded organization that
// passes reads as migrated and ready for enforcement there.
func upstreamAuthReadiness(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, organizationID string) error {
	ctx = authz.WithUpstreamSelectorWriteGate(ctx, seededSelectorWrites(organizationID))
	if err := checkLegacyUpstreamAuth(ctx, db, organizationID); err != nil {
		return err
	}

	var problems []string
	pending := func(what string, n int) {
		if n != 0 {
			problems = append(problems, fmt.Sprintf("%d %s", n, what))
		}
	}

	clients, err := authclientbackfill.Run(ctx, db, authclientbackfill.Options{OrganizationID: organizationID, Apply: false, PageSize: 0})
	if err != nil {
		return fmt.Errorf("check typed auth clients: %w", err)
	}
	if len(clients.PhaseErrors) > 0 {
		problems = append(problems, "failed phases "+phaseErrors(clients.PhaseErrors))
	}
	pending("typed auth clients to record", clients.PendingClients)
	pending("typed auth client server links to record", clients.PendingLinks)
	pending("typed auth client header copies to record", clients.PendingHeaders)
	pending("typed auth client decisions unresolved", clients.Gaps)
	pending("typed auth clients or links to retire", clients.PendingRetiredClients+clients.PendingRetiredLinks)
	pending("servers to mark as requiring upstream auth", clients.PendingMarkedServers)
	pending("credential choices to map onto auth clients", clients.PendingSelectionMappings)
	pending("default auth permission grants to write", clients.PendingGrants)
	pending("agent attachment grants to write", clients.AttachmentsWaiting)
	for _, class := range slices.Sorted(maps.Keys(clients.Selections)) {
		pending(fmt.Sprintf("%s credential choices without a usable auth client", class), clients.Selections[class].Gaps())
	}
	for _, remediation := range clients.AttachmentRemediations {
		problems = append(problems, fmt.Sprintf("agent %s attachment %s needs remediation: %s", remediation.AgentID, remediation.BindingID, remediation.Reason))
	}

	profiles, err := executionprofiles.Backfill(ctx, db, organizationID, false)
	if err != nil {
		return fmt.Errorf("check execution selection profiles: %w", err)
	}
	pending("execution profile versions to record", profiles.VersionsWritten)
	for _, execCtx := range executionprofiles.Contexts {
		for _, reason := range slices.Sorted(maps.Keys(profiles.Gaps[execCtx])) {
			pending(fmt.Sprintf("%s execution profile servers without a choice (%s)", execCtx, reason), profiles.Gaps[execCtx][reason])
		}
	}

	if len(problems) > 0 {
		return errors.New("demo seed left typed upstream auth incomplete: " + strings.Join(problems, "; "))
	}
	return nil
}

// seededSelectorWrites admits the seeded organization's upstream_client
// selector writes, so the seed's backfills write agent attachment grants as
// though the selector write rollout covered it.
func seededSelectorWrites(organizationID string) authz.UpstreamSelectorWriteGate {
	return func(_ context.Context, id string) (bool, error) {
		return id == organizationID, nil
	}
}

func phaseErrors(errs map[string]string) string {
	phases := slices.Sorted(maps.Keys(errs))
	out := make([]string, 0, len(phases))
	for _, phase := range phases {
		out = append(out, phase+": "+errs[phase])
	}
	return strings.Join(out, ", ")
}
