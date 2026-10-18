package toolcall

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/clidestructive"
	"github.com/speakeasy-api/gram/server/internal/scanners/destructivetool"
	"github.com/speakeasy-api/gram/server/internal/scanners/shadowmcpscan"
)

type shadowScanner interface {
	Scan(context.Context, string, uuid.UUID, uuid.UUID, []shadowmcpscan.Message) [][]scanners.Finding
}

type destructiveScanner interface {
	Scan(context.Context, string, []destructivetool.ToolCall) []scanners.Finding
}

type cliScanner interface {
	Scan(context.Context, []clidestructive.ToolCall) scanners.Result
}

type Handler struct {
	logger       *slog.Logger
	shadow       shadowScanner
	destructive  destructiveScanner
	cli          cliScanner
	findingsPub  gcp.Publisher[*riskv1.Finding]
	riskRecorder *metering.RiskRecorder
}

func NewHandler(logger *slog.Logger, shadow shadowScanner, destructive destructiveScanner, cli cliScanner, findingsPub gcp.Publisher[*riskv1.Finding], riskRecorder *metering.RiskRecorder) *Handler {
	return &Handler{
		logger:       logger.With(attr.SlogComponent("tool-call-analyzer")),
		shadow:       shadow,
		destructive:  destructive,
		cli:          cli,
		findingsPub:  findingsPub,
		riskRecorder: riskRecorder,
	}
}

func (h *Handler) Handle(ctx context.Context, m *riskv1.ToolCallAnalysis, _ gcp.MessageMetadata) error {
	projectID, err := uuid.Parse(m.GetProjectId())
	if err != nil {
		return fmt.Errorf("parse project id: %w", err)
	}
	policyID, err := uuid.Parse(m.GetRiskPolicyId())
	if err != nil {
		return fmt.Errorf("parse risk policy id: %w", err)
	}

	shadowCalls := make([]shadowmcpscan.ToolCall, 0, len(m.GetToolCalls()))
	destructiveCalls := make([]destructivetool.ToolCall, 0, len(m.GetToolCalls()))
	cliCalls := make([]clidestructive.ToolCall, 0, len(m.GetToolCalls()))
	for _, call := range m.GetToolCalls() {
		createdAt, err := time.Parse(time.RFC3339Nano, call.GetCreatedAt())
		if err != nil {
			return fmt.Errorf("parse tool call created_at: %w", err)
		}
		shadowCalls = append(shadowCalls, shadowmcpscan.ToolCall{
			ID:        call.GetId(),
			Name:      call.GetName(),
			Arguments: call.GetArguments(),
			CreatedAt: createdAt,
			Sender:    call.GetSender(),
		})
		destructiveCalls = append(destructiveCalls, destructivetool.ToolCall{Name: call.GetName(), Arguments: call.GetArguments()})
		cliCalls = append(cliCalls, clidestructive.ToolCall{Name: call.GetName(), Arguments: call.GetArguments()})
	}

	startedAt := time.Now().UTC()
	var findings []scanners.Finding
	var cliResult scanners.Result
	sources := newSourceSet(m.GetSources())
	if sources.Has(shadowmcpscan.Source) {
		batches := h.shadow.Scan(ctx, m.GetOrganizationId(), projectID, policyID, []shadowmcpscan.Message{{UserID: m.GetUserId(), ToolCalls: shadowCalls}})
		if len(batches) > 0 {
			findings = append(findings, batches[0]...)
		}
	}
	if sources.Has(destructivetool.Source) {
		findings = append(findings, h.destructive.Scan(ctx, m.GetOrganizationId(), destructiveCalls)...)
	}
	if sources.Has(clidestructive.Source) {
		cliResult = h.cli.Scan(ctx, cliCalls)
		findings = append(findings, cliResult.Findings...)
	}

	results, ruleIDs := scanners.StartPublishFindings(ctx, h.findingsPub, scanners.FindingMetadata{
		RequestID:         m.GetRequestId(),
		ChatMessageID:     m.GetChatMessageId(),
		ContentPartID:     m.GetContentPartId(),
		ProjectID:         m.GetProjectId(),
		OrganizationID:    m.GetOrganizationId(),
		RiskPolicyID:      m.GetRiskPolicyId(),
		RiskPolicyVersion: m.GetRiskPolicyVersion(),
		Shadow:            false,
	}, findings)

	published := 0
	var publishErr error
	for _, result := range results {
		if _, err := result.Get(ctx); err != nil {
			publishErr = errors.Join(publishErr, err)
			continue
		}
		published++
	}

	h.logger.InfoContext(ctx, "tool call scan complete", attr.SlogValueAny(map[string]any{
		"request_id":      m.GetRequestId(),
		"chat_message_id": m.GetChatMessageId(),
		"detections":      len(findings),
		"published":       published,
		"rule_ids":        ruleIDs,
	}))

	if cliResult.Completed {
		provenance, provenanceErr := scanners.ParseRiskProvenance(m, m.GetMessageType(), "async")
		if provenanceErr != nil {
			h.logger.WarnContext(ctx, "skipping CLI destructive usage with invalid attribution", attr.SlogError(provenanceErr))
		} else if err := h.riskRecorder.Record(ctx, metering.RiskCLIDestructive(), provenance, cliResult.STokens, startedAt); err != nil {
			h.logger.ErrorContext(ctx, "record CLI destructive usage", attr.SlogError(err))
		}
	}
	if publishErr != nil {
		return fmt.Errorf("publish tool call findings: %w", publishErr)
	}
	return nil
}

type sourceSet map[string]struct{}

func newSourceSet(sources []string) sourceSet {
	set := make(sourceSet, len(sources))
	for _, source := range sources {
		set[source] = struct{}{}
	}
	return set
}

func (s sourceSet) Has(source string) bool {
	_, ok := s[source]
	return ok
}
