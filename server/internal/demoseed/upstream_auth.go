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
	"github.com/speakeasy-api/gram/server/internal/authenforcement"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/executionprofiles"
)

// migrateUpstreamAuth materializes typed state, then validates typed readiness.
// It runs after the seed commits because the remaining migration phases use
// their own organization lock and transactions. A failed phase fails the
// seed run and the next reseed retries it.
func migrateUpstreamAuth(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, organizationID string) error {
	if err := recordTypedUpstreamAuth(ctx, logger, db, organizationID); err != nil {
		return err
	}

	return upstreamAuthReadiness(ctx, logger, db, organizationID)
}

// recordTypedUpstreamAuth finishes header copies and grant provisioning. The
// seed writes assistant execution profiles in the main transaction.
func recordTypedUpstreamAuth(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, organizationID string) error {
	ctx = authz.WithUpstreamSelectorWriteGate(ctx, seededSelectorWrites(organizationID))
	// The selection backfill reads the organization's enforcement state on its
	// own; the auth client backfill is told it, as the admin migration does.
	enforced, err := authenforcement.ReadEnforced(ctx, db, organizationID)
	if err != nil {
		return fmt.Errorf("record typed auth clients: %w", err)
	}
	clients, err := authclientbackfill.Run(ctx, db, authclientbackfill.Options{OrganizationID: organizationID, Apply: true, PageSize: 0, TypedOnly: enforced})
	if err != nil {
		return fmt.Errorf("record typed auth clients: %w", err)
	}
	if len(clients.PhaseErrors) > 0 {
		return fmt.Errorf("record typed auth clients: failed phases %s", phaseErrors(clients.PhaseErrors))
	}
	logger.InfoContext(ctx, fmt.Sprintf(
		"demo seed recorded typed upstream auth: %d auth clients, %d links, %d header copies, %d choices mapped, %d grants",
		clients.ClientsWritten, clients.LinksWritten, clients.HeadersWritten, clients.SelectionsMapped, clients.GrantsWritten,
	), attr.SlogOrganizationID(organizationID))

	return nil
}

// upstreamAuthReadiness checks for missing or unusable typed state after seeding.
func upstreamAuthReadiness(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, organizationID string) error {
	ctx = authz.WithUpstreamSelectorWriteGate(ctx, seededSelectorWrites(organizationID))

	var problems []string
	pending := func(what string, n int) {
		if n != 0 {
			problems = append(problems, fmt.Sprintf("%d %s", n, what))
		}
	}

	enforced, err := authenforcement.ReadEnforced(ctx, db, organizationID)
	if err != nil {
		return fmt.Errorf("check typed auth clients: %w", err)
	}
	clients, err := authclientbackfill.Run(ctx, db, authclientbackfill.Options{OrganizationID: organizationID, Apply: false, PageSize: 0, TypedOnly: enforced})
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
