package risk_analysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func (a *AnalyzeBatch) buildRows(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage, batchFindings [][]scanners.Finding) ([]repo.InsertRiskResultsParams, int) {
	var rows []repo.InsertRiskResultsParams
	findingsCount := 0

	for i, msg := range messages {
		findings := batchFindings[i]
		realFindings := findings[:0:0]
		for _, f := range findings {
			if f.DeadLetterReason != "" {
				resultID, _ := uuid.NewV7()
				rows = append(rows, deadLetterRow(resultID, args, msg, f))
				continue
			}
			realFindings = append(realFindings, f)
		}

		if len(realFindings) == 0 {
			resultID, _ := uuid.NewV7()
			rows = append(rows, emptyResultRow(resultID, args, msg))
			continue
		}

		for _, grp := range groupFindings(realFindings) {
			f := grp.primary
			findingsCount++
			a.metrics.RecordFindingConfidence(ctx, args.OrganizationID, f.RuleID, f.Confidence)
			resultID, _ := uuid.NewV7()
			spansJSON, err := json.Marshal(grp.spans)
			if err != nil {
				spansJSON = nil
			}
			rows = append(rows, repo.InsertRiskResultsParams{
				ID:                resultID,
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

	if len(args.MessageIDs) > 0 {
		if err := txRepo.DeleteRiskResultsForMessages(ctx, repo.DeleteRiskResultsForMessagesParams{
			RiskPolicyID: args.RiskPolicyID,
			ProjectID:    args.ProjectID,
			MessageIds:   args.MessageIDs,
		}); err != nil {
			writeSpan.SetStatus(codes.Error, err.Error())
			return false, fmt.Errorf("delete old results: %w", err)
		}
	}
	if len(args.ContentPartIDs) > 0 {
		if err := txRepo.DeleteRiskResultsForContentParts(ctx, repo.DeleteRiskResultsForContentPartsParams{
			RiskPolicyID:   args.RiskPolicyID,
			ProjectID:      args.ProjectID,
			ContentPartIds: args.ContentPartIDs,
		}); err != nil {
			writeSpan.SetStatus(codes.Error, err.Error())
			return false, fmt.Errorf("delete old content part results: %w", err)
		}
	}

	if len(rows) > 0 {
		if _, err := txRepo.InsertRiskResults(ctx, rows); err != nil {
			writeSpan.SetStatus(codes.Error, err.Error())
			return false, fmt.Errorf("insert risk results: %w", err)
		}
	}

	payloads := findingCreatedPayloads(rows, time.Now())
	if len(payloads) > 0 {
		if _, err := outbox.PublishWebhookEvents(ctx, tx, args.OrganizationID, events.RiskFindingCreatedV1, payloads); err != nil {
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

func findingCreatedPayloads(rows []repo.InsertRiskResultsParams, now time.Time) []events.RiskFindingCreatedPayloadV1 {
	var payloads []events.RiskFindingCreatedPayloadV1
	for _, row := range rows {
		if !row.Found || !row.RuleID.Valid {
			continue
		}
		// Content-part findings emit no webhook yet. The v1 payload pins
		// chat_message_id to a non-null uuid, and widening it would break the
		// published schema for existing subscribers. Carrying the part anchor
		// needs a new event version (AIS-XXX).
		if !row.ChatMessageID.Valid {
			continue
		}
		payloads = append(payloads, events.RiskFindingCreatedPayloadV1{
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
		})
	}
	return payloads
}

func emptyResultRow(id uuid.UUID, args AnalyzeBatchArgs, msg batchMessage) repo.InsertRiskResultsParams {
	return repo.InsertRiskResultsParams{
		ID:                id,
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

func deadLetterRow(id uuid.UUID, args AnalyzeBatchArgs, msg batchMessage, f scanners.Finding) repo.InsertRiskResultsParams {
	return repo.InsertRiskResultsParams{
		ID:                id,
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
