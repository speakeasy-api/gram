package activities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

// McpApprovalRecheckTarget names one approved request for the sweep to
// recheck. Deliberately only identity: both sides of the comparison are
// loaded when the recheck runs. Carrying evidence documents here instead
// would put a page of them through the workflow twice — once as the scan's
// result and once as each activity's input, both recorded in history — and
// would freeze the decision snapshot at scan time, minutes to hours before
// the recheck that reads it.
type McpApprovalRecheckTarget struct {
	ID             uuid.UUID
	ProjectID      uuid.UUID
	OrganizationID string
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

// ListPage returns one page of approved requests that have a decision to
// compare against, ordered by id for keyset pagination.
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
		targets = append(targets, McpApprovalRecheckTarget{
			ID:             row.ID,
			ProjectID:      row.ProjectID,
			OrganizationID: row.OrganizationID,
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

	queries := approvalrepo.New(m.db)

	// Both comparison sides are read here, not at scan time: a request
	// decided or denied since the scan is resolved by this read rather than
	// rechecked against what it used to be.
	request, err := queries.GetApprovalRequestForRecheck(ctx, approvalrepo.GetApprovalRequestForRecheckParams{
		ID:        target.ID,
		ProjectID: target.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// No longer approved, or its decisions are gone. Nothing to compare.
		return nil
	case err != nil:
		return fmt.Errorf("load approval request for recheck: %w", err)
	}

	document, err := m.assembler.Assemble(ctx, target.ProjectID, identity.Resolve(request.TargetRaw))
	if err != nil {
		return fmt.Errorf("assemble evidence for recheck: %w", err)
	}

	current, err := evidence.DecodeDocument(document, evidence.Version)
	if err != nil {
		return fmt.Errorf("decode recheck gather: %w", err)
	}

	storedDocument, storedErr := evidence.DecodeDocument(request.CurrentEvidence, int(request.EvidenceVersion))
	// A gather that failed on every remote source has learned nothing, and
	// storing it would replace a document that did better with a page of
	// failures — the same refusal the manual refresh path makes. Comparing it
	// is equally pointless: every section it could speak to is gapped.
	if current.GappedOnAllRemoteSources() && (storedErr != nil || !storedDocument.GappedOnAllRemoteSources()) {
		m.logger.WarnContext(ctx, "skipping recheck whose gather failed on every remote source",
			attr.SlogProjectID(target.ProjectID.String()),
		)
		return nil
	}

	// The store is a compare-and-set against the gather this activity just
	// read: a manual refresh landing in between wins, and this gather is
	// discarded rather than clobbering the newer document.
	stored, err := queries.SetApprovalRequestEvidenceIfUnchanged(ctx, approvalrepo.SetApprovalRequestEvidenceIfUnchangedParams{
		CurrentEvidence:     document,
		EvidenceVersion:     evidence.Version,
		ID:                  target.ID,
		ProjectID:           target.ProjectID,
		PreviousCollectedAt: request.EvidenceCollectedAt,
	})
	if err != nil {
		return fmt.Errorf("store recheck gather: %w", err)
	}
	if stored == 0 {
		// A concurrent refresh won, so this gather is not what the request
		// holds. Flagging on it would announce a drift the page cannot show —
		// the banner reads the stored document — and would stamp the winner's
		// evidence with a fingerprint describing a different gather. The
		// winning document is compared on the next sweep, and the read-path
		// diff already shows any drift it carries.
		return nil
	}

	snapshot, err := evidence.DecodeDocument(request.DecisionEvidenceSnapshot, int(request.DecisionEvidenceVersion))
	if err != nil {
		// An undecodable snapshot means the decision predates a shape change
		// this package cannot read any more; there is nothing to compare
		// against and nothing to flag.
		m.logger.WarnContext(ctx, "skipping recheck comparison for undecodable decision snapshot",
			attr.SlogProjectID(target.ProjectID.String()),
			attr.SlogError(err),
		)
		return nil
	}

	diff := evidencediff.Compare(snapshot, current)
	if !diff.Changed {
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

	// The write is the arbiter of whether this drift is news: it declines to
	// touch a row whose fingerprint already matches, whose request is no
	// longer approved, or that has been decided since the snapshot compared
	// above. Announcing only on a written row is what makes an activity retry
	// a no-op instead of a second webhook.
	flagged, err := txQueries.MarkApprovalRequestEvidenceChanged(ctx, approvalrepo.MarkApprovalRequestEvidenceChangedParams{
		ID:                 target.ID,
		ProjectID:          target.ProjectID,
		Fingerprint:        conv.ToPGText(evidencediff.Fingerprint(diff)),
		ComparedDecisionAt: request.DecisionDecidedAt,
		ComparedDecisionID: request.DecisionID,
	})
	if err != nil {
		return fmt.Errorf("flag evidence change: %w", err)
	}
	if flagged == 0 {
		return nil
	}

	if err := m.audit.LogMCPApprovalRequestEvidenceChanged(ctx, dbtx, audit.LogMCPApprovalRequestEvidenceChangedEvent{
		OrganizationID: target.OrganizationID,
		ProjectID:      target.ProjectID,
		// No person acted: the sweep observed.
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, "system"),
		ActorDisplayName: conv.PtrEmpty("Evidence recheck"),
		RequestURN:       urn.NewMCPApprovalRequest(target.ID),
		TargetRaw:        request.TargetRaw,
		DiffSummary:      diffSummary,
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
