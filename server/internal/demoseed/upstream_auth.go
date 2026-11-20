package demoseed

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/authenforcement"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/executionprofiles"
)

// seedUpstreamAuthGrants provisions only grants for the typed offers that the
// fixture has already inserted in this transaction.
func seedUpstreamAuthGrants(ctx context.Context, tx pgx.Tx, organizationID string) error {
	ctx = authz.WithUpstreamSelectorWriteGate(ctx, seededSelectorWrites(organizationID))
	if err := authclientbackfill.ProvisionFromTypedOffers(ctx, tx, organizationID); err != nil {
		return fmt.Errorf("provision fixture auth grants: %w", err)
	}
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
	for _, kind := range authclientbackfill.Kinds {
		counts := clients.ByKind[kind]
		pending(fmt.Sprintf("%s auth clients to record", kind), counts.PendingClients)
		pending(fmt.Sprintf("%s auth client server links to record", kind), counts.PendingLinks)
	}
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
