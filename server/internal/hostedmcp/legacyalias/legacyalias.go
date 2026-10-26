// Package legacyalias is the legacy /mcp/<slug> alias of toolsets created before hosted servers owned their
// endpoints: toolsets.mcp_slug and toolsets.custom_domain_id. It is the only code that reads or writes those
// columns, and AIM-670 deletes it with the slug fallback.
package legacyalias

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/hostedmcp/hostingstate"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/legacyalias/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// None is the alias of a toolset that has none.
func None() hostingstate.Address {
	return hostingstate.Address{Slug: pgtype.Text{String: "", Valid: false}, CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}
}

// Get returns a toolset's alias.
func Get(ctx context.Context, db repo.DBTX, projectID, toolsetID uuid.UUID) (hostingstate.Address, error) {
	row, err := repo.New(db).GetAlias(ctx, repo.GetAliasParams{ID: toolsetID, ProjectID: projectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return None(), nil
	case err != nil:
		return None(), fmt.Errorf("load legacy toolset alias: %w", err)
	}
	return hostingstate.Address{Slug: row.McpSlug, CustomDomainID: row.CustomDomainID}, nil
}

// List returns the aliases of the given live toolsets in a project, keyed by toolset id; toolsets without one are
// left out.
func List(ctx context.Context, db repo.DBTX, projectID uuid.UUID, toolsetIDs []uuid.UUID) (map[uuid.UUID]hostingstate.Address, error) {
	aliases := make(map[uuid.UUID]hostingstate.Address, len(toolsetIDs))
	if len(toolsetIDs) == 0 {
		return aliases, nil
	}
	rows, err := repo.New(db).ListAliases(ctx, repo.ListAliasesParams{ProjectID: projectID, Ids: toolsetIDs})
	if err != nil {
		return nil, fmt.Errorf("list legacy toolset aliases: %w", err)
	}
	for _, row := range rows {
		aliases[row.ID] = hostingstate.Address{Slug: row.McpSlug, CustomDomainID: row.CustomDomainID}
	}
	return aliases, nil
}

// Find returns the live toolset an alias names, with that alias. A valid customDomainID scopes the lookup to that
// domain; otherwise strictPlatform restricts it to platform aliases, and without it a platform alias wins over a
// custom-domain one with the same slug. It returns pgx.ErrNoRows when no toolset has the alias.
func Find(ctx context.Context, db toolsetsrepo.DBTX, slug string, customDomainID uuid.NullUUID, strictPlatform bool) (toolsetsrepo.Toolset, hostingstate.Address, error) {
	q := repo.New(db)
	text := pgtype.Text{String: slug, Valid: true}
	var row repo.FindToolsetByAliasRow
	var err error
	switch {
	case customDomainID.Valid:
		var r repo.FindToolsetByCustomDomainAliasRow
		r, err = q.FindToolsetByCustomDomainAlias(ctx, repo.FindToolsetByCustomDomainAliasParams{McpSlug: text, CustomDomainID: customDomainID})
		row = repo.FindToolsetByAliasRow(r)
	case strictPlatform:
		var r repo.FindToolsetByPlatformAliasRow
		r, err = q.FindToolsetByPlatformAlias(ctx, text)
		row = repo.FindToolsetByAliasRow(r)
	default:
		row, err = q.FindToolsetByAlias(ctx, text)
	}
	if err != nil {
		return toolsetsrepo.Toolset{}, None(), err
	}
	return load(ctx, db, row.ID, row.ProjectID, hostingstate.Address{Slug: row.McpSlug, CustomDomainID: row.CustomDomainID})
}

// FindInProject returns the live toolset in the project whose alias has the slug, or pgx.ErrNoRows.
func FindInProject(ctx context.Context, db toolsetsrepo.DBTX, projectID uuid.UUID, slug string) (toolsetsrepo.Toolset, hostingstate.Address, error) {
	row, err := repo.New(db).FindToolsetByProjectAlias(ctx, repo.FindToolsetByProjectAliasParams{McpSlug: pgtype.Text{String: slug, Valid: true}, ProjectID: projectID})
	if err != nil {
		return toolsetsrepo.Toolset{}, None(), err
	}
	return load(ctx, db, row.ID, row.ProjectID, hostingstate.Address{Slug: row.McpSlug, CustomDomainID: row.CustomDomainID})
}

func load(ctx context.Context, db toolsetsrepo.DBTX, id, projectID uuid.UUID, alias hostingstate.Address) (toolsetsrepo.Toolset, hostingstate.Address, error) {
	toolset, err := toolsetsrepo.New(db).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: id, ProjectID: projectID})
	if err != nil {
		return toolsetsrepo.Toolset{}, None(), err
	}
	return toolset, alias, nil
}

// TakenCheck names an address in the unified MCP slug namespace.
type TakenCheck struct {
	Slug string
	// CustomDomainID selects the namespace: invalid for the platform, else that custom domain.
	CustomDomainID uuid.NullUUID
	// ExcludeToolsetID discounts that toolset's own alias.
	ExcludeToolsetID uuid.NullUUID
	// ExcludeMcpServerID discounts the alias of the toolset backing that server.
	ExcludeMcpServerID uuid.NullUUID
}

// Taken reports whether a live toolset alias holds the address. Callers hold the slug scope lock.
func Taken(ctx context.Context, db repo.DBTX, check TakenCheck) (bool, error) {
	taken, err := repo.New(db).AliasTaken(ctx, repo.AliasTakenParams{
		Slug: pgtype.Text{String: check.Slug, Valid: true}, CustomDomainID: check.CustomDomainID,
		ExcludeToolsetID: check.ExcludeToolsetID, ExcludeMcpServerID: check.ExcludeMcpServerID,
	})
	if err != nil {
		return false, fmt.Errorf("check legacy toolset alias: %w", err)
	}
	return taken, nil
}

// AliasAddress is the address a legacy toolset's alias follows among its hosted server's live endpoints: its
// current address while an endpoint still holds it, else the platform endpoint, else the oldest one. With no live
// endpoint the alias stays. This is the address rule pods that mirror the toolset columns apply, so both agree.
func AliasAddress(alias hostingstate.Address, endpoints []mcpendpointsrepo.McpEndpoint) (pgtype.Text, uuid.NullUUID) {
	var preferred *mcpendpointsrepo.McpEndpoint
	for i := range endpoints {
		candidate := &endpoints[i]
		switch {
		case alias.Slug.Valid && candidate.Slug == alias.Slug.String && candidate.CustomDomainID == alias.CustomDomainID:
			return alias.Slug, alias.CustomDomainID
		case preferred == nil:
			preferred = candidate
		case !candidate.CustomDomainID.Valid && preferred.CustomDomainID.Valid:
			preferred = candidate
		case candidate.CustomDomainID.Valid == preferred.CustomDomainID.Valid && candidate.CreatedAt.Time.Before(preferred.CreatedAt.Time):
			preferred = candidate
		}
	}
	if preferred == nil {
		return alias.Slug, alias.CustomDomainID
	}
	return pgtype.Text{String: preferred.Slug, Valid: true}, preferred.CustomDomainID
}

// FollowAlias moves a legacy toolset's alias to the endpoint AliasAddress picks, after a write to its hosted
// server's endpoints. Toolsets without an alias are left alone. The caller holds the toolset row lock.
func FollowAlias(ctx context.Context, tx repo.DBTX, organizationID string, projectID, toolsetID uuid.UUID) error {
	alias, err := Get(ctx, tx, projectID, toolsetID)
	switch {
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "load hosted toolset alias")
	case !alias.Slug.Valid || alias.Slug.String == "":
		return nil
	}
	rows, err := mcpendpointsrepo.New(tx).ListMCPEndpointsOnLiveDomainsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsOnLiveDomainsByMCPServerIDParams{
		OrganizationID: organizationID, ProjectID: projectID, McpServerID: toolsetID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list hosted MCP endpoints")
	}
	live := make([]mcpendpointsrepo.McpEndpoint, 0, len(rows))
	for _, row := range rows {
		live = append(live, row.McpEndpoint)
	}
	slug, domainID := AliasAddress(alias, live)
	if slug == alias.Slug && domainID == alias.CustomDomainID {
		return nil
	}
	if err := repo.New(tx).MoveAlias(ctx, repo.MoveAliasParams{McpSlug: slug, CustomDomainID: domainID, ID: toolsetID, ProjectID: projectID}); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return oops.E(oops.CodeConflict, err, "this address is already taken by another toolset")
		}
		return oops.E(oops.CodeUnexpected, err, "move hosted MCP alias")
	}
	return nil
}

// SetFixture sets or clears a toolset's alias, for tests and seeds of legacy toolsets.
func SetFixture(ctx context.Context, db repo.DBTX, projectID, toolsetID uuid.UUID, alias hostingstate.Address) error {
	if err := repo.New(db).SetAliasFixture(ctx, repo.SetAliasFixtureParams{McpSlug: alias.Slug, CustomDomainID: alias.CustomDomainID, ID: toolsetID, ProjectID: projectID}); err != nil {
		return fmt.Errorf("set legacy toolset alias: %w", err)
	}
	return nil
}
