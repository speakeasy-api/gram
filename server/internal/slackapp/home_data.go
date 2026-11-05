package slackapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/risk"
	approvalrepo "github.com/speakeasy-api/gram/server/internal/risk/approval/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/slackapp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// homeListLimit bounds each Home tab list.
	homeListLimit = 5
)

// HomeFlags evaluates the rollout flags that gate Home tab sections.
// feature.Provider satisfies it.
type HomeFlags interface {
	IsFlagEnabled(ctx context.Context, flag feature.Flag, distinctID string, groups map[string]string) (bool, error)
}

// HomeSource loads what a linked Speakeasy user sees on the Slack app's Home
// tab: their organization's data across projects, filtered by their own
// effective permissions. Every failure hides the affected section rather
// than showing data the viewer may not be allowed to see.
type HomeSource struct {
	logger    *slog.Logger
	db        *pgxpool.Pool
	flags     HomeFlags
	serverURL *url.URL

	// isMember reports whether a user is an active organization member.
	isMember func(ctx context.Context, organizationID, userID string) (bool, error)
}

// NewHomeSource returns the Home tab data source. flags gates the tool
// approval and MCP review sections the way the dashboard does.
func NewHomeSource(logger *slog.Logger, db *pgxpool.Pool, flags HomeFlags) *HomeSource {
	s := &HomeSource{logger: logger.With(attr.SlogComponent("slack_app_home")), db: db, flags: flags, serverURL: nil, isMember: nil}
	s.isMember = func(ctx context.Context, organizationID, userID string) (bool, error) {
		member, err := orgrepo.New(s.db).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{UserID: userID, OrganizationID: organizationID})
		if err != nil {
			return false, fmt.Errorf("check organization membership: %w", err)
		}
		return member, nil
	}
	return s
}

// WithServerURL sets the public API origin used for existing MCP logo assets.
// Slack fetches these images without the viewer's browser session.
func (s *HomeSource) WithServerURL(serverURL *url.URL) *HomeSource {
	s.serverURL = serverURL
	return s
}

// Load builds the Home tab data for userID, an active member of
// organizationID the caller has already resolved from a linked Slack user.
func (s *HomeSource) Load(ctx context.Context, organizationID, userID, dashboardURL string) HomeData {
	logger := s.logger.With(attr.SlogOrganizationID(organizationID), attr.SlogUserID(userID))
	data := HomeData{DashboardURL: dashboardURL, Linked: true} //nolint:exhaustruct // sections are filled below
	queries := repo.New(s.db)

	org, err := queries.GetHomeOrganization(ctx, organizationID)
	if err != nil {
		logger.ErrorContext(ctx, "load organization for Slack app home", attr.SlogError(err))
		return failedHome(data)
	}
	data.OrganizationName = org.Name
	links := homeLinks{base: parseDashboardURL(dashboardURL), orgSlug: org.Slug}

	principals, err := authz.ResolveUserPrincipals(ctx, s.db, organizationID, userID)
	if err != nil {
		logger.ErrorContext(ctx, "resolve principals for Slack app home", attr.SlogError(err))
		return failedHome(data)
	}
	grants, err := authz.LoadGrants(ctx, s.db, organizationID, principals)
	if err != nil {
		logger.ErrorContext(ctx, "load grants for Slack app home", attr.SlogError(err))
		return failedHome(data)
	}
	isAdmin, err := authz.GrantsAuthorize(grants, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: organizationID, Dimensions: nil})
	if err != nil {
		logger.ErrorContext(ctx, "authorize Slack app home", attr.SlogError(err))
		isAdmin = false
	}
	groups := feature.OrgProjectGroups(org.Slug, "")

	if s.flagEnabled(ctx, logger, feature.FlagToolApprovals, organizationID, groups) {
		data.ApprovalsEnabled, data.Approvals, err = approvals(ctx, queries, organizationID, userID, approverPrincipals(principals), links)
		if err != nil {
			logger.ErrorContext(ctx, "load tool approvals for Slack app home", attr.SlogError(err))
			data.ApprovalsEnabled, data.Approvals, data.ApprovalsError = true, nil, true
		}
	}
	if isAdmin && s.flagEnabled(ctx, logger, feature.FlagMCPApproval, organizationID, groups) {
		data.MCPRequestsEnabled = true
		data.MCPRequests, err = mcpRequests(ctx, queries, organizationID, links)
		if err != nil {
			logger.ErrorContext(ctx, "load MCP requests for Slack app home", attr.SlogError(err))
			data.MCPRequests, data.MCPRequestsError = nil, true
		}
	}
	if isAdmin {
		data.RiskEnabled = true
		data.Risks, err = riskFindings(ctx, queries, organizationID, links)
		if err != nil {
			logger.ErrorContext(ctx, "load risk findings for Slack app home", attr.SlogError(err))
			data.Risks, data.RiskError = nil, true
		}
	}
	data.Servers, err = servers(ctx, queries, organizationID, grants, links, s.serverURL)
	if err != nil {
		logger.ErrorContext(ctx, "load servers for Slack app home", attr.SlogError(err))
		data.Servers, data.ServersError = nil, true
	}
	return data
}

// toolApprovalsEnabled re-checks the tool approvals rollout flag for a click
// on a Home tab published earlier. It fails closed.
func (s *HomeSource) toolApprovalsEnabled(ctx context.Context, organizationID string) bool {
	org, err := repo.New(s.db).GetHomeOrganization(ctx, organizationID)
	if err != nil {
		s.logger.ErrorContext(ctx, "load organization for Slack app home click", attr.SlogOrganizationID(organizationID), attr.SlogError(err))
		return false
	}
	return s.flagEnabled(ctx, s.logger, feature.FlagToolApprovals, organizationID, feature.OrgProjectGroups(org.Slug, ""))
}

// memberEmail names a decider in the audit log, when known.
func (s *HomeSource) memberEmail(ctx context.Context, organizationID, userID string) *string {
	members, err := approvalrepo.New(s.db).ListApprovalRecipientMembers(ctx, approvalrepo.ListApprovalRecipientMembersParams{OrganizationID: organizationID, AllMembers: false, UserIds: []string{userID}, MaxRows: 1})
	if err != nil || len(members) == 0 {
		return nil
	}
	return &members[0].Email
}

// failedHome marks every section as unavailable.
func failedHome(data HomeData) HomeData {
	data.ApprovalsError, data.MCPRequestsError, data.RiskError, data.ServersError = true, true, true, true
	return data
}

// flagEnabled fails closed: a flag-service error reads as off.
func (s *HomeSource) flagEnabled(ctx context.Context, logger *slog.Logger, flag feature.Flag, organizationID string, groups map[string]string) bool {
	if s.flags == nil {
		return false
	}
	enabled, err := s.flags.IsFlagEnabled(ctx, flag, organizationID, groups)
	if err != nil {
		logger.WarnContext(ctx, "Slack app home flag check failed; treating as disabled", attr.SlogError(err))
		return false
	}
	return enabled
}

// approvals lists the open tool approval requests the viewer is an approver
// for. A request the viewer made shows without a decision, since the
// requester cannot decide their own request.
func approvals(ctx context.Context, queries *repo.Queries, organizationID, userID string, approvers []string, links homeLinks) (bool, []HomeApproval, error) {
	isApprover, err := queries.HasHomeApproverPolicy(ctx, repo.HasHomeApproverPolicyParams{OrganizationID: organizationID, PrincipalUrns: approvers})
	if err != nil {
		return false, nil, fmt.Errorf("check approver policies: %w", err)
	}
	rows, err := queries.ListHomePendingApprovals(ctx, repo.ListHomePendingApprovalsParams{OrganizationID: organizationID, PrincipalUrns: approvers, MaxRows: homeListLimit})
	if err != nil {
		return false, nil, fmt.Errorf("list pending approvals: %w", err)
	}
	out := make([]HomeApproval, 0, len(rows))
	for _, row := range rows {
		query := url.Values{"tab": {"tool-approvals"}, "request": {row.ID.String()}}
		out = append(out, HomeApproval{
			ID:            row.ID.String(),
			ProjectID:     row.ProjectID.String(),
			PolicyName:    row.PolicyName,
			ToolName:      row.ToolName,
			RequesterName: homeRequesterName(row.RequesterName, row.RequesterUrn),
			URL:           links.project(row.ProjectSlug, query, "risk-policies"),
			CanDecide:     !row.RequesterUserID.Valid || row.RequesterUserID.String != userID,
		})
	}
	// A pending request on a disabled policy still counts: the viewer can
	// decide it, so the section shows.
	return isApprover || len(out) > 0, out, nil
}

// approverPrincipals is what a person can approve as: their user and roles.
// "All users" is left out, since an approver list never names it.
func approverPrincipals(principals []urn.Principal) []string {
	out := make([]string, 0, len(principals))
	for _, principal := range principals {
		if principal.ID == urn.AllUsersPrincipalID {
			continue
		}
		out = append(out, principal.String())
	}
	return out
}

// homeRequesterName names who made a call. Agents have no display name, so
// they read as a short ID.
func homeRequesterName(name, requesterURN string) string {
	if name != "" {
		return name
	}
	_, id, ok := strings.Cut(requesterURN, ":")
	if !ok {
		return requesterURN
	}
	if len(id) > 8 {
		id = id[:8]
	}
	return "agent " + id
}

func mcpRequests(ctx context.Context, queries *repo.Queries, organizationID string, links homeLinks) ([]HomeMCPRequest, error) {
	rows, err := queries.ListHomeMCPRequests(ctx, repo.ListHomeMCPRequestsParams{OrganizationID: organizationID, MaxRows: homeListLimit})
	if err != nil {
		return nil, fmt.Errorf("list MCP requests: %w", err)
	}
	out := make([]HomeMCPRequest, 0, len(rows))
	for _, row := range rows {
		out = append(out, HomeMCPRequest{
			Name:        row.TargetRaw,
			ProjectName: row.ProjectName,
			URL:         links.project(row.ProjectSlug, url.Values{"tab": {"mcp-approvals"}}, "risk-policies"),
		})
	}
	return out, nil
}

func riskFindings(ctx context.Context, queries *repo.Queries, organizationID string, links homeLinks) ([]HomeRisk, error) {
	rules, customHigh, err := homeCategoryRules()
	if err != nil {
		return nil, err
	}
	rows, err := queries.ListHomeHighRiskFindings(ctx, repo.ListHomeHighRiskFindingsParams{CategoryRules: rules, CustomHigh: customHigh, OrganizationID: organizationID, MaxRows: homeListLimit})
	if err != nil {
		return nil, fmt.Errorf("list risk findings: %w", err)
	}
	out := make([]HomeRisk, 0, len(rows))
	for _, row := range rows {
		out = append(out, HomeRisk{
			PolicyName:  row.PolicyName,
			Severity:    homeFindingSeverity(row.PolicyLive.Bool, row.PolicyScore, row.Source, row.RuleID),
			ProjectName: row.ProjectName,
			URL:         links.project(row.ProjectSlug, nil, "risk-events"),
			CreatedAt:   row.CreatedAt.Time,
		})
	}
	return out, nil
}

// homeFindingSeverity is the dashboard's band for a finding: its live
// policy's score, or its category's fallback when the policy is deleted or
// unscored.
func homeFindingSeverity(policyLive bool, policyScore float64, source, ruleID string) string {
	if policyLive {
		return risk.FindingSeverity(policyScore, "")
	}
	return risk.FindingSeverity(0, string(categories.Classify(source, ruleID)))
}

// homeHighBand reports whether a severity band is high or critical.
func homeHighBand(band string) bool {
	return band == "high" || band == "critical"
}

// homeCategoryRule is one category in categories.Classify order, as the
// high-risk query reads it.
type homeCategoryRule struct {
	Ord     int      `json:"ord"`
	Source  string   `json:"source"`
	Prefix  string   `json:"prefix"`
	RuleIDs []string `json:"rule_ids"`
	High    bool     `json:"high"`
}

// homeCategoryRules encodes categories.Classify and each category's fallback
// band for the high-risk query, plus whether an unmatched finding is high.
func homeCategoryRules() ([]byte, bool, error) {
	rules := make([]homeCategoryRule, 0, len(categories.Definitions))
	for i, def := range categories.Definitions {
		ruleIDs := def.RuleIDs
		if ruleIDs == nil {
			ruleIDs = []string{}
		}
		rules = append(rules, homeCategoryRule{Ord: i, Source: def.Source, Prefix: def.RulePrefix, RuleIDs: ruleIDs, High: homeHighBand(risk.FindingSeverity(0, string(def.Category)))})
	}
	encoded, err := json.Marshal(rules)
	if err != nil {
		return nil, false, fmt.Errorf("encode risk categories: %w", err)
	}
	return encoded, homeHighBand(risk.FindingSeverity(0, string(categories.CategoryCustom))), nil
}

// servers lists the MCP servers the viewer can read, with the same check and
// route the server's dashboard page uses.
func servers(ctx context.Context, queries *repo.Queries, organizationID string, grants []authz.Grant, links homeLinks, serverURL *url.URL) ([]HomeServer, error) {
	rows, err := queries.ListHomeServers(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list servers: %w", err)
	}
	out := make([]HomeServer, 0, homeListLimit)
	for _, row := range rows {
		if len(out) == homeListLimit {
			break
		}
		resourceID := row.ID.String()
		if row.ToolsetID.Valid {
			resourceID = row.ToolsetID.UUID.String()
		}
		allowed, err := authz.GrantsAuthorize(grants, authz.MCPCheck(authz.ScopeMCPRead, resourceID, row.ProjectID.String()))
		if err != nil {
			return nil, fmt.Errorf("authorize server: %w", err)
		}
		if !allowed {
			continue
		}
		logoURL := ""
		if row.LogoID.Valid && serverURL != nil {
			logoURL = assets.ServeImageURL(serverURL, row.LogoID.UUID)
		}
		out = append(out, HomeServer{
			Name:        row.Name,
			ProjectName: row.ProjectName,
			URL:         links.project(row.ProjectSlug, nil, "mcp", mcpservers.DashboardRouteParam(row.Slug, row.ID)),
			LogoURL:     logoURL,
		})
	}
	return out, nil
}

// homeLinks builds dashboard links into the viewer's organization.
type homeLinks struct {
	base    *url.URL
	orgSlug string
}

func (l homeLinks) project(projectSlug string, query url.Values, segments ...string) string {
	if l.base == nil || l.orgSlug == "" || projectSlug == "" {
		return ""
	}
	link := l.base.JoinPath(append([]string{l.orgSlug, "projects", projectSlug}, segments...)...)
	if len(query) > 0 {
		link.RawQuery = query.Encode()
	}
	return link.String()
}

func parseDashboardURL(raw string) *url.URL {
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil
	}
	return parsed
}
