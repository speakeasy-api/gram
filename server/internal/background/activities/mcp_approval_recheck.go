package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/evidence"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/evidencediff"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/identity"
	approvalrepo "github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// McpApprovalRecheckPageArgs pages the sweep's scan of approved requests.
type McpApprovalRecheckPageArgs struct {
	AfterID  uuid.UUID
	PageSize int32
}

// McpApprovalRecheckTarget is one approved request the sweep re-gathers,
// carrying both comparison sides: the request's identity for the fresh
// gather and the latest decision's frozen snapshot.
type McpApprovalRecheckTarget struct {
	ID                        uuid.UUID
	ProjectID                 uuid.UUID
	OrganizationID            string
	TargetRaw                 string
	EvidenceCollectedAt       *time.Time
	NotifiedChangeFingerprint string
	DecisionEvidenceSnapshot  json.RawMessage
	DecisionEvidenceVersion   int32
}

// McpApprovalRecheck re-gathers evidence for approved MCP servers and flags
// permission-relevant drift from the snapshot their approval rested on. It
// is a re-review trigger, not a threat control: an unchanged published
// interface says nothing about unchanged behavior.
type McpApprovalRecheck struct {
	logger    *slog.Logger
	db        *pgxpool.Pool
	assembler *evidence.Assembler
	features  *productfeatures.Client
	audit     *audit.Logger
}

// NewMcpApprovalRecheck builds the recheck activities over the same evidence
// assembler the intake path uses, so a sweep gather and a page-view gather
// can never disagree about what a source said.
func NewMcpApprovalRecheck(logger *slog.Logger, db *pgxpool.Pool, assembler *evidence.Assembler, features *productfeatures.Client, auditLogger *audit.Logger) *McpApprovalRecheck {
	return &McpApprovalRecheck{
		logger:    logger.With(attr.SlogComponent("mcp-approval-recheck")),
		db:        db,
		assembler: assembler,
		features:  features,
		audit:     auditLogger,
	}
}

// ListPage returns one page of approved requests with their latest decision
// snapshots, ordered by id for keyset pagination.
func (m *McpApprovalRecheck) ListPage(ctx context.Context, args McpApprovalRecheckPageArgs) ([]McpApprovalRecheckTarget, error) {
	rows, err := approvalrepo.New(m.db).ListApprovedRequestsForRecheck(ctx, approvalrepo.ListApprovedRequestsForRecheckParams{
		AfterID:  args.AfterID,
		PageSize: args.PageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("list approved requests for recheck: %w", err)
	}

	targets := make([]McpApprovalRecheckTarget, 0, len(rows))
	for _, row := range rows {
		var collectedAt *time.Time
		if row.EvidenceCollectedAt.Valid {
			at := row.EvidenceCollectedAt.Time
			collectedAt = &at
		}
		targets = append(targets, McpApprovalRecheckTarget{
			ID:                        row.ID,
			ProjectID:                 row.ProjectID,
			OrganizationID:            row.OrganizationID,
			TargetRaw:                 row.TargetRaw,
			EvidenceCollectedAt:       collectedAt,
			NotifiedChangeFingerprint: conv.PtrValOr(conv.FromPGText[string](row.NotifiedChangeFingerprint), ""),
			DecisionEvidenceSnapshot:  row.DecisionEvidenceSnapshot,
			DecisionEvidenceVersion:   row.DecisionEvidenceVersion,
		})
	}

	return targets, nil
}

// Recheck re-gathers one approved request's evidence, stores it, and flags
// drift from the latest decision's snapshot. Detection compares only the
// permission-relevant slice — scopes, authority mode, demanded credentials,
// advisories — and announces each distinct drift once through the audit
// feed's webhook channel.
func (m *McpApprovalRecheck) Recheck(ctx context.Context, target McpApprovalRecheckTarget) error {
	stopHeartbeat := startActivityHeartbeat(ctx)
	defer stopHeartbeat()

	// The feature gate is per-organization state that can change between the
	// scan and this activity, so it is checked here rather than in the list.
	enabled, err := m.features.IsFeatureEnabled(ctx, target.OrganizationID, productfeatures.FeatureMCPApproval)
	if err != nil {
		return fmt.Errorf("check mcp approval feature: %w", err)
	}
	if !enabled {
		return nil
	}

	document, err := m.assembler.Assemble(ctx, target.ProjectID, identity.Resolve(target.TargetRaw))
	if err != nil {
		return fmt.Errorf("assemble evidence for recheck: %w", err)
	}

	current, err := evidence.DecodeDocument(document, evidence.Version)
	if err != nil {
		return fmt.Errorf("decode recheck gather: %w", err)
	}

	queries := approvalrepo.New(m.db)

	// The store is a compare-and-set against the gather this sweep read at
	// scan time: a manual refresh landing in between wins, and this gather is
	// discarded rather than clobbering the newer document. Drift detection
	// still runs — the comparison below uses this gather either way, and a
	// detection from a discarded gather is still a detection.
	previousCollectedAt := pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite}
	if target.EvidenceCollectedAt != nil {
		previousCollectedAt = pgtype.Timestamptz{Time: *target.EvidenceCollectedAt, Valid: true, InfinityModifier: pgtype.Finite}
	}
	if _, err := queries.SetApprovalRequestEvidenceIfUnchanged(ctx, approvalrepo.SetApprovalRequestEvidenceIfUnchangedParams{
		CurrentEvidence:     document,
		EvidenceVersion:     evidence.Version,
		ID:                  target.ID,
		ProjectID:           target.ProjectID,
		PreviousCollectedAt: previousCollectedAt,
	}); err != nil {
		return fmt.Errorf("store recheck gather: %w", err)
	}

	snapshot, err := evidence.DecodeDocument(target.DecisionEvidenceSnapshot, int(target.DecisionEvidenceVersion))
	if err != nil {
		// An undecodable snapshot means the decision predates a shape change
		// this package cannot read any more; there is nothing to compare
		// against and nothing to flag.
		m.logger.WarnContext(ctx, "skipping recheck comparison for undecodable decision snapshot",
			attr.SlogError(err))
		return nil
	}

	diff := evidencediff.Compare(snapshot, current)
	if !diff.Changed {
		return nil
	}

	fingerprint := evidencediff.Fingerprint(current)
	if fingerprint == target.NotifiedChangeFingerprint {
		// This exact drift was already announced; the flag is still set and
		// stays set until a new decision clears it.
		return nil
	}

	diffSummary, err := json.Marshal(diff)
	if err != nil {
		return fmt.Errorf("encode evidence diff: %w", err)
	}

	// Flag and announcement commit together: a webhook event about a flag
	// that was never set, or a set flag whose announcement was lost, would
	// each break the announce-once contract in a different direction.
	dbtx, err := m.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin evidence change transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txQueries := queries.WithTx(dbtx)
	if err := txQueries.MarkApprovalRequestEvidenceChanged(ctx, approvalrepo.MarkApprovalRequestEvidenceChangedParams{
		ID:          target.ID,
		ProjectID:   target.ProjectID,
		Fingerprint: conv.ToPGText(fingerprint),
	}); err != nil {
		return fmt.Errorf("flag evidence change: %w", err)
	}

	if err := m.audit.LogMCPApprovalRequestEvidenceChanged(ctx, dbtx, audit.LogMCPApprovalRequestEvidenceChangedEvent{
		OrganizationID: target.OrganizationID,
		ProjectID:      target.ProjectID,
		RequestURN:     urn.NewMCPApprovalRequest(target.ID),
		TargetRaw:      target.TargetRaw,
		DiffSummary:    diffSummary,
	}); err != nil {
		return fmt.Errorf("audit evidence change: %w", err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return fmt.Errorf("commit evidence change: %w", err)
	}

	m.logger.InfoContext(ctx, "approved mcp server evidence changed",
		attr.SlogOrganizationID(target.OrganizationID),
		attr.SlogProjectID(target.ProjectID.String()),
	)

	return nil
}
