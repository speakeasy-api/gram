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
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// AliasBackfillActor is the system principal the alias endpoint backfill audits as.
const AliasBackfillActor = "legacy-alias-endpoint-backfill"

// ErrAliasBackfillCommitUnknown marks a server whose commit failed in a way that leaves its outcome unknown.
var ErrAliasBackfillCommitUnknown = errors.New("commit outcome unknown")

// ErrAliasBackfillNotRecorded marks a server rolled back because its pending writes could not be recorded.
var ErrAliasBackfillNotRecorded = errors.New("pending writes not recorded; rolled back")

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

// AliasMove records the backfill moving a toolset's alias, so a rollback can restore it.
type AliasMove struct {
	// From is the alias before the run.
	From legacyalias.Alias

	// To is the alias after the run.
	To legacyalias.Alias
}

// AliasBackfillResult is the outcome for one aliased toolset.
type AliasBackfillResult struct {
	// Rows has one entry per address the alias serves.
	Rows []AliasBackfillRow

	// AliasMove is set when the run moved the alias.
	AliasMove *AliasMove
}

// BackfillAliasEndpoints gives a legacy toolset's canonical server an endpoint at every address its alias serves that
// no live endpoint holds, in one transaction under the hosted endpoint create path's locks and rules, except the
// organization slug prefix: the alias already serves each address, so its slug is kept as clients hold it. Without
// apply it runs the same transaction and rolls it back. Audit rows are written through auditLogger. With apply,
// beforeCommit receives the writes about to commit; an error from it rolls them back and returns
// ErrAliasBackfillNotRecorded. A failed commit returns the planned result with ErrAliasBackfillCommitUnknown.
func BackfillAliasEndpoints(ctx context.Context, db *pgxpool.Pool, logger *slog.Logger, auditLogger *audit.Logger, candidate AliasBackfillCandidate, apply bool, beforeCommit func(AliasBackfillResult) error) (AliasBackfillResult, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return AliasBackfillResult{Rows: nil, AliasMove: nil}, fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	result, err := backfillAliasEndpointsInTx(ctx, tx, logger, auditLogger, candidate)
	if err != nil {
		return AliasBackfillResult{Rows: nil, AliasMove: nil}, err
	}
	if !apply {
		return result, nil
	}
	if beforeCommit != nil {
		if err := beforeCommit(result); err != nil {
			return result, fmt.Errorf("%w: %w", ErrAliasBackfillNotRecorded, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return result, fmt.Errorf("%w: %w", ErrAliasBackfillCommitUnknown, err)
	}
	return result, nil
}

func backfillAliasEndpointsInTx(ctx context.Context, tx pgx.Tx, logger *slog.Logger, auditLogger *audit.Logger, candidate AliasBackfillCandidate) (AliasBackfillResult, error) {
	var result AliasBackfillResult
	authCtx := &contextvalues.AuthContext{
		ActiveOrganizationID: candidate.OrganizationID, UserID: "", ExternalUserID: "", APIKeyID: "", APIKeyName: "",
		OrgWidePluginHooksKey: false, SessionID: nil, ProjectID: &candidate.ProjectID, OrganizationSlug: "", Email: nil,
		AccountType: "", HasActiveSubscription: false, Whitelisted: false, ProjectSlug: nil, APIKeyScopes: nil, IsAdmin: false,
		SupportOrganizationID: "",
	}
	if err := lockEndpointMutationScope(ctx, tx, authCtx); err != nil {
		return result, fmt.Errorf("lock endpoint mutation scope: %w", err)
	}
	if err := lockHostedToolset(ctx, tx, candidate.ProjectID, candidate.ToolsetID); err != nil {
		return result, err
	}

	own := AliasBackfillRow{
		ToolsetID: candidate.ToolsetID, ProjectID: candidate.ProjectID, Slug: "", CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		PlatformForCustomDomain: false, Action: AliasBackfillSkip, Reason: "", EndpointID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}
	skipAll := func(reason string) (AliasBackfillResult, error) {
		own.Reason = reason
		return AliasBackfillResult{Rows: []AliasBackfillRow{own}, AliasMove: nil}, nil
	}

	state, err := legacyaliasrepo.New(tx).GetAliasEndpointBackfillState(ctx, legacyaliasrepo.GetAliasEndpointBackfillStateParams{ID: candidate.ToolsetID, ProjectID: candidate.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return skipAll("toolset not found in its project")
	case err != nil:
		return result, fmt.Errorf("load toolset alias: %w", err)
	}
	own.Slug, own.CustomDomainID = state.McpSlug.String, state.CustomDomainID
	switch {
	case state.ToolsetOrganizationID != candidate.OrganizationID || state.ProjectOrganizationID != candidate.OrganizationID:
		return skipAll("toolset or project organization changed; rerun")
	case !state.OrganizationSlug.Valid || !state.GramAccountType.Valid:
		return skipAll("organization metadata missing")
	case !state.McpSlug.Valid:
		return skipAll("toolset no longer has an alias")
	case state.ToolsetDeleted:
		return skipAll("toolset deleted")
	case state.ProjectDeleted:
		return skipAll("project deleted")
	}
	authCtx.OrganizationSlug = state.OrganizationSlug.String
	authCtx.AccountType = state.GramAccountType.String

	// Domains lock before the server row, as in the create path. The organization's domain is locked too, so the
	// lockdown read below cannot race an allowlist change.
	orgDomainID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	switch orgDomain, err := customdomainsrepo.New(tx).GetCustomDomainByOrganization(ctx, candidate.OrganizationID); {
	case err == nil:
		orgDomainID = uuid.NullUUID{UUID: orgDomain.ID, Valid: true}
	case !errors.Is(err, pgx.ErrNoRows):
		return result, fmt.Errorf("load organization custom domain: %w", err)
	}
	liveDomains := make(map[uuid.UUID]bool)
	for _, domainID := range uniqueIDs(state.CustomDomainID, orgDomainID) {
		switch _, err := customdomainsrepo.New(tx).LockCustomDomainByID(ctx, domainID); {
		case err == nil:
			liveDomains[domainID] = true
		case !errors.Is(err, pgx.ErrNoRows):
			return result, fmt.Errorf("lock custom domain: %w", err)
		}
	}
	lockdown := false
	switch orgDomain, err := customdomainsrepo.New(tx).GetCustomDomainByOrganization(ctx, candidate.OrganizationID); {
	case err == nil && (!orgDomainID.Valid || orgDomain.ID != orgDomainID.UUID):
		return skipAll("organization custom domain changed; rerun")
	case err == nil:
		lockdown = len(orgDomain.IpAllowlist) > 0
	case !errors.Is(err, pgx.ErrNoRows):
		return result, fmt.Errorf("load organization custom domain: %w", err)
	case orgDomainID.Valid:
		return skipAll("organization custom domain changed; rerun")
	}

	server, err := mcpserversrepo.New(tx).LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{ID: candidate.ToolsetID, ProjectID: candidate.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return skipAll("no live canonical server")
	case err != nil:
		return result, fmt.Errorf("lock canonical server: %w", err)
	case !isCanonicalHosted(server):
		return skipAll("server is not the toolset's canonical server")
	}
	// The fallback refuses a server its network mode keeps off the public host; a disabled server still counts, since
	// the alias serves again when it is enabled.
	mode, err := networkaccess.Effective(server.NetworkAccessMode)
	switch {
	case err != nil:
		return skipAll("invalid network mode")
	case !mode.Allows(networkaccess.SurfacePublic):
		return skipAll("network mode does not serve the public host")
	}

	ownDomainDeleted := state.CustomDomainID.Valid && !liveDomains[state.CustomDomainID.UUID]
	rows := []AliasBackfillRow{own}
	if ownDomainDeleted {
		rows[0].Reason = "custom domain deleted"
	}
	if state.CustomDomainID.Valid {
		platform := own
		platform.CustomDomainID = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
		platform.PlatformForCustomDomain = true
		reason, err := loosePlatformSkipReason(ctx, tx, candidate, state.McpSlug, lockdown)
		if err != nil {
			return result, err
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
			return result, err
		}
		created = created || rows[i].Action == AliasBackfillCreate
	}
	result.Rows = rows

	// The alias follows the endpoints only when its own address is now an endpoint or can no longer serve; it never
	// leaves a custom domain the backfill could not claim.
	ownSettled := rows[0].Action == AliasBackfillCreate || rows[0].Action == AliasBackfillAlreadyHeld || ownDomainDeleted
	if !created || !ownSettled {
		return result, nil
	}
	before := legacyalias.Alias{Slug: state.McpSlug, CustomDomainID: state.CustomDomainID}
	if err := legacyalias.FollowAlias(ctx, tx, candidate.OrganizationID, candidate.ProjectID, server.ID); err != nil {
		return result, fmt.Errorf("follow alias: %w", err)
	}
	after, err := legacyalias.Get(ctx, tx, candidate.ProjectID, candidate.ToolsetID)
	if err != nil {
		return result, fmt.Errorf("reload alias: %w", err)
	}
	if after == before {
		return result, nil
	}
	toolset, err := toolsetsrepo.New(tx).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: candidate.ToolsetID, ProjectID: candidate.ProjectID})
	if err != nil {
		return result, fmt.Errorf("load toolset: %w", err)
	}
	if err := auditLogger.LogToolsetMoveLegacyAlias(ctx, tx, audit.LogToolsetMoveLegacyAliasEvent{
		OrganizationID: candidate.OrganizationID, ProjectID: candidate.ProjectID, Actor: urn.NewSystemPrincipal(AliasBackfillActor),
		ActorDisplayName: nil, ActorSlug: nil, ToolsetURN: urn.NewToolset(toolset.ID), ToolsetName: toolset.Name, ToolsetSlug: toolset.Slug,
		LegacyAliasSnapshotBefore: auditAliasAddress(before), LegacyAliasSnapshotAfter: auditAliasAddress(after),
	}); err != nil {
		return result, fmt.Errorf("audit alias move: %w", err)
	}
	result.AliasMove = &AliasMove{From: before, To: after}
	return result, nil
}

func auditAliasAddress(alias legacyalias.Alias) *audit.ToolsetLegacyAliasAddress {
	address := &audit.ToolsetLegacyAliasAddress{Slug: alias.Slug.String, CustomDomainID: nil}
	if alias.CustomDomainID.Valid {
		address.CustomDomainID = &alias.CustomDomainID.UUID
	}
	return address
}

// loosePlatformSkipReason reports why the platform host does not serve a custom-domain alias, or "" when it does.
func loosePlatformSkipReason(ctx context.Context, tx pgx.Tx, candidate AliasBackfillCandidate, slug pgtype.Text, lockdown bool) (string, error) {
	contenders, err := legacyaliasrepo.New(tx).GetPlatformAliasContenders(ctx, legacyaliasrepo.GetPlatformAliasContendersParams{Slug: slug, ToolsetID: candidate.ToolsetID})
	if err != nil {
		return "", fmt.Errorf("check platform alias contenders: %w", err)
	}
	switch {
	case contenders.PlatformAliasHeld:
		return "not served on platform: a platform alias holds the slug", nil
	case contenders.Ambiguous:
		return "not served on platform: another custom-domain alias has the slug", nil
	case lockdown:
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
		ActorDisplayName: nil, ActorSlug: nil, McpEndpointURN: urn.NewMcpEndpoint(created.ID), Slug: created.Slug,
	}); err != nil {
		return fmt.Errorf("audit endpoint creation: %w", err)
	}
	row.Action = AliasBackfillCreate
	row.EndpointID = uuid.NullUUID{UUID: created.ID, Valid: true}
	return nil
}
