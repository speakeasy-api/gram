package hooks

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

const (
	shadowMCPApprovalRequestTokenTTL = 7 * 24 * time.Hour
	shadowMCPApprovalRequestPrompt   = "Would you like me to open this link in a browser?"
)

type shadowMCPRequestLinkParams struct {
	OrganizationID  string
	ProjectID       string
	RequesterUserID string
	UserMessage     *string
	AuditReason     string
	Evidence        shadowmcp.AccessEvidence
	ToolName        string
	ToolInput       any
	RiskPolicyID    string
}

func (s *Service) renderShadowMCPUserBlockReason(ctx context.Context, params shadowMCPRequestLinkParams) string {
	message := renderUserBlockReason(params.UserMessage, params.AuditReason)
	requestURL, ok := s.shadowMCPApprovalRequestURL(ctx, params)
	if !ok {
		return message
	}
	return strings.TrimSpace(message) + "\n\nRequest access:\n" + requestURL + "\n\n" + shadowMCPApprovalRequestPrompt
}

func (s *Service) shadowMCPApprovalRequestURL(ctx context.Context, params shadowMCPRequestLinkParams) (string, bool) {
	if s.siteURL == nil || strings.TrimSpace(s.jwtSecret) == "" {
		return "", false
	}

	evidence := shadowmcp.NormalizeAccessEvidence(params.Evidence)
	if evidence.Empty() {
		return "", false
	}

	token, _, err := access.GenerateShadowMCPApprovalRequestToken(s.jwtSecret, access.ShadowMCPApprovalRequestTokenInput{
		OrganizationID:         params.OrganizationID,
		ProjectID:              params.ProjectID,
		RequesterUserID:        params.RequesterUserID,
		ObservedName:           observedShadowMCPName(evidence, params.ToolName),
		ObservedFullURL:        stringPtrOrNil(evidence.FullURL),
		ObservedURLHost:        stringPtrOrNil(evidence.URLHost),
		ObservedServerIdentity: stringPtrOrNil(evidence.ServerIdentity),
		ToolName:               stringPtrOrNil(params.ToolName),
		ToolCall:               nil,
		BlockReason:            stringPtrOrNil(params.AuditReason),
		RiskPolicyID:           uuidStringPtrOrNil(params.RiskPolicyID),
		RiskResultID:           nil,
	}, shadowMCPApprovalRequestTokenTTL)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to generate shadow mcp approval request link",
			attr.SlogError(err),
			attr.SlogOrganizationID(params.OrganizationID),
			attr.SlogProjectID(params.ProjectID),
		)
		return "", false
	}

	requestURL := s.siteURL.JoinPath("shadow-mcp", "request")
	query := url.Values{}
	query.Set("request_token", token)
	requestURL.Fragment = query.Encode()
	return requestURL.String(), true
}

func observedShadowMCPName(evidence shadowmcp.AccessEvidence, toolName string) *string {
	switch {
	case evidence.ServerIdentity != "":
		name := humanizeShadowMCPServerIdentity(evidence.ServerIdentity)
		return &name
	case evidence.URLHost != "":
		return &evidence.URLHost
	case toolName != "":
		return &toolName
	default:
		return nil
	}
}

func humanizeShadowMCPServerIdentity(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '_' || r == '-' || r == '.' || r == ':' || r == ' '
	})
	if len(parts) == 0 {
		return value
	}

	words := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		words = append(words, humanizeShadowMCPServerIdentityWord(part))
	}
	if len(words) == 0 {
		return value
	}
	return strings.Join(words, " ")
}

func humanizeShadowMCPServerIdentityWord(value string) string {
	lower := strings.ToLower(value)
	switch lower {
	case "ai", "api", "http", "https", "mcp", "oauth", "url":
		return strings.ToUpper(lower)
	case "github":
		return "GitHub"
	default:
		return strings.ToUpper(lower[:1]) + lower[1:]
	}
}

func stringPtrOrNil(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func uuidStringPtrOrNil(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	if _, err := uuid.Parse(trimmed); err != nil {
		return nil
	}
	return &trimmed
}
