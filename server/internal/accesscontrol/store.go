package accesscontrol

import (
	"context"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/matchvalue"
)

const (
	ResourceTypeShadowMCP = "shadow_mcp"

	RequestStatusRequested = "requested"
	RequestStatusApproved  = "approved"
	RequestStatusDenied    = "denied"

	DispositionAllowed = "allowed"
	DispositionDenied  = "denied"

	AccessScopeOrganization = "organization"
	AccessScopeProject      = "project"

	MatchKindFullURL        = "full_url"
	MatchKindURLHost        = "url_host"
	MatchKindServerIdentity = "server_identity"
)

const AlphaTTL = 90 * 24 * time.Hour

type ObservedSummary struct {
	Name           *string
	FullURL        *string
	URLHost        *string
	ServerIdentity *string
	ToolName       *string
	ToolCall       *string
	BlockReason    *string
}

type AccessApprovalRequest struct {
	ID                   string
	OrganizationID       string
	ProjectID            string
	ResourceType         string
	Status               string
	RequesterUserID      string
	RequesterEmail       string
	RequesterDisplayName string
	RequestFingerprint   string
	DisplayName          string
	ObservedSummary      ObservedSummary
	RequestedAt          time.Time
	DecidedAt            *time.Time
	DecidedBy            string
	DecisionNote         string
	SourceRuleIDs        []string
}

type AccessRule struct {
	ID              string
	OrganizationID  string
	ProjectID       string
	AccessScope     string
	ResourceType    string
	Disposition     string
	MatchKind       string
	MatchValue      string
	DisplayName     string
	ObservedSummary ObservedSummary
	SourceRequestID string
	CreatedBy       string
	UpdatedBy       string
	Reason          string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type RequestFilters struct {
	OrganizationID string
	ProjectID      string
	ResourceType   string
	Status         string
	Limit          int
	Cursor         string
}

type RuleFilters struct {
	OrganizationID string
	ProjectID      string
	AccessScope    string
	ResourceType   string
	Disposition    string
	Limit          int
	Cursor         string
}

type MatchingRuleFilters struct {
	OrganizationID string
	ProjectID      string
	ResourceType   string
	MatchKinds     []string
	MatchValues    []string
}

type ListRequestsResult struct {
	Requests   []AccessApprovalRequest
	NextCursor string
}

type ListRulesResult struct {
	Rules      []AccessRule
	NextCursor string
}

type Store interface {
	ListRequests(ctx context.Context, filters RequestFilters) (ListRequestsResult, error)
	UpsertRequest(ctx context.Context, request AccessApprovalRequest) (AccessApprovalRequest, bool, error)
	GetRequest(ctx context.Context, organizationID, resourceType, id string) (AccessApprovalRequest, error)
	DecideRequest(ctx context.Context, organizationID, resourceType, id, status, decidedBy, note string, sourceRuleIDs []string) (AccessApprovalRequest, error)
	ListRules(ctx context.Context, filters RuleFilters) (ListRulesResult, error)
	CreateRule(ctx context.Context, rule AccessRule) (AccessRule, error)
	UpdateRule(ctx context.Context, rule AccessRule) (AccessRule, error)
	DeleteRule(ctx context.Context, organizationID, resourceType, id string) (AccessRule, error)
	GetRule(ctx context.Context, organizationID, resourceType, id string) (AccessRule, error)
	GetRuleByMatch(ctx context.Context, organizationID, resourceType, accessScope, projectID, matchKind, matchValue string) (AccessRule, error)
	ListMatchingRules(ctx context.Context, filters MatchingRuleFilters) ([]AccessRule, error)
}

func CanonicalizeMatchValue(matchKind, raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}

	switch matchKind {
	case MatchKindFullURL, MatchKindURLHost, MatchKindServerIdentity:
		if normalized, err := matchvalue.Normalize(matchKind, value); err == nil {
			return normalized
		}
	}

	return strings.Join(strings.Fields(value), " ")
}
