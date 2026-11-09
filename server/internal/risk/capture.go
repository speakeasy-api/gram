package risk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/captureredact"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

// CaptureInspector preserves policy findings before capture removes their
// credential values. It returns only prepared, non-recoverable finding events;
// the message writer commits those events alongside the redacted observation.
type CaptureInspector struct {
	// scanner supplies the warm detector and the existing category scopes.
	scanner *Scanner

	// redactor shares the detector while decoding the same JSON surfaces as capture.
	redactor *captureredact.Redactor

	// preparer resolves raw-dependent suppression and fingerprint metadata.
	preparer *SafeFindingPreparer

	// now timestamps detection independently of the captured message's time.
	now func() time.Time
}

// NewCaptureInspector reuses the server's policy scanner. Inspection does not
// enforce a verdict or change the payload the harness or model executes.
func NewCaptureInspector(scanner *Scanner, preparer *SafeFindingPreparer) *CaptureInspector {
	var redactor *captureredact.Redactor
	if scanner != nil {
		redactor = captureredact.NewWithScanner(scanner.gitleaks)
	}
	return &CaptureInspector{scanner: scanner, redactor: redactor, preparer: preparer, now: time.Now}
}

// InspectCapture finds the credential matches erased by capture redaction.
// Generic assignment heuristics remain in the stored observation and continue
// through the existing asynchronous scanner. The policy admission matches the
// async path: enabled unscoped standard policies selecting gitleaks, with the
// same category scope. Policy audiences govern enforcement independently.
func (c *CaptureInspector) InspectCapture(ctx context.Context, input chat.CaptureInput) ([]*riskv1.Finding, error) {
	if c == nil || c.scanner == nil || c.preparer == nil {
		return nil, errors.New("capture finding inspector is unavailable")
	}
	if input.ProjectID == uuid.Nil || strings.TrimSpace(input.OrgID) == "" ||
		(input.MessageID == uuid.Nil) == (input.ContentPartID == uuid.Nil) {
		return nil, errors.New("capture finding requires a project, organization and one anchor")
	}
	view, surface, parts, ok := captureMessageView(input)
	if !ok || surface == "" {
		return nil, nil
	}
	policies, err := c.scanner.repo.ListEnabledUnscopedRiskPoliciesByProject(ctx, input.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("list capture risk policies: %w", err)
	}
	if len(policies) == 0 {
		return nil, nil
	}
	result, err := c.inspectSurface(ctx, surface, parts)
	if err != nil {
		return nil, fmt.Errorf("scan capture credentials: %w", err)
	}
	if !result.Completed {
		return nil, errors.New("capture credential scan is incomplete")
	}
	findings := slices.DeleteFunc(result.Findings, func(f scanners.Finding) bool {
		return !captureredact.RedactsRule(f.RuleID)
	})
	if len(findings) == 0 {
		return nil, nil
	}
	createdAt := c.now().UTC().Format(time.RFC3339)
	prepared := make([]*riskv1.Finding, 0, len(findings)*len(policies))
	for _, policy := range policies {
		if policy.OrganizationID != input.OrgID {
			return nil, errors.New("capture policy organization does not match observation")
		}
		if policy.PolicyType == ra.PolicyTypePromptBased || !slices.Contains(policy.Sources, ra.SourceGitleaks) {
			continue
		}
		specified, err := ra.CompileDetectionScopes(c.scanner.celEng, ra.DetectionScopesFromConfig(policy.AnalyzerConfig))
		if err != nil {
			return nil, fmt.Errorf("compile capture detection scopes: %w", err)
		}
		scopes := ra.NewCategoryScope(c.scanner.recommended, specified)
		if !scopes.SourceInScope(view, ra.SourceGitleaks) {
			continue
		}
		selected := scopes.FilterFindings(view, findings)
		if len(selected) == 0 {
			continue
		}
		anchor, contentPart := input.MessageID, false
		if input.ContentPartID != uuid.Nil {
			anchor, contentPart = input.ContentPartID, true
		}
		requestID := ra.BatchScanRequestID(input.ProjectID, policy.ID, policy.Version, "standard", contentPart, anchor).String()
		meta := scanners.FindingMetadata{
			RequestID: requestID, ChatMessageID: uuidString(input.MessageID), ContentPartID: uuidString(input.ContentPartID),
			ProjectID: input.ProjectID.String(), OrganizationID: input.OrgID,
			RiskPolicyID: policy.ID.String(), RiskPolicyVersion: policy.Version, Shadow: false,
		}
		ids := scanners.FindingIDs(meta, selected)
		for i, finding := range selected {
			start, end := conv.SafeInt32(finding.StartPos), conv.SafeInt32(finding.EndPos)
			surfaceKind := scanners.FindingSurface(finding.Source, finding.Field, finding.Path)
			if finding.Field == captureredact.DecodedJSONField {
				surfaceKind = scanners.SurfaceNone
			}
			published := riskv1.Finding_builder{
				Id: new(ids[i].String()), RequestId: &requestID,
				ChatMessageId: conv.PtrEmpty(meta.ChatMessageID), ContentPartId: conv.PtrEmpty(meta.ContentPartID),
				ProjectId: &meta.ProjectID, OrganizationId: &meta.OrganizationID,
				RiskPolicyId: &meta.RiskPolicyID, RiskPolicyVersion: &meta.RiskPolicyVersion, CreatedAt: &createdAt,
				RuleId: &finding.RuleID, Description: &finding.Description, Match: &finding.Match,
				StartPos: &start, EndPos: &end, Tags: finding.Tags, Source: &finding.Source, Confidence: &finding.Confidence,
				Surface: &surfaceKind, Field: &finding.Field, Path: &finding.Path,
				ToolCallId: &finding.McpLookupToolCallID, EventKind: new(chrepo.EventKindFinding),
			}.Build()
			published.SetTags(append(slices.Clone(finding.Tags), CaptureRedactionTag))
			published.SetDescription("Credential removed from captured content: " + finding.Description)
			if err := c.preparer.Prepare(ctx, published); err != nil {
				return nil, fmt.Errorf("prepare capture finding: %w", err)
			}
			if published.GetSafeMatch().GetDisabledRule() || published.GetSafeMatch().GetBuiltinPreset() {
				continue
			}
			// Capture privacy is deterministic in every analyzer mode. Keep the
			// actual scanner source for existing exclusions and filters, while
			// making clear this is redaction evidence rather than a model verdict.
			published.GetSafeMatch().SetCaptureRedacted(true)
			if err := ValidateSafeFinding(published); err != nil {
				return nil, fmt.Errorf("validate prepared capture finding: %w", err)
			}
			prepared = append(prepared, published)
		}
	}
	return prepared, nil
}

type captureTextPart struct {
	// text is one content or argument string before scan-surface concatenation.
	text string

	// offset is its byte position in the concatenated scan surface.
	offset int
}

// inspectSurface keeps ordinary findings anchored to the original scan surface,
// while also decoding JSON argument strings that concatenation made non-JSON.
func (c *CaptureInspector) inspectSurface(ctx context.Context, surface string, parts []captureTextPart) (scanners.Result, error) {
	result, err := c.redactor.InspectText(ctx, surface)
	if err != nil {
		return result, fmt.Errorf("inspect capture surface: %w", err)
	}
	for i, part := range parts {
		if !json.Valid([]byte(part.text)) || (part.offset == 0 && part.text == surface) {
			continue
		}
		decoded, err := c.redactor.InspectText(ctx, part.text)
		if err != nil {
			return result, fmt.Errorf("inspect capture JSON field: %w", err)
		}
		result.STokens += decoded.STokens
		for _, finding := range decoded.Findings {
			if finding.Field == captureredact.DecodedJSONField {
				finding.Path = fmt.Sprintf("$parts[%d].%s", i, finding.Path)
			} else {
				finding.StartPos += part.offset
				finding.EndPos += part.offset
			}
			duplicate := slices.ContainsFunc(result.Findings, func(existing scanners.Finding) bool {
				return existing.RuleID == finding.RuleID && existing.StartPos == finding.StartPos && existing.EndPos == finding.EndPos && existing.Field == finding.Field && existing.Path == finding.Path
			})
			if !duplicate {
				result.Findings = append(result.Findings, finding)
			}
		}
	}
	return result, nil
}

func uuidString(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

// captureMessageView mirrors the async scanner's role and tool-argument
// projection. Capture scans the full submitted text because values outside the
// async scan budget must also be removed before storage.
func captureMessageView(input chat.CaptureInput) (ra.MessageView, string, []captureTextPart, bool) {
	view := ra.MessageView{Content: input.Content, Type: "", Tools: nil}
	parts := []captureTextPart{{text: input.Content, offset: 0}}
	switch input.Role {
	case "user":
		view.Type = message.User
	case "tool":
		view.Type = message.ToolResponse
	case "assistant":
		view.Type = message.Assistant
		if len(input.ToolCalls) > 0 {
			view.Type = message.ToolRequest
		}
	case message.PromptAttachment:
		view.Type = message.PromptAttachment
	default:
		return view, "", nil, false
	}
	if view.Type != message.ToolRequest {
		return view, input.Content, parts, true
	}
	var calls []struct {
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	if json.Unmarshal(input.ToolCalls, &calls) != nil {
		view.Tools = []ra.ToolView{ra.NewToolView("tool_calls", string(input.ToolCalls))}
		parts = append(parts, captureTextPart{text: string(input.ToolCalls), offset: captureArgumentOffset(input.Content)})
		return view, appendCaptureArguments(input.Content, string(input.ToolCalls)), parts, true
	}
	surface := input.Content
	for _, call := range calls {
		if call.Function.Name != "" || strings.TrimSpace(call.Function.Arguments) != "" {
			view.Tools = append(view.Tools, ra.NewToolView(call.Function.Name, call.Function.Arguments))
		}
		parts = append(parts, captureTextPart{text: call.Function.Arguments, offset: captureArgumentOffset(surface)})
		surface = appendCaptureArguments(surface, call.Function.Arguments)
	}
	return view, surface, parts, true
}

func captureArgumentOffset(surface string) int {
	if surface == "" {
		return 0
	}
	return len(surface) + 1
}

func appendCaptureArguments(surface, arguments string) string {
	if arguments == "" {
		return surface
	}
	if surface == "" {
		return arguments
	}
	return surface + "\n" + arguments
}
