package scanners

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/metering"
)

// Result describes both a scanner's findings and whether real scan work
// completed. Findings remain independent from completion so fail-closed
// findings cannot be mistaken for billable work.
type Result struct {
	// Findings contains the detections or fail-closed findings returned to the
	// caller.
	Findings []Finding

	// STokens is the exact count of prepared content supplied to the scanner.
	STokens int64

	// Completed is true only when a real scanner successfully produced a
	// complete, valid result, including a clean result.
	Completed bool
}

// AsyncRiskOperationID identifies one scanner execution independently of
// delivery attempt or batch ordering. The meter definition supplies scanner
// identity, so the operation key only needs path, originating policy, and the
// most specific stable content anchor.
func AsyncRiskOperationID(executionPath, policyID string, policyVersion int64, inputMessageID, inputPartID, requestID string) string {
	anchorKind, anchorID := "request", requestID
	if inputPartID != "" {
		anchorKind, anchorID = "input_part", inputPartID
	} else if inputMessageID != "" {
		anchorKind, anchorID = "input_message", inputMessageID
	}
	return strings.Join([]string{executionPath, policyID, strconv.FormatInt(policyVersion, 10), anchorKind, anchorID}, ":")
}

// RiskMessage is the common attribution carried by asynchronous risk scan
// envelopes. Message type is supplied separately because custom rules names
// that field kind.
type RiskMessage interface {
	GetRequestId() string
	GetChatMessageId() string
	GetProjectId() string
	GetOrganizationId() string
	GetRiskPolicyId() string
	GetRiskPolicyVersion() int64
	GetContentPartId() string
	GetChatId() string
	GetExternalConversationId() string
	GetParentChatMessageId() string
	GetOriginRiskPolicyId() string
	GetOriginRiskPolicyVersion() int64
	GetPolicyLinkReason() string
	GetMessageLinkReason() string
	GetExecutionPath() string
	GetToolCallId() string
	GetToolName() string
	GetHookSource() string
	GetUserId() string
}

// ParseRiskProvenance validates and normalizes attribution shared by async risk
// consumers. Legacy envelopes that predate origin fields use their policy
// fields only when both form a complete policy identity.
func ParseRiskProvenance(m RiskMessage, messageType, defaultExecutionPath string) (metering.RiskProvenance, error) {
	projectID, err := uuid.Parse(m.GetProjectId())
	if err != nil {
		return metering.RiskProvenance{}, fmt.Errorf("parse project id: %w", err)
	}
	if projectID == uuid.Nil {
		return metering.RiskProvenance{}, fmt.Errorf("project id must not be nil")
	}
	if strings.TrimSpace(m.GetOrganizationId()) == "" {
		return metering.RiskProvenance{}, fmt.Errorf("organization id must not be empty")
	}

	policyText := m.GetOriginRiskPolicyId()
	policyVersion := m.GetOriginRiskPolicyVersion()
	if policyText == "" && policyVersion == 0 && m.GetPolicyLinkReason() == "" && m.GetRiskPolicyId() != "" && m.GetRiskPolicyVersion() > 0 {
		policyText = m.GetRiskPolicyId()
		policyVersion = m.GetRiskPolicyVersion()
	}
	policyID, err := parseOptionalUUID(policyText)
	if err != nil {
		return metering.RiskProvenance{}, fmt.Errorf("parse origin risk policy id: %w", err)
	}
	chatID, err := parseOptionalUUID(m.GetChatId())
	if err != nil {
		return metering.RiskProvenance{}, fmt.Errorf("parse chat id: %w", err)
	}
	chatMessageText := m.GetChatMessageId()
	if chatMessageText == "" {
		chatMessageText = m.GetParentChatMessageId()
	}
	chatMessageID, err := parseOptionalUUID(chatMessageText)
	if err != nil {
		return metering.RiskProvenance{}, fmt.Errorf("parse chat message id: %w", err)
	}
	contentPartID, err := parseOptionalUUID(m.GetContentPartId())
	if err != nil {
		return metering.RiskProvenance{}, fmt.Errorf("parse content part id: %w", err)
	}
	executionPath := m.GetExecutionPath()
	if executionPath == "" {
		executionPath = defaultExecutionPath
	}
	if policyID == uuid.Nil {
		if policyVersion != 0 || strings.TrimSpace(m.GetPolicyLinkReason()) == "" {
			return metering.RiskProvenance{}, fmt.Errorf("risk provenance requires originating policy or explicit no-policy reason")
		}
	} else if policyVersion <= 0 || m.GetPolicyLinkReason() != "" {
		return metering.RiskProvenance{}, fmt.Errorf("policy-triggered risk provenance requires a version and no unlinked policy reason")
	}
	if strings.TrimSpace(executionPath) == "" {
		return metering.RiskProvenance{}, fmt.Errorf("risk provenance requires execution path")
	}
	if chatMessageID == uuid.Nil && strings.TrimSpace(m.GetMessageLinkReason()) == "" {
		return metering.RiskProvenance{}, fmt.Errorf("risk provenance requires a chat message id or explicit unlinked reason")
	}
	if chatMessageID != uuid.Nil && m.GetMessageLinkReason() != "" {
		return metering.RiskProvenance{}, fmt.Errorf("linked risk provenance must not have an unlinked reason")
	}

	return metering.RiskProvenance{
		OrganizationID:         m.GetOrganizationId(),
		ProjectID:              projectID,
		RiskPolicyID:           policyID,
		RiskPolicyVersion:      policyVersion,
		PolicyLinkReason:       m.GetPolicyLinkReason(),
		ChatID:                 chatID,
		ExternalConversationID: m.GetExternalConversationId(),
		ChatMessageID:          chatMessageID,
		ContentPartID:          contentPartID,
		MessageLinkReason:      m.GetMessageLinkReason(),
		OperationID:            AsyncRiskOperationID(executionPath, policyText, policyVersion, chatMessageText, m.GetContentPartId(), m.GetRequestId()),
		ExecutionPath:          executionPath,
		RequestID:              m.GetRequestId(),
		MessageType:            messageType,
		HookSource:             m.GetHookSource(),
		UserID:                 m.GetUserId(),
		ToolCallID:             m.GetToolCallId(),
		ToolName:               m.GetToolName(),
		Model:                  "",
		Provider:               "",
	}, nil
}

func parseOptionalUUID(value string) (uuid.UUID, error) {
	if value == "" {
		return uuid.Nil, nil
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse optional UUID: %w", err)
	}
	return id, nil
}

// Finding represents a single secret or sensitive data match found in a message.
type Finding struct {
	RuleID           string
	Description      string
	Match            string
	StartPos         int
	EndPos           int
	Tags             []string
	Source           string
	Confidence       float64
	DeadLetterReason string

	McpLookupToolCallID string
	SpanGroupKey        string
	Field               string
	Path                string
}

// FieldToolCalls marks a judge verdict over a tool request's calls as a
// whole; no stored text can reveal it.
const FieldToolCalls = "tool_calls"

// Surface values published on Finding.surface and stored in the ClickHouse
// risk_findings.surface column — which text the finding's start_pos/end_pos
// offsets index. Kept in sync with the column's schema comment
// (server/clickhouse/schema.sql) and the backfill's transform
// (server/cmd/tools/migrations/riskfindings).
const (
	SurfaceContent  = "content"
	SurfaceToolArgs = "tool_args"
	SurfaceJSONPath = "json_path"
	SurfaceDerived  = "derived"
	SurfaceNone     = "none"
)

// FindingSurface maps a finding's span attribution (field, path) — or, when
// the scanner records no spans, its source — to the text its offsets index.
// The span-level arms mirror the offline backfill's spanSurface so live rows
// and backfilled rows agree; the per-source defaults differ deliberately
// because live stream scanners index different text than the legacy sync-era
// rows the backfill covers (e.g. the gitleaks stream handler scans the verbatim
// request content, not the batch-composed scan surface). An unrecognized field
// (e.g. tool.name) leaves the surface empty so reveal falls back to its
// verified candidate cascade rather than slicing the wrong text.
//
// Source names are literals (same precedent as the categories package) because
// the scanner subpackages import this one. Canonical definitions:
//
//	gitleaks          server/internal/scanners/gitleaks.Source
//	presidio          pystreams/src/pystreams/risk/handler.py SOURCE_PRESIDIO
//	prompt_injection  server/internal/scanners/promptinjection.Source
//	llm_judge         server/internal/scanners/promptpolicy.Source
//	shadow_mcp        server/internal/scanners/shadowmcpscan.Source
//	account_identity  server/internal/background/activities/risk_analysis.SourceAccountIdentity
//	destructive_tool  server/internal/scanners/destructivetool.Source
//	cli_destructive   server/internal/scanners/clidestructive.Source
func FindingSurface(source, field, path string) string {
	switch {
	case path != "":
		return SurfaceJSONPath
	case field == "tool.args":
		return SurfaceToolArgs
	case field == "content", field == "prompt", field == "assistant", field == "tool_result":
		return SurfaceContent
	case field == "tool.server", field == "tool.function":
		return SurfaceDerived
	case field == FieldToolCalls:
		return SurfaceNone
	case field != "":
		return ""
	}

	switch source {
	case "gitleaks", "presidio", "prompt_injection":
		// These scanners match against the request's verbatim content, so the
		// offsets index the anchored message/part text (prompt_injection flags
		// the whole scanned content).
		return SurfaceContent
	case "llm_judge":
		// The judge's match is a rendered artifact of the judged content; the
		// rationale carries the signal and the offsets index no stored text.
		return SurfaceNone
	case "shadow_mcp", "account_identity", "destructive_tool", "cli_destructive":
		// The match is derived metadata (server identifier, account email,
		// tool name), not a slice of any recorded surface.
		return SurfaceDerived
	default:
		return ""
	}
}
