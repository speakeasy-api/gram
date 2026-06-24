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
)

func dedup(findings []Finding) []Finding {
	if len(findings) <= 1 {
		return findings
	}
	var out []Finding
	for _, f := range findings {
		if overlapsAny(out, f) {
			continue
		}
		out = append(out, f)
	}
	return out
}

func overlapsAny(kept []Finding, candidate Finding) bool {
	for _, k := range kept {
		if k.StartPos < candidate.EndPos && candidate.StartPos < k.EndPos {
			return true
		}
	}
	return false
}

func (a *AnalyzeBatch) buildRows(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage, batchFindings [][]Finding) ([]repo.InsertRiskResultsParams, int) {
	var rows []repo.InsertRiskResultsParams
	findingsCount := 0

	for i, msg := range messages {
		findings := batchFindings[i]
		realFindings := findings[:0:0]
		for _, f := range findings {
			if f.DeadLetterReason != "" {
				resultID, _ := uuid.NewV7()
				rows = append(rows, deadLetterRow(resultID, args, msg.ID, f))
				continue
			}
			realFindings = append(realFindings, f)
		}

		if len(realFindings) == 0 {
			resultID, _ := uuid.NewV7()
			rows = append(rows, emptyResultRow(resultID, args, msg.ID))
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
				ChatMessageID:     msg.ID,
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
	primary Finding
	spans   []FindingSpan
}

func groupFindings(findings []Finding) []findingGroup {
	var order []string
	groups := map[string]*findingGroup{}
	uniq := 0
	for _, f := range findings {
		var key string
		if f.spanGroupKey != "" {
			key = f.Source + "\x00" + f.RuleID + "\x00" + f.spanGroupKey
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
			Field:    f.field,
			Path:     f.path,
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

func (a *AnalyzeBatch) writeResults(ctx context.Context, args AnalyzeBatchArgs, rows []repo.InsertRiskResultsParams) error {
	ctx, writeSpan := a.tracer.Start(ctx, "risk.writeResults")
	defer writeSpan.End()

	rows = a.guardRuleIDs(ctx, rows)

	tx, err := a.db.Begin(ctx)
	if err != nil {
		writeSpan.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("begin transaction: %w", err)
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
			return nil
		}
		writeSpan.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("re-check risk policy before writing results: %w", err)
	}

	if err := txRepo.DeleteRiskResultsForMessages(ctx, repo.DeleteRiskResultsForMessagesParams{
		RiskPolicyID: args.RiskPolicyID,
		ProjectID:    args.ProjectID,
		MessageIds:   args.MessageIDs,
	}); err != nil {
		writeSpan.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("delete old results: %w", err)
	}

	if len(rows) > 0 {
		if _, err := txRepo.InsertRiskResults(ctx, rows); err != nil {
			writeSpan.SetStatus(codes.Error, err.Error())
			return fmt.Errorf("insert risk results: %w", err)
		}
	}

	payloads := findingCreatedPayloads(rows, time.Now())
	if len(payloads) > 0 {
		if _, err := outbox.AppendBatch(ctx, tx, args.OrganizationID, events.RiskFindingCreatedV1, payloads); err != nil {
			writeSpan.SetStatus(codes.Error, err.Error())
			return fmt.Errorf("append risk findings to outbox: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		writeSpan.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("commit results: %w", err)
	}
	return nil
}

func findingCreatedPayloads(rows []repo.InsertRiskResultsParams, now time.Time) []events.RiskFindingCreatedPayloadV1 {
	var payloads []events.RiskFindingCreatedPayloadV1
	for _, row := range rows {
		if !row.Found || !row.RuleID.Valid {
			continue
		}
		payloads = append(payloads, events.RiskFindingCreatedPayloadV1{
			ID:                row.ID,
			ProjectID:         row.ProjectID,
			OrganizationID:    row.OrganizationID,
			RiskPolicyID:      row.RiskPolicyID,
			RiskPolicyVersion: row.RiskPolicyVersion,
			ChatMessageID:     row.ChatMessageID,
			RuleID:            row.RuleID.String,
			Description:       row.Description.String,
			Confidence:        row.Confidence.Float64,
			Tags:              row.Tags,
			CreatedAt:         now,
		})
	}
	return payloads
}

func (a *AnalyzeBatch) guardRuleIDs(_ context.Context, rows []repo.InsertRiskResultsParams) []repo.InsertRiskResultsParams {
	if !enforceRuleIDFormat {
		return rows
	}
	for _, row := range rows {
		if !row.RuleID.Valid || row.RuleID.String == "" {
			continue
		}
		if err := ValidateRuleID(row.RuleID.String); err != nil {
			panic(fmt.Sprintf("risk_analysis.writeResults: malformed rule_id %q from source %q: %v", row.RuleID.String, row.Source, err))
		}
	}
	return rows
}

func emptyResultRow(id uuid.UUID, args AnalyzeBatchArgs, messageID uuid.UUID) repo.InsertRiskResultsParams {
	return repo.InsertRiskResultsParams{
		ID:                id,
		ProjectID:         args.ProjectID,
		OrganizationID:    args.OrganizationID,
		RiskPolicyID:      args.RiskPolicyID,
		RiskPolicyVersion: args.PolicyVersion,
		ChatMessageID:     messageID,
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

func deadLetterRow(id uuid.UUID, args AnalyzeBatchArgs, messageID uuid.UUID, f Finding) repo.InsertRiskResultsParams {
	return repo.InsertRiskResultsParams{
		ID:                id,
		ProjectID:         args.ProjectID,
		OrganizationID:    args.OrganizationID,
		RiskPolicyID:      args.RiskPolicyID,
		RiskPolicyVersion: args.PolicyVersion,
		ChatMessageID:     messageID,
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
