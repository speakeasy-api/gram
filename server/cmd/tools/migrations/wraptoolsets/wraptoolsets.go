// Package wraptoolsets implements the admin migration that wraps every live
// toolset still publishing directly through the toolsets.mcp_slug column in a
// toolset-backed mcp_servers row plus a single mcp_endpoints row, moving
// dependent mcp_metadata and collection attachment ownership onto the
// wrapper. See WRAPTOOLSETS.md for the operator runbook.
package wraptoolsets

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/wraptoolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// idNamespace seeds the UUIDv5 derivation of wrapper row ids from toolset
// ids. Changing it (or the versioned labels below) changes every derived id
// and breaks rerun idempotency, so all three are frozen.
var idNamespace = uuid.MustParse("6fa0b614-f04c-4eaf-9605-254509c957df")

const (
	serverIDLabel   = "wraptoolsets:v1:server:"
	endpointIDLabel = "wraptoolsets:v1:endpoint:"
)

// runLockKey serializes concurrent runs via pg_advisory_xact_lock, derived
// from the id namespace so unrelated tooling cannot collide on it by chance.
var runLockKey = int64(binary.BigEndian.Uint64(idNamespace[:8]))

func deriveServerID(toolsetID uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(idNamespace, []byte(serverIDLabel+toolsetID.String()))
}

func deriveEndpointID(toolsetID uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(idNamespace, []byte(endpointIDLabel+toolsetID.String()))
}

// deriveServerSlug appends the first 8 hex chars of the compact toolset id to
// the toolset slug, giving a stable project-unique internal server slug that
// never collides with a plain toolset slug reused across candidates.
func deriveServerSlug(toolsetSlug string, toolsetID uuid.UUID) string {
	return toolsetSlug + "-" + hex.EncodeToString(toolsetID[:4])
}

const (
	visibilityDisabled = "disabled"
	visibilityPrivate  = "private"
	visibilityPublic   = "public"
)

// mapVisibility maps the toolset publishing flags onto mcp_servers.visibility;
// disabled wins regardless of mcp_is_public.
func mapVisibility(mcpEnabled, mcpIsPublic bool) string {
	switch {
	case !mcpEnabled:
		return visibilityDisabled
	case mcpIsPublic:
		return visibilityPublic
	default:
		return visibilityPrivate
	}
}

type Outcome string

const (
	OutcomeCreated                  Outcome = "created"
	OutcomeWouldCreate              Outcome = "would_create"
	OutcomeAlreadyComplete          Outcome = "already_complete"
	OutcomeBlockedCollision         Outcome = "blocked_collision"
	OutcomeBlockedEnvironment       Outcome = "blocked_environment"
	OutcomeBlockedDeadDomain        Outcome = "blocked_dead_domain"
	OutcomeBlockedAmbiguousWrapper  Outcome = "blocked_ambiguous_wrapper"
	OutcomeBlockedDependentConflict Outcome = "blocked_dependent_conflict"
	OutcomeBlockedChanged           Outcome = "blocked_changed"
)

type Options struct {
	// DryRun performs every read and guard but no writes, reporting
	// would_create instead of created.
	DryRun bool
	// After resumes the keyset scan strictly after this toolset id.
	After uuid.NullUUID
	// Limit caps the number of candidates processed this run; 0 means all.
	Limit int64
	// ProjectID restricts the candidate set to one project.
	ProjectID uuid.NullUUID
	// ClearDeadDomain nulls a candidate's custom_domain_id when the referenced
	// domain row is soft-deleted, wrapping the toolset as a platform candidate.
	ClearDeadDomain bool
}

type RowResult struct {
	ToolsetID         uuid.UUID  `json:"toolset_id"`
	ProjectID         uuid.UUID  `json:"project_id"`
	Slug              string     `json:"slug"`
	Outcome           Outcome    `json:"outcome"`
	Reason            string     `json:"reason,omitempty"`
	McpServerID       *uuid.UUID `json:"mcp_server_id,omitempty"`
	McpEndpointID     *uuid.UUID `json:"mcp_endpoint_id,omitempty"`
	ClearedDeadDomain bool       `json:"cleared_dead_domain,omitempty"`
	MetadataMoved     int64      `json:"metadata_moved,omitempty"`
	AttachmentsMoved  int64      `json:"attachments_moved,omitempty"`
}

type Report struct {
	Mode       string          `json:"mode"`
	Counts     map[Outcome]int `json:"counts"`
	Rows       []RowResult     `json:"rows"`
	LastCursor *uuid.UUID      `json:"last_cursor,omitempty"`
}

// Run lists candidates once and processes each in its own short transaction.
// A returned error means the run stopped early; the partial Report is still
// returned so the operator can resume from Report.LastCursor.
func Run(ctx context.Context, pool *pgxpool.Pool, opts Options) (*Report, error) {
	mode := "apply"
	if opts.DryRun {
		mode = "dry-run"
	}
	report := &Report{
		Mode:       mode,
		Counts:     map[Outcome]int{},
		Rows:       nil,
		LastCursor: nil,
	}

	after := uuid.Nil
	if opts.After.Valid {
		after = opts.After.UUID
	}
	candidates, err := repo.New(pool).ListCandidateToolsets(ctx, repo.ListCandidateToolsetsParams{
		AfterID:   after,
		ProjectID: opts.ProjectID,
		RowLimit:  opts.Limit,
	})
	if err != nil {
		return report, fmt.Errorf("list candidate toolsets: %w", err)
	}

	for _, cand := range candidates {
		row, err := processCandidate(ctx, pool, cand, opts)
		if err != nil {
			return report, fmt.Errorf("process toolset %s: %w", cand.ID, err)
		}
		report.Rows = append(report.Rows, row)
		report.Counts[row.Outcome]++
		cursor := cand.ID
		report.LastCursor = &cursor
	}

	return report, nil
}

// processCandidate runs the full guard-and-write sequence for one toolset in
// one transaction. Guard failures roll back with a structured outcome; only a
// successful apply commits. Dry-run always rolls back.
func processCandidate(ctx context.Context, pool *pgxpool.Pool, cand repo.ListCandidateToolsetsRow, opts Options) (RowResult, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return RowResult{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	q := repo.New(tx)
	if err := q.AcquireWrapRunLock(ctx, runLockKey); err != nil {
		return RowResult{}, fmt.Errorf("acquire run advisory lock: %w", err)
	}

	res := RowResult{
		ToolsetID:         cand.ID,
		ProjectID:         cand.ProjectID,
		Slug:              "",
		Outcome:           "",
		Reason:            "",
		McpServerID:       nil,
		McpEndpointID:     nil,
		ClearedDeadDomain: false,
		MetadataMoved:     0,
		AttachmentsMoved:  0,
	}
	block := func(outcome Outcome, reason string) (RowResult, error) {
		res.Outcome = outcome
		res.Reason = reason
		return res, nil
	}

	toolset, err := q.LockCandidateToolset(ctx, repo.LockCandidateToolsetParams(cand))
	if errors.Is(err, pgx.ErrNoRows) {
		return block(OutcomeBlockedChanged, "toolset no longer matches the candidate criteria under lock")
	}
	if err != nil {
		return RowResult{}, fmt.Errorf("lock candidate toolset: %w", err)
	}
	res.Slug = toolset.McpSlug.String

	if _, err := q.GetLiveProject(ctx, toolset.ProjectID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return block(OutcomeBlockedChanged, "project is no longer live")
		}
		return RowResult{}, fmt.Errorf("load project: %w", err)
	}

	effectiveDomain := toolset.CustomDomainID
	clearDeadDomain := false
	if toolset.CustomDomainID.Valid {
		domain, err := q.GetCustomDomainState(ctx, toolset.CustomDomainID.UUID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return block(OutcomeBlockedDeadDomain, "custom_domain_id references a missing custom_domains row")
		case err != nil:
			return RowResult{}, fmt.Errorf("load custom domain: %w", err)
		case domain.OrganizationID != toolset.OrganizationID:
			return block(OutcomeBlockedDeadDomain, "custom_domain_id references a domain in a different organization")
		case domain.Deleted && !opts.ClearDeadDomain:
			return block(OutcomeBlockedDeadDomain, "custom domain is soft-deleted; rerun with -clear-dead-domain to publish on the platform host")
		case domain.Deleted:
			effectiveDomain = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
			clearDeadDomain = true
		}
	}

	environmentID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if toolset.DefaultEnvironmentSlug.Valid {
		envID, err := q.ResolveDefaultEnvironment(ctx, repo.ResolveDefaultEnvironmentParams{
			ProjectID: toolset.ProjectID,
			Slug:      toolset.DefaultEnvironmentSlug.String,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return block(OutcomeBlockedEnvironment, fmt.Sprintf("default_environment_slug %q does not resolve to a live environment in the project", toolset.DefaultEnvironmentSlug.String))
		}
		if err != nil {
			return RowResult{}, fmt.Errorf("resolve default environment: %w", err)
		}
		environmentID = uuid.NullUUID{UUID: envID, Valid: true}
	}

	visibility := mapVisibility(toolset.McpEnabled, toolset.McpIsPublic)

	wrappers, err := q.ListLiveToolsetWrappers(ctx, repo.ListLiveToolsetWrappersParams{
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ProjectID: toolset.ProjectID,
	})
	if err != nil {
		return RowResult{}, fmt.Errorf("list toolset wrappers: %w", err)
	}

	occupants, err := q.FindLiveEndpointAtAddress(ctx, repo.FindLiveEndpointAtAddressParams{
		Slug:           toolset.McpSlug.String,
		CustomDomainID: effectiveDomain,
	})
	if err != nil {
		return RowResult{}, fmt.Errorf("find endpoint at toolset address: %w", err)
	}

	var serverID, endpointID uuid.UUID
	creating := false
	switch {
	case len(wrappers) > 1:
		return block(OutcomeBlockedAmbiguousWrapper, fmt.Sprintf("toolset has %d live toolset-backed mcp_servers rows; expected at most one", len(wrappers)))

	case len(wrappers) == 1:
		wrapper := wrappers[0]
		if len(occupants) == 0 || occupants[0].McpServerID != wrapper.ID {
			return block(OutcomeBlockedAmbiguousWrapper, fmt.Sprintf("live wrapper %s does not own the endpoint at the toolset's published address", wrapper.ID))
		}
		if wrapper.Visibility != visibility {
			return block(OutcomeBlockedAmbiguousWrapper, fmt.Sprintf("wrapper %s visibility %q does not match expected %q", wrapper.ID, wrapper.Visibility, visibility))
		}
		if wrapper.EnvironmentID != environmentID {
			return block(OutcomeBlockedAmbiguousWrapper, fmt.Sprintf("wrapper %s environment does not match the toolset's resolved default environment", wrapper.ID))
		}
		serverID = wrapper.ID
		endpointID = occupants[0].ID

	default:
		if len(occupants) > 0 {
			return block(OutcomeBlockedCollision, fmt.Sprintf("endpoint address is already occupied by mcp_endpoints row %s (mcp_server %s)", occupants[0].ID, occupants[0].McpServerID))
		}
		serverID = deriveServerID(toolset.ID)
		endpointID = deriveEndpointID(toolset.ID)

		// A fully matching live row at a derived id would have been adopted
		// through the wrapper branch above, so any occupant here is a mismatch.
		if _, err := q.GetMcpServerRow(ctx, serverID); err == nil {
			return block(OutcomeBlockedDependentConflict, fmt.Sprintf("an mcp_servers row already exists at derived id %s but is not this toolset's live wrapper", serverID))
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return RowResult{}, fmt.Errorf("probe derived mcp_servers id: %w", err)
		}
		if _, err := q.GetMcpEndpointRow(ctx, endpointID); err == nil {
			return block(OutcomeBlockedDependentConflict, fmt.Sprintf("an mcp_endpoints row already exists at derived id %s but is not this toolset's endpoint", endpointID))
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return RowResult{}, fmt.Errorf("probe derived mcp_endpoints id: %w", err)
		}

		serverSlug := deriveServerSlug(toolset.Slug, toolset.ID)
		owner, err := q.FindLiveServerSlugOwner(ctx, repo.FindLiveServerSlugOwnerParams{
			ProjectID: toolset.ProjectID,
			Slug:      conv.ToPGText(serverSlug),
		})
		if err == nil {
			return block(OutcomeBlockedCollision, fmt.Sprintf("internal server slug %q is already taken by mcp_servers row %s", serverSlug, owner.ID))
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return RowResult{}, fmt.Errorf("probe server slug: %w", err)
		}
		creating = true
	}

	res.McpServerID = &serverID
	res.McpEndpointID = &endpointID
	res.ClearedDeadDomain = clearDeadDomain

	metadataRows, err := q.ListToolsetOwnedMetadata(ctx, uuid.NullUUID{UUID: toolset.ID, Valid: true})
	if err != nil {
		return RowResult{}, fmt.Errorf("list toolset metadata: %w", err)
	}
	for _, md := range metadataRows {
		if md.ProjectID != toolset.ProjectID {
			return block(OutcomeBlockedDependentConflict, fmt.Sprintf("mcp_metadata row %s is owned by the toolset but belongs to a different project", md.ID))
		}
	}
	if len(metadataRows) > 0 {
		serverMetadata, err := q.ListServerOwnedMetadata(ctx, repo.ListServerOwnedMetadataParams{
			McpServerID: uuid.NullUUID{UUID: serverID, Valid: true},
			ProjectID:   toolset.ProjectID,
		})
		if err != nil {
			return RowResult{}, fmt.Errorf("list wrapper metadata: %w", err)
		}
		if len(serverMetadata) > 0 {
			return block(OutcomeBlockedDependentConflict, "both the toolset and its wrapper own an mcp_metadata row; refusing to merge")
		}
	}

	conflictingAttachments, err := q.CountConflictingCollectionAttachments(ctx, repo.CountConflictingCollectionAttachmentsParams{
		McpServerID: uuid.NullUUID{UUID: serverID, Valid: true},
		ToolsetID:   uuid.NullUUID{UUID: toolset.ID, Valid: true},
	})
	if err != nil {
		return RowResult{}, fmt.Errorf("count conflicting collection attachments: %w", err)
	}
	if conflictingAttachments > 0 {
		return block(OutcomeBlockedDependentConflict, fmt.Sprintf("%d collection(s) hold live attachments to both the toolset and its wrapper; refusing to dedupe", conflictingAttachments))
	}

	if opts.DryRun {
		attachments, err := q.CountToolsetOwnedCollectionAttachments(ctx, uuid.NullUUID{UUID: toolset.ID, Valid: true})
		if err != nil {
			return RowResult{}, fmt.Errorf("count toolset collection attachments: %w", err)
		}
		res.MetadataMoved = int64(len(metadataRows))
		res.AttachmentsMoved = attachments
		if creating {
			res.Outcome = OutcomeWouldCreate
		} else {
			res.Outcome = OutcomeAlreadyComplete
		}
		return res, nil
	}

	if clearDeadDomain {
		cleared, err := q.ClearToolsetCustomDomain(ctx, repo.ClearToolsetCustomDomainParams{
			ID:        toolset.ID,
			ProjectID: toolset.ProjectID,
		})
		if err != nil {
			return RowResult{}, fmt.Errorf("clear dead custom domain: %w", err)
		}
		if cleared != 1 {
			return RowResult{}, fmt.Errorf("clear dead custom domain: expected 1 row, updated %d", cleared)
		}
	}

	if creating {
		if _, err := q.InsertWrapperMcpServer(ctx, repo.InsertWrapperMcpServerParams{
			ID:            serverID,
			ProjectID:     toolset.ProjectID,
			Name:          conv.ToPGText(toolset.Name),
			Slug:          conv.ToPGText(deriveServerSlug(toolset.Slug, toolset.ID)),
			EnvironmentID: environmentID,
			ToolsetID:     uuid.NullUUID{UUID: toolset.ID, Valid: true},
			Visibility:    visibility,
		}); err != nil {
			return RowResult{}, fmt.Errorf("insert wrapper mcp_servers row: %w", err)
		}
		if _, err := q.InsertWrapperMcpEndpoint(ctx, repo.InsertWrapperMcpEndpointParams{
			ID:             endpointID,
			ProjectID:      toolset.ProjectID,
			CustomDomainID: effectiveDomain,
			McpServerID:    serverID,
			Slug:           toolset.McpSlug.String,
		}); err != nil {
			return RowResult{}, fmt.Errorf("insert wrapper mcp_endpoints row: %w", err)
		}
	}

	if len(metadataRows) > 0 {
		moved, err := q.MoveMetadataOwnershipToServer(ctx, repo.MoveMetadataOwnershipToServerParams{
			McpServerID: uuid.NullUUID{UUID: serverID, Valid: true},
			ToolsetID:   uuid.NullUUID{UUID: toolset.ID, Valid: true},
			ProjectID:   toolset.ProjectID,
		})
		if err != nil {
			return RowResult{}, fmt.Errorf("move metadata ownership: %w", err)
		}
		res.MetadataMoved = moved
	}

	movedAttachments, err := q.MoveCollectionAttachmentOwnershipToServer(ctx, repo.MoveCollectionAttachmentOwnershipToServerParams{
		McpServerID: uuid.NullUUID{UUID: serverID, Valid: true},
		ToolsetID:   uuid.NullUUID{UUID: toolset.ID, Valid: true},
	})
	if err != nil {
		return RowResult{}, fmt.Errorf("move collection attachment ownership: %w", err)
	}
	res.AttachmentsMoved = movedAttachments

	if err := tx.Commit(ctx); err != nil {
		return RowResult{}, fmt.Errorf("commit: %w", err)
	}

	if creating {
		res.Outcome = OutcomeCreated
	} else {
		res.Outcome = OutcomeAlreadyComplete
	}
	return res, nil
}
