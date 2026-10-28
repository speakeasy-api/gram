package mcpendpoints

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/legacyalias"
	legacyaliasrepo "github.com/speakeasy-api/gram/server/internal/hostedmcp/legacyalias/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// AliasBackfillActor is the system principal the alias endpoint backfill audits as.
const AliasBackfillActor = "legacy-alias-endpoint-backfill"

// AliasBackfillAction is what the alias endpoint backfill does, or would do, for one address.
type AliasBackfillAction string

const (
	// AliasBackfillCreate gives the canonical server an endpoint at the address.
	AliasBackfillCreate AliasBackfillAction = "create"

	// AliasBackfillAlreadyHeld means the canonical server already has an endpoint at the address.
	AliasBackfillAlreadyHeld AliasBackfillAction = "already_held"

	// AliasBackfillConflict means another server's endpoint or another toolset's alias holds the address.
	AliasBackfillConflict AliasBackfillAction = "conflict"

	// AliasBackfillSkip means the alias does not serve the address, or a hosted address rule rejects it.
	AliasBackfillSkip AliasBackfillAction = "skip"
)

// AliasBackfillCandidate names one aliased toolset.
type AliasBackfillCandidate struct {
	// ToolsetID is the toolset, and the id of its canonical server.
	ToolsetID uuid.UUID

	// ProjectID is the toolset's project.
	ProjectID uuid.UUID

	// OrganizationID is the toolset's organization.
	OrganizationID string
}

// AliasBackfillRow is the outcome for one address an alias serves.
type AliasBackfillRow struct {
	// ToolsetID is the toolset and its canonical server id.
	ToolsetID uuid.UUID

	// ProjectID is the toolset's project.
	ProjectID uuid.UUID

	// Slug is the address slug; empty when the toolset has no alias anymore.
	Slug string

	// CustomDomainID is the address's custom domain; invalid for the platform host.
	CustomDomainID uuid.NullUUID

	// PlatformForCustomDomain marks the platform address the loose lookup serves for a custom-domain alias.
	PlatformForCustomDomain bool

	// Action is what the backfill did, or would do.
	Action AliasBackfillAction

	// Reason explains a skip or conflict.
	Reason string

	// EndpointID is the endpoint created, or the one already holding the address.
	EndpointID uuid.NullUUID
}

// BackfillAliasEndpoints gives a legacy toolset's canonical server an endpoint at every address its alias serves that
// no live endpoint holds, in one transaction under the hosted endpoint create path's locks and rules, except the
// organization slug prefix: the alias already serves each address, so its slug is kept as clients hold it. Without apply
// it runs the same transaction and rolls it back. Audit rows are written through auditLogger.
func BackfillAliasEndpoints(ctx context.Context, db *pgxpool.Pool, logger *slog.Logger, auditLogger *audit.Logger, candidate AliasBackfillCandidate, apply bool) ([]AliasBackfillRow, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	rows, err := backfillAliasEndpointsInTx(ctx, tx, logger, auditLogger, candidate)
	if err != nil {
		return nil, err
	}
	if !apply {
		return rows, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}
	return rows, nil
}

func backfillAliasEndpointsInTx(ctx context.Context, tx pgx.Tx, logger *slog.Logger, auditLogger *audit.Logger, candidate AliasBackfillCandidate) ([]AliasBackfillRow, error) {
	authCtx := &contextvalues.AuthContext{
		ActiveOrganizationID: candidate.OrganizationID, UserID: "", ExternalUserID: "", APIKeyID: "", APIKeyName: "",
		OrgWidePluginHooksKey: false, SessionID: nil, ProjectID: &candidate.ProjectID, OrganizationSlug: "", Email: nil,
		AccountType: "", HasActiveSubscription: false, Whitelisted: false, ProjectSlug: nil, APIKeyScopes: nil, IsAdmin: false,
		SupportOrganizationID: "",
	}
	if err := lockEndpointMutationScope(ctx, tx, authCtx); err != nil {
		return nil, fmt.Errorf("lock endpoint mutation scope: %w", err)
	}
	if err := lockHostedToolset(ctx, tx, candidate.ProjectID, candidate.ToolsetID); err != nil {
		return nil, err
	}

	state, err := legacyaliasrepo.New(tx).GetAliasEndpointBackfillState(ctx, legacyaliasrepo.GetAliasEndpointBackfillStateParams{ID: candidate.ToolsetID, ProjectID: candidate.ProjectID})
	if err != nil {
		return nil, fmt.Errorf("load toolset alias: %w", err)
	}
	own := AliasBackfillRow{
		ToolsetID: candidate.ToolsetID, ProjectID: candidate.ProjectID, Slug: state.McpSlug.String, CustomDomainID: state.CustomDomainID,
		PlatformForCustomDomain: false, Action: AliasBackfillSkip, Reason: "", EndpointID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}
	skipAll := func(reason string) []AliasBackfillRow {
		own.Reason = reason
		return []AliasBackfillRow{own}
	}
	switch {
	case state.OrganizationID != candidate.OrganizationID:
		return nil, errors.New("toolset organization changed during the run")
	case !state.McpSlug.Valid:
		return skipAll("toolset no longer has an alias"), nil
	case state.ToolsetDeleted:
		return skipAll("toolset deleted"), nil
	case state.ProjectDeleted:
		return skipAll("project deleted"), nil
	}
	authCtx.OrganizationSlug = state.OrganizationSlug
	authCtx.AccountType = state.GramAccountType

	ownDomainLive := true
	if state.CustomDomainID.Valid {
		switch err := lockCustomDomains(ctx, tx, []uuid.UUID{state.CustomDomainID.UUID}); {
		case errors.Is(err, pgx.ErrNoRows):
			ownDomainLive = false
		case err != nil:
			return nil, err
		}
	}

	server, err := mcpserversrepo.New(tx).LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{ID: candidate.ToolsetID, ProjectID: candidate.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return skipAll("no live canonical server"), nil
	case err != nil:
		return nil, fmt.Errorf("lock canonical server: %w", err)
	case !isCanonicalHosted(server):
		return skipAll("server is not the toolset's canonical server"), nil
	}

	rows := []AliasBackfillRow{own}
	if !ownDomainLive {
		rows[0].Reason = "custom domain deleted"
	}
	if state.CustomDomainID.Valid {
		platform := own
		platform.CustomDomainID = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
		platform.PlatformForCustomDomain = true
		reason, err := loosePlatformSkipReason(ctx, tx, candidate, state.McpSlug)
		if err != nil {
			return nil, err
		}
		platform.Reason = reason
		rows = append(rows, platform)
	}

	created := false
	for i := range rows {
		if rows[i].Reason != "" {
			continue
		}
		if err := backfillAliasAddress(ctx, tx, logger, auditLogger, authCtx, server, &rows[i]); err != nil {
			return nil, err
		}
		created = created || rows[i].Action == AliasBackfillCreate
	}
	if created {
		if err := legacyalias.FollowAlias(ctx, tx, candidate.OrganizationID, candidate.ProjectID, server.ID); err != nil {
			return nil, fmt.Errorf("follow alias: %w", err)
		}
	}
	return rows, nil
}

// loosePlatformSkipReason reports why the platform host does not serve a custom-domain alias, or "" when it does.
func loosePlatformSkipReason(ctx context.Context, tx pgx.Tx, candidate AliasBackfillCandidate, slug pgtype.Text) (string, error) {
	contenders, err := legacyaliasrepo.New(tx).GetPlatformAliasContenders(ctx, legacyaliasrepo.GetPlatformAliasContendersParams{Slug: slug, ToolsetID: candidate.ToolsetID})
	if err != nil {
		return "", fmt.Errorf("check platform alias contenders: %w", err)
	}
	switch {
	case contenders.PlatformAliasHeld:
		return "not served on platform: a platform alias holds the slug", nil
	case contenders.Ambiguous:
		return "not served on platform: another custom-domain alias has the slug", nil
	}
	domain, err := customdomainsrepo.New(tx).GetCustomDomainByOrganization(ctx, candidate.OrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("load organization custom domain: %w", err)
	case len(domain.IpAllowlist) > 0:
		return "not served on platform: custom domain lockdown", nil
	}
	return "", nil
}

// backfillAliasAddress claims one address for the server under the hosted create rules, filling in row.
func backfillAliasAddress(ctx context.Context, tx pgx.Tx, logger *slog.Logger, auditLogger *audit.Logger, authCtx *contextvalues.AuthContext, server mcpserversrepo.McpServer, row *AliasBackfillRow) error {
	if err := LockSlugScope(ctx, tx, row.CustomDomainID, row.Slug); err != nil {
		return err
	}

	holder, err := repo.New(tx).GetMCPEndpointByCustomDomainAndSlug(ctx, repo.GetMCPEndpointByCustomDomainAndSlugParams{Slug: row.Slug, CustomDomainID: row.CustomDomainID})
	switch {
	case err == nil && holder.McpServerID.Valid && holder.McpServerID.UUID == server.ID:
		row.Action = AliasBackfillAlreadyHeld
		row.EndpointID = uuid.NullUUID{UUID: holder.ID, Valid: true}
		return nil
	case err == nil:
		row.Action = AliasBackfillConflict
		row.Reason = "another server's endpoint holds the address"
		row.EndpointID = uuid.NullUUID{UUID: holder.ID, Valid: true}
		return nil
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("load endpoint at address: %w", err)
	}

	if err := checkHostedNewAddress(ctx, tx, logger, authCtx, server, row.Slug, row.CustomDomainID, slugPrefixExemptLegacyAlias); err != nil {
		var shareable *oops.ShareableError
		if errors.As(err, &shareable) && shareable.Code != oops.CodeUnexpected {
			row.Reason = shareable.Error()
			return nil
		}
		return fmt.Errorf("check hosted address: %w", err)
	}

	available, err := CheckSlugAvailable(ctx, tx, SlugAvailabilityCheck{
		Slug: row.Slug, CustomDomainID: row.CustomDomainID, OrganizationID: authCtx.ActiveOrganizationID,
		ExcludeToolsetID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ExcludeMcpServerID: uuid.NullUUID{UUID: server.ID, Valid: true}, SkipDomainOwnershipCheck: false,
	})
	if err != nil {
		return err
	}
	if !available {
		row.Action = AliasBackfillConflict
		row.Reason = "another toolset's alias holds the address"
		return nil
	}

	created, err := repo.New(tx).CreateMCPEndpoint(ctx, repo.CreateMCPEndpointParams{
		ProjectID: server.ProjectID, CustomDomainID: row.CustomDomainID, McpServerID: uuid.NullUUID{UUID: server.ID, Valid: true},
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Slug: row.Slug,
	})
	if err != nil {
		return fmt.Errorf("create endpoint: %w", err)
	}
	if err := auditLogger.LogMcpEndpointCreate(ctx, tx, audit.LogMcpEndpointCreateEvent{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: server.ProjectID, Actor: urn.NewSystemPrincipal(AliasBackfillActor),
		ActorDisplayName: new("Speakeasy"), ActorSlug: nil, McpEndpointURN: urn.NewMcpEndpoint(created.ID), Slug: created.Slug,
	}); err != nil {
		return fmt.Errorf("audit endpoint creation: %w", err)
	}
	row.Action = AliasBackfillCreate
	row.EndpointID = uuid.NullUUID{UUID: created.ID, Valid: true}
	return nil
}
