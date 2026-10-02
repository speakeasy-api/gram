package access

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	gen "github.com/speakeasy-api/gram/server/gen/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"go.opentelemetry.io/otel/trace"
)

const (
	// explainedLevelAll names a rule held through a scope covering every
	// permission rather than one of the audience levels.
	explainedLevelAll = "all"

	// explainedAppliesToProject names a rule covering every resource in the
	// resource's project.
	explainedAppliesToProject = "project"

	// explainedEffectLimits names a tool- or annotation-scoped block that takes
	// some of the resource's tools away without blocking the resource.
	explainedEffectLimits = "limits"

	// explainedResourceGateway is the resource type FindMCPResourceVisibility
	// reports for a gateway (meta server).
	explainedResourceGateway = "gateway"

	explainedToolAccessAll  = "all"
	explainedToolAccessSome = "some"
	explainedToolAccessNone = "none"

	// explainedProbeTool stands in for a tool no rule names when probing an
	// annotation-scoped rule, so the probe is a tool call like any real one.
	explainedProbeTool = "gram:explain-access:other-tool"
)

// The levels an explanation covers, in display order, and the scope each
// checks. They are the same checks the MCP endpoint and dashboard enforce.
var explainedAccessLevels = []struct {
	level string
	scope authz.Scope
}{
	{level: audienceLevelUse, scope: authz.ScopeMCPConnect},
	{level: audienceLevelView, scope: authz.ScopeMCPRead},
	{level: audienceLevelManage, scope: authz.ScopeMCPWrite},
}

// Deciding rules first: what proves or takes away the access, then what only
// narrows it, then what lost.
var explainedEffectOrder = []string{
	string(authz.GrantEffectOverrides),
	string(authz.GrantEffectBlocks),
	string(authz.GrantEffectAllows),
	explainedEffectLimits,
	string(authz.GrantEffectOverridden),
	string(authz.GrantEffectBlocked),
}

// ExplainResourceAccess reports whether one member can use, view, and manage
// one resource, and every rule that decides it. Each decision comes from
// authz.ExplainGrantCheck over the member's own grants, so it is the decision
// runtime enforcement makes.
func (s *Service) ExplainResourceAccess(ctx context.Context, payload *gen.ExplainResourceAccessPayload) (*gen.ExplainResourceAccessResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}
	projectID, err := s.resourceProjectID(ctx, ac.ActiveOrganizationID, payload.ResourceID)
	if err != nil {
		return nil, err
	}
	// The same gate as the resource's audience: the explanation names the
	// rules reaching a member, which that surface already shows.
	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, payload.ResourceID, projectID)); err != nil {
		return nil, err
	}
	if err := s.authz.Require(ctx, authz.Check{
		Scope:        authz.ScopeOrgRead,
		ResourceKind: "",
		ResourceID:   ac.ActiveOrganizationID,
		Dimensions:   nil,
	}); err != nil {
		return nil, err
	}

	logger := s.logger.With(
		attr.SlogOrganizationID(ac.ActiveOrganizationID),
		attr.SlogUserID(ac.UserID),
		attr.SlogAccessMemberID(payload.UserID),
	)
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
		attr.AccessMemberID(payload.UserID),
	)

	// Checked before resolving: for a non-member the resolver answers with the
	// everyone principal alone, which would explain a stranger's access as
	// everyone's.
	isMember, err := orgrepo.New(s.db).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{
		UserID:         payload.UserID,
		OrganizationID: ac.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check organization membership").LogError(ctx, logger)
	}
	if !isMember {
		return nil, oops.E(oops.CodeNotFound, nil, "user not found in this organization").LogWarn(ctx, logger)
	}

	resourceID, err := uuid.Parse(payload.ResourceID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "resource id is not a valid identifier").LogWarn(ctx, logger)
	}
	resource, err := accessrepo.New(s.db).FindMCPResourceVisibility(ctx, accessrepo.FindMCPResourceVisibilityParams{
		OrganizationID: ac.ActiveOrganizationID,
		ResourceID:     resourceID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "server not found").LogWarn(ctx, logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "resolve resource visibility").LogError(ctx, logger)
	}
	// Nothing checks connect on a gateway's own id: each server it fronts is
	// checked for the member instead, so an answer for the gateway id would
	// not be the one runtime gives.
	if resource.ResourceType == explainedResourceGateway {
		return nil, oops.E(oops.CodeBadRequest, nil, "a gateway's access is decided by each server it fronts; check those servers instead").LogWarn(ctx, logger)
	}

	// The same principals and grants the engine loads for this member's own
	// requests, including roles reached through directory role mappings.
	principals, err := authz.ResolveUserPrincipals(ctx, s.db, ac.ActiveOrganizationID, payload.UserID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve member principals").LogError(ctx, logger)
	}
	grants, err := authz.LoadGrants(ctx, s.db, ac.ActiveOrganizationID, principals)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load member grants").LogError(ctx, logger)
	}

	sources, err := s.explainedRoleSources(ctx, ac.ActiveOrganizationID, payload.UserID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve member role sources").LogError(ctx, logger)
	}
	// Directory attribute values can carry personal data, so which mapping
	// produced a role is shown to the same audience as the mappings. A legacy
	// API key is never checked against grants, so it would pass any scope
	// check; like the mappings themselves, the sources stay out of its reach.
	showSources, err := s.authz.Evaluate(ctx, authz.Check{
		Scope:        authz.ScopeOrgAdmin,
		ResourceKind: "",
		ResourceID:   ac.ActiveOrganizationID,
		Dimensions:   nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "evaluate directory mapping visibility").LogError(ctx, logger)
	}
	if mode, byKey := contextvalues.APIKeyAuthorization(ctx); byKey && mode != contextvalues.APIKeyAuthorizationModePrincipal {
		showSources = false
	}
	if !showSources {
		for principalURN, source := range sources {
			source.mappings = nil
			sources[principalURN] = source
		}
	}

	names, err := s.audienceNames(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}

	describe := explainedRuleDescriber{
		resourceID: payload.ResourceID,
		projectID:  projectID,
		names:      names,
		sources:    sources,
	}
	result := &gen.ExplainResourceAccessResult{
		Visibility: resource.Visibility,
		Levels:     make([]*gen.ExplainedAccessLevel, 0, len(explainedAccessLevels)),
	}
	for _, level := range explainedAccessLevels {
		explained, err := explainAccessLevel(grants, level.level, authz.MCPCheck(level.scope, payload.ResourceID, projectID), describe)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "explain access").LogError(ctx, logger)
		}
		result.Levels = append(result.Levels, explained)
	}
	return result, nil
}

// explainAccessLevel explains one level. For use it also reports the tool and
// annotation rules that narrow which tools the member can call.
func explainAccessLevel(grants []authz.Grant, level string, check authz.Check, describe explainedRuleDescriber) (*gen.ExplainedAccessLevel, error) {
	explanation, err := authz.ExplainGrantCheck(grants, check)
	if err != nil {
		return nil, fmt.Errorf("explain %s: %w", level, err)
	}

	byGrant := make(map[string]*gen.ExplainedAccessRule)
	rules := make([]*gen.ExplainedAccessRule, 0, len(explanation.Contributions))
	narrowed := true
	for _, contribution := range explanation.Contributions {
		rule := describe.rule(contribution.Grant, string(contribution.Effect), contribution.Reason)
		byGrant[explainedGrantKey(contribution.Grant)] = rule
		rules = append(rules, rule)
		if contribution.Effect == authz.GrantEffectAllows || contribution.Effect == authz.GrantEffectOverrides {
			narrowed = narrowed && isNarrowedGrant(contribution.Grant)
		}
	}

	result := &gen.ExplainedAccessLevel{
		Level:      level,
		Allowed:    explanation.Allowed,
		ToolAccess: nil,
		Rules:      rules,
	}
	if level != audienceLevelUse {
		sortExplainedRules(result.Rules)
		return result, nil
	}

	// Tool and annotation blocks never match a check for the whole resource;
	// they apply to tool calls. Probe one tool call per narrowing a rule names,
	// and one for any other tool, so a block is reported where it takes tools
	// away. That includes a block on the whole resource that a narrowed direct
	// grant outranks only for the tools it names.
	limited := false
	for _, probe := range toolProbes(grants, check) {
		probed, err := authz.ExplainGrantCheck(grants, probe)
		if err != nil {
			return nil, fmt.Errorf("explain tool access: %w", err)
		}
		for _, contribution := range probed.Contributions {
			key := explainedGrantKey(contribution.Grant)
			existing := byGrant[key]
			switch contribution.Effect {
			case authz.GrantEffectBlocks:
				limited = true
				switch {
				case existing == nil:
					rule := describe.rule(contribution.Grant, explainedEffectLimits, authz.BlockedReasonNone)
					byGrant[key] = rule
					result.Rules = append(result.Rules, rule)
				case existing.Effect == string(authz.GrantEffectOverridden):
					// Overridden for some tools but still taking others away:
					// what the block still does is the part worth reading.
					existing.Effect = explainedEffectLimits
				}
			case authz.GrantEffectOverridden:
				if existing == nil {
					rule := describe.rule(contribution.Grant, string(authz.GrantEffectOverridden), authz.BlockedReasonNone)
					byGrant[key] = rule
					result.Rules = append(result.Rules, rule)
				}
			default:
				continue
			}
		}
	}

	toolAccess := explainedToolAccessAll
	switch {
	case !explanation.Allowed:
		toolAccess = explainedToolAccessNone
	case limited || narrowed:
		toolAccess = explainedToolAccessSome
	}
	result.ToolAccess = &toolAccess
	sortExplainedRules(result.Rules)
	return result, nil
}

// toolProbes returns one tool call check per distinct tool or annotation
// narrowing among grants. A rule naming only an annotation is probed with a
// tool name no rule names, standing for any other tool carrying it.
func toolProbes(grants []authz.Grant, check authz.Check) []authz.Check {
	named := make(map[string]struct{})
	for _, grant := range grants {
		if tool := grant.Selector[authz.SelectorKeyTool]; tool != "" {
			named[tool] = struct{}{}
		}
	}
	otherTool := explainedProbeTool
	for {
		if _, ok := named[otherTool]; !ok {
			break
		}
		otherTool += "'"
	}

	type probeKey struct {
		tool        string
		disposition string
	}
	seen := make(map[probeKey]struct{})
	var probes []authz.Check
	for _, grant := range grants {
		if !isNarrowedGrant(grant) {
			continue
		}
		key := probeKey{tool: grant.Selector[authz.SelectorKeyTool], disposition: grant.Selector[authz.SelectorKeyDisposition]}
		if key.tool == "" || key.tool == authz.WildcardResource {
			key.tool = otherTool
		}
		if key.disposition == authz.WildcardResource {
			key.disposition = ""
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		probes = append(probes, authz.MCPToolCallCheck(check.ResourceID, authz.MCPToolCallDimensions{
			Tool:        key.tool,
			Disposition: key.disposition,
			ProjectID:   check.Dimensions[authz.SelectorKeyProjectID],
		}))
	}
	// Any other tool, carrying no annotation a rule names: what a narrowed
	// grant leaves out.
	if len(probes) > 0 {
		if _, ok := seen[probeKey{tool: otherTool, disposition: ""}]; !ok {
			probes = append(probes, authz.MCPToolCallCheck(check.ResourceID, authz.MCPToolCallDimensions{
				Tool:        otherTool,
				Disposition: "",
				ProjectID:   check.Dimensions[authz.SelectorKeyProjectID],
			}))
		}
	}
	return probes
}

// isNarrowedGrant reports whether grant covers only some of a resource's tools.
// A "*" value matches every tool, so it narrows nothing.
func isNarrowedGrant(grant authz.Grant) bool {
	narrows := func(value string) bool { return value != "" && value != authz.WildcardResource }
	return narrows(grant.Selector[authz.SelectorKeyTool]) || narrows(grant.Selector[authz.SelectorKeyDisposition])
}

// explainedGrantKey identifies one loaded grant across several explanations.
func explainedGrantKey(grant authz.Grant) string {
	keys := slices.Sorted(maps.Keys(grant.Selector))
	var b strings.Builder
	b.WriteString(grant.PrincipalUrn)
	b.WriteByte('|')
	b.WriteString(string(grant.Scope))
	for _, key := range keys {
		fmt.Fprintf(&b, "|%s=%s", key, grant.Selector[key])
	}
	return b.String()
}

func sortExplainedRules(rules []*gen.ExplainedAccessRule) {
	sort.SliceStable(rules, func(i, j int) bool {
		left, right := slices.Index(explainedEffectOrder, rules[i].Effect), slices.Index(explainedEffectOrder, rules[j].Effect)
		if left != right {
			return left < right
		}
		return rules[i].DisplayName < rules[j].DisplayName
	})
}

// explainedRoleSource records how a member holds one role.
type explainedRoleSource struct {
	// mappedOnly is set when the member holds the role only through directory
	// role mappings.
	mappedOnly bool

	// mappings are the directory role mappings giving the member the role,
	// including when they also hold it directly: taking the role away means
	// undoing every source.
	mappings []*gen.ExplainedAccessDirectorySource
}

// explainedRoleSources reports, per role principal reached through directory
// role mappings, whether the member holds it only that way and which mappings
// give it to them.
func (s *Service) explainedRoleSources(ctx context.Context, organizationID, userID string) (map[string]explainedRoleSource, error) {
	repo := accessrepo.New(s.db)
	roles, err := repo.ListUserRolePrincipals(ctx, accessrepo.ListUserRolePrincipalsParams{
		OrganizationID: organizationID,
		UserID:         userID,
	})
	if err != nil {
		return nil, fmt.Errorf("list member roles: %w", err)
	}
	// Direct assignments come first, so a mapped role already seen directly is
	// held both ways.
	direct := make(map[string]struct{})
	sources := make(map[string]explainedRoleSource)
	for _, role := range roles {
		if !role.FromDirectoryMapping {
			direct[role.PrincipalUrn] = struct{}{}
			continue
		}
		_, alsoDirect := direct[role.PrincipalUrn]
		sources[role.PrincipalUrn] = explainedRoleSource{mappedOnly: !alsoDirect, mappings: nil}
	}

	mappings, err := repo.ListUserDirectoryRoleMappingSources(ctx, accessrepo.ListUserDirectoryRoleMappingSourcesParams{
		OrganizationID: organizationID,
		UserID:         userID,
	})
	if err != nil {
		return nil, fmt.Errorf("list member directory role mappings: %w", err)
	}
	for _, mapping := range mappings {
		source, ok := sources[mapping.RoleUrn]
		if !ok {
			continue
		}
		source.mappings = append(source.mappings, &gen.ExplainedAccessDirectorySource{
			SourceKind:         mapping.SourceKind,
			DirectoryGroupName: conv.FromPGText[string](mapping.DirectoryGroupName),
			AttributeKey:       conv.FromPGText[string](mapping.AttributeKey),
			AttributeValue:     conv.FromPGText[string](mapping.AttributeValue),
		})
		sources[mapping.RoleUrn] = source
	}
	return sources, nil
}

// explainedRuleDescriber renders loaded grants as rules a person can read.
type explainedRuleDescriber struct {
	resourceID string
	projectID  string
	names      map[string]audienceName
	sources    map[string]explainedRoleSource
}

func (d explainedRuleDescriber) rule(grant authz.Grant, effect string, reason authz.BlockedReason) *gen.ExplainedAccessRule {
	name, ok := d.names[grant.PrincipalUrn]
	if !ok {
		name = describeUnknownPrincipal(grant.PrincipalUrn)
	}
	kind := name.kind
	if kind != "everyone" && kind != "role" && kind != "user" {
		kind = "unknown"
	}

	level := explainedLevelAll
	for audienceLevel, scope := range audienceLevelScopes {
		if scope == grant.Scope {
			level = audienceLevel
			break
		}
	}

	appliesTo := audienceAppliesToAllResources
	switch {
	case grant.Selector.ResourceID() == d.resourceID:
		appliesTo = audienceAppliesToResource
	case grant.Selector[authz.SelectorKeyProjectID] == d.projectID:
		appliesTo = explainedAppliesToProject
	}

	var tools, dispositions []string
	if tool := grant.Selector[authz.SelectorKeyTool]; tool != "" {
		tools = []string{tool}
	}
	if disposition := grant.Selector[authz.SelectorKeyDisposition]; disposition != "" {
		dispositions = []string{disposition}
	}

	var reasonValue *string
	if reason != authz.BlockedReasonNone {
		reasonValue = new(string(reason))
	}

	source := d.sources[grant.PrincipalUrn]
	return &gen.ExplainedAccessRule{
		PrincipalUrn:        grant.PrincipalUrn,
		Kind:                kind,
		DisplayName:         name.displayName,
		Level:               level,
		AppliesTo:           appliesTo,
		Tools:               tools,
		Dispositions:        dispositions,
		Effect:              effect,
		Reason:              reasonValue,
		ViaDirectoryMapping: source.mappedOnly,
		DirectorySources:    source.mappings,
	}
}
