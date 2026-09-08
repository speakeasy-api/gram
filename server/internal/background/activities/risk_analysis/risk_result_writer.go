package risk_analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

func dedup(findings []scanners.Finding) []scanners.Finding {
	if len(findings) <= 1 {
		return findings
	}
	var out []scanners.Finding
	for _, f := range findings {
		if overlapsAny(out, f) {
			continue
		}
		out = append(out, f)
	}
	return out
}

func overlapsAny(kept []scanners.Finding, candidate scanners.Finding) bool {
	for _, k := range kept {
		if k.StartPos < candidate.EndPos && candidate.StartPos < k.EndPos {
			return true
		}
	}
	return false
}

// resultRowIdentity is the canonical identity of one stored risk_results row.
// Every field is recoverable from stored (and delete-RETURNING) columns, so
// the deterministic id derived from it can be recomputed for rows written by
// any binary — including legacy rows inserted under random ids before ids
// became deterministic. Content fields (match, description) are part of the
// identity, so rows differing only in content get distinct ids and a redrive
// whose scanner output changed re-announces instead of silently swapping
// content under a known id.
type resultRowIdentity struct {
	projectID        uuid.UUID
	riskPolicyID     uuid.UUID
	policyVersion    int64
	chatMessageID    uuid.NullUUID
	contentPartID    uuid.NullUUID
	found            bool
	source           string
	ruleID           string
	description      string
	match            string
	startPos         int32
	endPos           int32
	deadLetterReason string
}

// key renders the identity as the canonical hash input. The row kind is
// derived the way buildRows constructs rows: a dead-letter reason marks a
// sentinel, found marks a finding, anything else is the per-message empty row.
func (ri resultRowIdentity) key() string {
	kind := "empty"
	switch {
	case ri.deadLetterReason != "":
		kind = "deadletter"
	case ri.found:
		kind = "finding"
	}
	messageID := ""
	if ri.chatMessageID.Valid {
		messageID = ri.chatMessageID.UUID.String()
	}
	partID := ""
	if ri.contentPartID.Valid {
		partID = ri.contentPartID.UUID.String()
	}
	return strings.Join([]string{
		ri.projectID.String(),
		ri.riskPolicyID.String(),
		strconv.FormatInt(ri.policyVersion, 10),
		messageID,
		partID,
		kind,
		ri.source,
		ri.ruleID,
		ri.description,
		ri.match,
		strconv.Itoa(int(ri.startPos)),
		strconv.Itoa(int(ri.endPos)),
		ri.deadLetterReason,
	}, "\x00")
}

// insertRowIdentity extracts the identity of a row about to be written.
func insertRowIdentity(row repo.InsertRiskResultsParams) resultRowIdentity {
	return resultRowIdentity{
		projectID:        row.ProjectID,
		riskPolicyID:     row.RiskPolicyID,
		policyVersion:    row.RiskPolicyVersion,
		chatMessageID:    row.ChatMessageID,
		contentPartID:    row.ChatContentPartID,
		found:            row.Found,
		source:           row.Source,
		ruleID:           row.RuleID.String,
		description:      row.Description.String,
		match:            row.Match.String,
		startPos:         row.StartPos.Int32,
		endPos:           row.EndPos.Int32,
		deadLetterReason: row.DeadLetterReason.String,
	}
}

// deletedRowIdentity rebuilds the identity of a replaced row from the
// delete's RETURNING columns, ignoring its stored id: recomputing the
// deterministic id from identity puts legacy random-id rows on the same
// footing as rows written after ids became deterministic.
func deletedRowIdentity(projectID, riskPolicyID uuid.UUID, row repo.DeleteRiskResultsForUnitsRow) resultRowIdentity {
	return resultRowIdentity{
		projectID:        projectID,
		riskPolicyID:     riskPolicyID,
		policyVersion:    row.RiskPolicyVersion,
		chatMessageID:    row.ChatMessageID,
		contentPartID:    row.ChatContentPartID,
		found:            row.Found,
		source:           row.Source,
		ruleID:           row.RuleID.String,
		description:      row.Description.String,
		match:            row.Match.String,
		startPos:         row.StartPos.Int32,
		endPos:           row.EndPos.Int32,
		deadLetterReason: row.DeadLetterReason.String,
	}
}

// resultRowIDs mints risk_results row ids: uuid5 over the row's canonical
// identity key plus a per-key ordinal disambiguating byte-identical
// duplicates within one population. Determinism is a delivery-guarantee
// requirement, the same one batchScanRequestID meets for scan requests: the
// webhook outbox event id derives from the row id, so a redriven activity
// must rebuild identical ids or every retry would re-announce every finding
// under fresh event ids.
type resultRowIDs struct {
	seen map[string]int
}

func newResultRowIDs() *resultRowIDs {
	return &resultRowIDs{seen: map[string]int{}}
}

func (r *resultRowIDs) mint(key string) uuid.UUID {
	ordinal := r.seen[key]
	r.seen[key]++
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("gram:risk:result:"+key+"\x00"+strconv.Itoa(ordinal)))
}

// priorRowIndex recomputes the deterministic id of every replaced row and
// maps it to the row's dismissal state. Rows are sorted before ordinal
// assignment — identity key, then dismissed-first, then stored id — because
// the DELETE returns rows in unspecified order: within a group of
// byte-identical rows, buildRows hands ordinals 0..n-1 to whatever the
// scanner reproduces, so pinning dismissals to the lowest ordinals is what
// makes a redrive land them on rows that exist, deterministically, instead of
// scattering them by RETURNING order onto ordinals the scanner may not have
// reproduced.
func priorRowIndex(projectID, riskPolicyID uuid.UUID, deleted []repo.DeleteRiskResultsForUnitsRow) map[uuid.UUID]repo.DeleteRiskResultsForUnitsRow {
	type keyedRow struct {
		key string
		row repo.DeleteRiskResultsForUnitsRow
	}
	keyed := make([]keyedRow, 0, len(deleted))
	for _, d := range deleted {
		keyed = append(keyed, keyedRow{key: deletedRowIdentity(projectID, riskPolicyID, d).key(), row: d})
	}
	slices.SortFunc(keyed, func(a, b keyedRow) int {
		if c := strings.Compare(a.key, b.key); c != 0 {
			return c
		}
		if a.row.FalsePositiveAt.Valid != b.row.FalsePositiveAt.Valid {
			if a.row.FalsePositiveAt.Valid {
				return -1
			}
			return 1
		}
		return bytes.Compare(a.row.ID[:], b.row.ID[:])
	})

	ids := newResultRowIDs()
	index := make(map[uuid.UUID]repo.DeleteRiskResultsForUnitsRow, len(keyed))
	for _, k := range keyed {
		index[ids.mint(k.key)] = k.row
	}
	return index
}

func (a *AnalyzeBatch) buildRows(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage, batchFindings [][]scanners.Finding) ([]repo.InsertRiskResultsParams, int) {
	var rows []repo.InsertRiskResultsParams
	findingsCount := 0
	rowIDs := newResultRowIDs()
	appendRow := func(row repo.InsertRiskResultsParams) {
		row.ID = rowIDs.mint(insertRowIdentity(row).key())
		rows = append(rows, row)
	}

	for i, msg := range messages {
		findings := batchFindings[i]
		realFindings := findings[:0:0]
		for _, f := range findings {
			if f.DeadLetterReason != "" {
				appendRow(deadLetterRow(args, msg, f))
				continue
			}
			realFindings = append(realFindings, f)
		}

		if len(realFindings) == 0 {
			appendRow(emptyResultRow(args, msg))
			continue
		}

		for _, grp := range groupFindings(realFindings) {
			f := grp.primary
			findingsCount++
			a.metrics.RecordFindingConfidence(ctx, args.OrganizationID, f.RuleID, f.Confidence)
			spansJSON, err := json.Marshal(grp.spans)
			if err != nil {
				spansJSON = nil
			}
			appendRow(repo.InsertRiskResultsParams{
				ID:                uuid.Nil,
				ProjectID:         args.ProjectID,
				OrganizationID:    args.OrganizationID,
				RiskPolicyID:      args.RiskPolicyID,
				RiskPolicyVersion: args.PolicyVersion,
				ChatMessageID:     msg.chatMessageID(),
				ChatContentPartID: msg.chatContentPartID(),
				Source:            f.Source,
				Found:             true,
				RuleID:            pgtype.Text{String: f.RuleID, Valid: true},
				Description:       pgtype.Text{String: f.Description, Valid: true},
				Match:             pgtype.Text{String: f.Match, Valid: true},
				StartPos:          pgtype.Int4{Int32: conv.SafeInt32(f.StartPos), Valid: true},
				EndPos:            pgtype.Int4{Int32: conv.SafeInt32(f.EndPos), Valid: true},
				Confidence:        pgtype.Float8{Float64: f.Confidence, Valid: true},
				Tags:              f.Tags,
				Spans:             spansJSON,
				DeadLetterReason:  pgtype.Text{String: "", Valid: false},
			})
		}
	}
	return rows, findingsCount
}

type findingGroup struct {
	primary scanners.Finding
	spans   []FindingSpan
}

func groupFindings(findings []scanners.Finding) []findingGroup {
	var order []string
	groups := map[string]*findingGroup{}
	uniq := 0
	for _, f := range findings {
		var key string
		if f.SpanGroupKey != "" {
			key = f.Source + "\x00" + f.RuleID + "\x00" + f.SpanGroupKey
		} else {
			key = fmt.Sprintf("u%d", uniq)
			uniq++
		}
		g := groups[key]
		if g == nil {
			g = &findingGroup{primary: f, spans: []FindingSpan{}}
			groups[key] = g
			order = append(order, key)
		}
		g.spans = append(g.spans, FindingSpan{
			Match:    f.Match,
			Field:    f.Field,
			Path:     f.Path,
			StartPos: f.StartPos,
			EndPos:   f.EndPos,
		})
	}
	out := make([]findingGroup, 0, len(order))
	for _, k := range order {
		out = append(out, *groups[k])
	}
	return out
}

// writeResults commits the batch's rows. The bool reports whether a commit
// actually happened: false when the policy was deleted mid-analysis and the
// results were dropped, so callers gating side effects (e.g. the batch-only
// finding publish) on a durable write can tell the two nil-error outcomes
// apart.
func (a *AnalyzeBatch) writeResults(ctx context.Context, args AnalyzeBatchArgs, rows []repo.InsertRiskResultsParams) (bool, error) {
	ctx, writeSpan := a.tracer.Start(ctx, "risk.writeResults")
	defer writeSpan.End()

	tx, err := a.db.Begin(ctx)
	if err != nil {
		writeSpan.SetStatus(codes.Error, err.Error())
		return false, fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	txRepo := repo.New(a.db).WithTx(tx)

	if _, err := txRepo.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        args.RiskPolicyID,
		ProjectID: args.ProjectID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeSpan.SetAttributes(attribute.Bool("risk.policy_deleted", true))
			a.logger.InfoContext(ctx, "risk policy deleted mid-analysis, dropping results", attr.SlogRiskPolicyID(args.RiskPolicyID.String()))
			return false, nil
		}
		writeSpan.SetStatus(codes.Error, err.Error())
		return false, fmt.Errorf("re-check risk policy before writing results: %w", err)
	}

	// prior holds each replaced row keyed by its RECOMPUTED deterministic id,
	// not its stored one: legacy rows predate deterministic ids, so only the
	// identity-derived key lines them up with the rows about to be reinserted.
	// A key present in prior means an earlier committed attempt already
	// announced that finding (skip its webhook event) and a manual dismissal
	// on it must survive the replacement (re-stamped below). A policy version
	// bump changes every identity, so bumped re-analyses re-announce and drop
	// old dismissals by design.
	prior := map[uuid.UUID]repo.DeleteRiskResultsForUnitsRow{}
	if len(args.MessageIDs) > 0 || len(args.ContentPartIDs) > 0 {
		deleted, err := txRepo.DeleteRiskResultsForUnits(ctx, repo.DeleteRiskResultsForUnitsParams{
			RiskPolicyID:   args.RiskPolicyID,
			ProjectID:      args.ProjectID,
			MessageIds:     args.MessageIDs,
			ContentPartIds: args.ContentPartIDs,
		})
		if err != nil {
			writeSpan.SetStatus(codes.Error, err.Error())
			return false, fmt.Errorf("delete old results: %w", err)
		}
		prior = priorRowIndex(args.ProjectID, args.RiskPolicyID, deleted)
	}

	if len(rows) > 0 {
		if _, err := txRepo.InsertRiskResults(ctx, rows); err != nil {
			writeSpan.SetStatus(codes.Error, err.Error())
			return false, fmt.Errorf("insert risk results: %w", err)
		}
	}

	restore := repo.RestoreRiskResultFalsePositiveStateParams{
		ProjectID:            args.ProjectID,
		Ids:                  nil,
		FalsePositiveAts:     nil,
		FalsePositiveReasons: nil,
	}
	for _, row := range rows {
		if d, ok := prior[row.ID]; ok && d.FalsePositiveAt.Valid {
			restore.Ids = append(restore.Ids, row.ID)
			restore.FalsePositiveAts = append(restore.FalsePositiveAts, d.FalsePositiveAt)
			restore.FalsePositiveReasons = append(restore.FalsePositiveReasons, d.FalsePositiveReason.String)
		}
	}
	if len(restore.Ids) > 0 {
		if err := txRepo.RestoreRiskResultFalsePositiveState(ctx, restore); err != nil {
			writeSpan.SetStatus(codes.Error, err.Error())
			return false, fmt.Errorf("restore false positive state: %w", err)
		}
	}

	// Only findings this transaction writes for the first time are announced:
	// an id present in prior was already announced by the committed transaction
	// that first wrote it (delete, insert and outbox share one transaction).
	// Event ids stay pinned to the deterministic row ids as the backstop for
	// the residual race — a zombie attempt and its retry can both commit "new"
	// rows, and delivery then dedups on the Svix idempotency key.
	webhookEvents := findingCreatedEvents(rows, prior, time.Now())
	if len(webhookEvents) > 0 {
		if _, err := outbox.PublishIdentifiedWebhookEvents(ctx, tx, args.OrganizationID, events.RiskFindingCreatedV1, webhookEvents); err != nil {
			writeSpan.SetStatus(codes.Error, err.Error())
			return false, fmt.Errorf("append risk findings to outbox: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		writeSpan.SetStatus(codes.Error, err.Error())
		return false, fmt.Errorf("commit results: %w", err)
	}
	return true, nil
}

func findingCreatedEvents(rows []repo.InsertRiskResultsParams, prior map[uuid.UUID]repo.DeleteRiskResultsForUnitsRow, now time.Time) []outbox.IdentifiedWebhookEvent[events.RiskFindingCreatedPayloadV1] {
	var evs []outbox.IdentifiedWebhookEvent[events.RiskFindingCreatedPayloadV1]
	for _, row := range rows {
		if !row.Found || !row.RuleID.Valid {
			continue
		}
		// Replaced-in-place: the transaction that first wrote this id already
		// emitted its created event.
		if _, existed := prior[row.ID]; existed {
			continue
		}
		// Content-part findings emit no webhook yet. The v1 payload pins
		// chat_message_id to a non-null uuid, and widening it would break the
		// published schema for existing subscribers. Carrying the part anchor
		// needs a new event version (AIS-XXX).
		if !row.ChatMessageID.Valid {
			continue
		}
		evs = append(evs, outbox.IdentifiedWebhookEvent[events.RiskFindingCreatedPayloadV1]{
			ID: findingWebhookEventID(row.ID),
			Payload: events.RiskFindingCreatedPayloadV1{
				ID:                row.ID,
				ProjectID:         row.ProjectID,
				OrganizationID:    row.OrganizationID,
				RiskPolicyID:      row.RiskPolicyID,
				RiskPolicyVersion: row.RiskPolicyVersion,
				ChatMessageID:     row.ChatMessageID.UUID,
				RuleID:            row.RuleID.String,
				Description:       row.Description.String,
				Confidence:        row.Confidence.Float64,
				Tags:              row.Tags,
				CreatedAt:         now,
			},
		})
	}
	return evs
}

// findingWebhookEventID pins the created-event's id to the finding's row id,
// which is itself deterministic over the batch identity: every redriven write
// of the same row re-emits the same event id — the Svix idempotency key — so
// delivery dedups instead of double-sending. Hashed into its own namespace so
// the event id never collides with the row id it derives from.
func findingWebhookEventID(rowID uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("gram:risk:finding-created-event:"+rowID.String()))
}

func emptyResultRow(args AnalyzeBatchArgs, msg batchMessage) repo.InsertRiskResultsParams {
	return repo.InsertRiskResultsParams{
		ID:                uuid.Nil,
		ProjectID:         args.ProjectID,
		OrganizationID:    args.OrganizationID,
		RiskPolicyID:      args.RiskPolicyID,
		RiskPolicyVersion: args.PolicyVersion,
		ChatMessageID:     msg.chatMessageID(),
		ChatContentPartID: msg.chatContentPartID(),
		Source:            SourceNone,
		Found:             false,
		RuleID:            pgtype.Text{String: "", Valid: false},
		Description:       pgtype.Text{String: "", Valid: false},
		Match:             pgtype.Text{String: "", Valid: false},
		StartPos:          pgtype.Int4{Int32: 0, Valid: false},
		EndPos:            pgtype.Int4{Int32: 0, Valid: false},
		Confidence:        pgtype.Float8{Float64: 0, Valid: false},
		Tags:              []string{},
		Spans:             nil,
		DeadLetterReason:  pgtype.Text{String: "", Valid: false},
	}
}

func deadLetterRow(args AnalyzeBatchArgs, msg batchMessage, f scanners.Finding) repo.InsertRiskResultsParams {
	return repo.InsertRiskResultsParams{
		ID:                uuid.Nil,
		ProjectID:         args.ProjectID,
		OrganizationID:    args.OrganizationID,
		RiskPolicyID:      args.RiskPolicyID,
		RiskPolicyVersion: args.PolicyVersion,
		ChatMessageID:     msg.chatMessageID(),
		ChatContentPartID: msg.chatContentPartID(),
		Source:            f.Source,
		Found:             false,
		RuleID:            pgtype.Text{String: f.RuleID, Valid: f.RuleID != ""},
		Description:       pgtype.Text{String: f.Description, Valid: f.Description != ""},
		Match:             pgtype.Text{String: "", Valid: false},
		StartPos:          pgtype.Int4{Int32: 0, Valid: false},
		EndPos:            pgtype.Int4{Int32: 0, Valid: false},
		Confidence:        pgtype.Float8{Float64: 0, Valid: false},
		Tags:              []string{},
		Spans:             nil,
		DeadLetterReason:  pgtype.Text{String: f.DeadLetterReason, Valid: true},
	}
}
