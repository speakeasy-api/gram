package hooks

import (
	"context"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
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
	// PolicyName feeds the structured "block" effect on the canonical ingest
	// path; legacy per-provider handlers may leave it empty.
	PolicyName string
}

// shadowMCPRequestLink is a minted approval-request link plus the fields the
// structured "block" effect needs alongside it.
type shadowMCPRequestLink struct {
	URL        string
	Token      string
	ExpiresAt  time.Time
	ServerName string
	ServerURL  string
}

func (s *Service) renderShadowMCPUserBlockReason(ctx context.Context, params shadowMCPRequestLinkParams) string {
	message := renderUserBlockReason(params.UserMessage, params.AuditReason)
	link, ok := s.shadowMCPApprovalRequestURL(ctx, params)
	if !ok {
		return message
	}
	setBlockEffect(ctx, blockEffect{
		Category:         string(categories.CategoryShadowMCP),
		RequestToken:     link.Token,
		RequestURL:       link.URL,
		RequestExpiresAt: link.ExpiresAt,
		ServerName:       link.ServerName,
		ServerURL:        link.ServerURL,
		PolicyName:       params.PolicyName,
		ToolName:         params.ToolName,
		BlockURL:         "",
	})
	return strings.TrimSpace(message) + "\n\nRequest access:\n" + link.URL + "\n\n" + shadowMCPApprovalRequestPrompt
}

func (s *Service) shadowMCPApprovalRequestURL(ctx context.Context, params shadowMCPRequestLinkParams) (shadowMCPRequestLink, bool) {
	if s.siteURL == nil || s.cache == nil || strings.TrimSpace(s.jwtSecret) == "" {
		return shadowMCPRequestLink{}, false
	}

	evidence := shadowmcp.NormalizeAccessEvidence(params.Evidence)
	if evidence.FullURL == "" && evidence.URLHost == "" && evidence.ServerIdentity == "" {
		return shadowMCPRequestLink{}, false
	}

	requestURL, token, expiry, err := risk.GeneratePolicyBypassRequestURL(ctx, s.cache, s.siteURL, risk.PolicyBypassRequestTokenInput{
		OrganizationID:         params.OrganizationID,
		ProjectID:              params.ProjectID,
		RequesterUserID:        params.RequesterUserID,
		ObservedName:           shadowmcp.ObservedName(evidence, params.ToolName),
		ObservedFullURL:        stringPtrOrNil(evidence.FullURL),
		ObservedURLHost:        stringPtrOrNil(evidence.URLHost),
		ObservedServerIdentity: stringPtrOrNil(evidence.ServerIdentity),
		ToolName:               stringPtrOrNil(params.ToolName),
		ToolCall:               nil,
		BlockReason:            stringPtrOrNil(params.AuditReason),
		RiskPolicyID:           params.RiskPolicyID,
		RiskResultID:           nil,
	}, shadowMCPApprovalRequestTokenTTL)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to generate shadow mcp approval request link",
			attr.SlogError(err),
			attr.SlogOrganizationID(params.OrganizationID),
			attr.SlogProjectID(params.ProjectID),
		)
		return shadowMCPRequestLink{}, false
	}

	return shadowMCPRequestLink{
		URL:        requestURL,
		Token:      token,
		ExpiresAt:  expiry,
		ServerName: conv.PtrValOr(shadowmcp.ObservedName(evidence, params.ToolName), ""),
		ServerURL:  evidence.FullURL,
	}, true
}

func stringPtrOrNil(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
