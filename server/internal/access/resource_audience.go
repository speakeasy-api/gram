package access

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	gen "github.com/speakeasy-api/gram/server/gen/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins/audience"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"go.opentelemetry.io/otel/trace"
)

// The access levels this surface exposes, and the scope each one is stored
// as. The "blocked_" levels are not levels but their absence: each writes the
// exclusion scope for exactly one grant. Per authz/scopes.go the mcp:blocked_*
// scopes are independent of one another, so "cannot connect" leaves view and
// manage alone — connecting to a server and administering it are different
// jobs. Taking a principal off a server entirely writes all three.
//
// This is how a rule covering every server is narrowed to one: the grant stays
// where the role editor wrote it, and the block names this server alone.
const (
	audienceLevelUse           = "use"
	audienceLevelView          = "view"
	audienceLevelManage        = "manage"
	audienceLevelBlocked       = "blocked"
	audienceLevelBlockedView   = "blocked_view"
	audienceLevelBlockedManage = "blocked_manage"

	audienceAppliesToResource     = "resource"
	audienceAppliesToAllResources = "all_resources"
)

var audienceLevelScopes = map[string]authz.Scope{
	audienceLevelUse:           authz.ScopeMCPConnect,
	audienceLevelView:          authz.ScopeMCPRead,
	audienceLevelManage:        authz.ScopeMCPWrite,
	audienceLevelBlocked:       authz.ScopeMCPBlockedConnect,
	audienceLevelBlockedView:   authz.ScopeMCPBlockedRead,
	audienceLevelBlockedManage: authz.ScopeMCPBlockedWrite,
}

// The block that takes away the read rendering this page, so a caller cannot
// write one against themselves and lose the means to undo it. Blocking
// connect or manage leaves the page readable, so neither needs a guard.
var audienceLockoutLevels = []string{audienceLevelBlockedView}

// Every block level. Blocking the administrator role is guarded at all three:
// the caller guard above only protects whoever is writing, and an administrator
// is not usually the one restricting a server. Blocking the role that exists to
// undo such a rule leaves nobody who can, so it is refused whichever capability
// it names.
var audienceBlockLevels = []string{
	audienceLevelBlocked,
	audienceLevelBlockedView,
	audienceLevelBlockedManage,
}

// Widest first: a principal holding several scopes is reported at its highest
// level, and a block outranks every grant.
var audienceLevelOrder = []string{
	audienceLevelBlocked,
	audienceLevelBlockedView,
	audienceLevelBlockedManage,
	audienceLevelManage,
	audienceLevelView,
	audienceLevelUse,
}

// resourceProjectID resolves the project owning one MCP resource. A
// project-scoped grant must be checked against the resource's own project:
// without it, a grant naming one project would authorize every server in the
// organization.
func (s *Service) resourceProjectID(ctx context.Context, organizationID, resourceID string) (string, error) {
	parsed, err := uuid.Parse(resourceID)
	if err != nil {
		return "", oops.E(oops.CodeBadRequest, err, "resource id is not a valid identifier")
	}
	projectID, err := accessrepo.New(s.db).FindMCPResourceProject(ctx, accessrepo.FindMCPResourceProjectParams{
		OrganizationID: organizationID,
		ResourceID:     parsed,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", oops.E(oops.CodeNotFound, err, "server not found")
	case err != nil:
		return "", oops.E(oops.CodeUnexpected, err, "resolve resource project").LogError(ctx, s.logger)
	}
	return projectID.String(), nil
}

// ListResourceAudience reports every rule that decides access to one resource:
// the rules naming it, and the organization-wide rules it inherits.
func (s *Service) ListResourceAudience(ctx context.Context, payload *gen.ListResourceAudiencePayload) (*gen.ResourceAudienceResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}
	projectID, err := s.resourceProjectID(ctx, ac.ActiveOrganizationID, payload.ResourceID)
	if err != nil {
		return nil, err
	}
	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, payload.ResourceID, projectID)); err != nil {
		return nil, err
	}
	// The entries name the people each rule reaches, so seeing them takes
	// organization read as well as access to the server itself.
	if err := s.authz.Require(ctx, authz.Check{
		Scope:        authz.ScopeOrgRead,
		ResourceKind: "",
		ResourceID:   ac.ActiveOrganizationID,
		Dimensions:   nil,
	}); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)

	entries, version, err := s.resourceAudienceEntries(ctx, ac.ActiveOrganizationID, payload.ResourceID, projectID)
	if err != nil {
		return nil, err
	}

	return &gen.ResourceAudienceResult{Entries: entries, Version: version}, nil
}

// SetResourceAudience replaces the rules that name one resource. Rules that
// cover every resource are owned by the organization-wide access surface and
// are never written here, so the two surfaces cannot overwrite each other.
func (s *Service) SetResourceAudience(ctx context.Context, payload *gen.SetResourceAudiencePayload) (*gen.ResourceAudienceResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}
	if err := s.authz.Require(ctx, authz.Check{
		Scope:        authz.ScopeOrgAdmin,
		ResourceKind: "",
		ResourceID:   ac.ActiveOrganizationID,
		Dimensions:   nil,
	}); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
	)
	// The resource has to exist in this organization before grants naming it
	// are written, and the project it belongs to decides which organization-
	// wide rules the response reports.
	projectID, err := s.resourceProjectID(ctx, ac.ActiveOrganizationID, payload.ResourceID)
	if err != nil {
		return nil, err
	}

	principalsByLevel := make(map[string][]authz.PrincipalSelectors, len(audienceLevelScopes))
	seen := make(map[string]struct{}, len(payload.Entries))
	for _, entry := range payload.Entries {
		if entry == nil {
			continue
		}
		if _, ok := audienceLevelScopes[entry.Level]; !ok {
			return nil, oops.E(oops.CodeInvalid, nil, "unknown access level %q", entry.Level)
		}
		principal, err := parseAudiencePrincipal(entry.PrincipalUrn)
		if err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid principal %q", entry.PrincipalUrn)
		}
		// A principal may appear once per level — "connect to the server" and
		// "never connect to its destructive tools" are different rules, and
		// the second is the only way this surface can subtract — but not twice
		// at the same level.
		if _, duplicate := seen[principal.String()+"|"+entry.Level]; duplicate {
			return nil, oops.E(oops.CodeInvalid, nil, "principal %q is listed more than once at the same level", entry.PrincipalUrn)
		}
		seen[principal.String()+"|"+entry.Level] = struct{}{}
		if err := authz.ValidatePrincipal(ctx, s.db, ac.ActiveOrganizationID, principal); err != nil {
			if errors.Is(err, authz.ErrPrincipalInvalid) || errors.Is(err, authz.ErrPrincipalNotFound) {
				return nil, oops.E(oops.CodeInvalid, err, "unknown principal %q", entry.PrincipalUrn)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "validate principal").LogError(ctx, s.logger)
		}
		selectors, err := audienceSelectors(audienceLevelScopes[entry.Level], payload.ResourceID, entry.Tools, entry.Dispositions)
		if err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid narrowing for %q", entry.PrincipalUrn)
		}
		principalsByLevel[entry.Level] = append(principalsByLevel[entry.Level], authz.PrincipalSelectors{
			Principal: principal,
			Selectors: selectors,
		})
	}

	// Lockout guardrail: the administrator role is what undoes a rule written
	// here, so a block naming it takes the server's access away from everyone
	// who could give it back. The caller guard below does not catch this — the
	// person restricting a server is rarely an administrator themselves.
	if err := s.rejectAdminRoleBlocks(ctx, ac.ActiveOrganizationID, principalsByLevel); err != nil {
		return nil, err
	}

	// Lockout guardrail: a block on view subtracts the read that renders this
	// page, so writing one against an audience the caller is part of would
	// take away their own ability to undo it. Blocks on connect and manage
	// leave the page readable, so neither needs a guard.
	if slices.ContainsFunc(audienceLockoutLevels, func(level string) bool {
		return len(principalsByLevel[level]) > 0
	}) {
		callerPrincipals, err := authz.ResolveUserPrincipals(ctx, s.db, ac.ActiveOrganizationID, ac.UserID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "resolve caller principals").LogError(ctx, s.logger)
		}
		held := make(map[string]struct{}, len(callerPrincipals))
		for _, principal := range callerPrincipals {
			held[principal.String()] = struct{}{}
		}
		for _, level := range audienceLockoutLevels {
			for _, entry := range principalsByLevel[level] {
				if _, ok := held[entry.Principal.String()]; ok {
					return nil, oops.E(oops.CodeInvalid, nil, "you cannot block your own access to this resource")
				}
			}
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin resource audience transaction").LogError(ctx, s.logger)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Optimistic concurrency: a save replaces every rule naming the resource,
	// so an edit made against a list someone else has since changed is a
	// conflict, not a silent overwrite. The lock makes the check and the
	// replacement one step: two saves holding the same version cannot both
	// pass it and then take turns writing.
	if err := accessrepo.New(tx).LockResourceAudience(ctx, accessrepo.LockResourceAudienceParams{
		OrganizationID: ac.ActiveOrganizationID,
		ResourceID:     payload.ResourceID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock resource audience").LogError(ctx, s.logger)
	}
	current, err := s.audienceFingerprint(ctx, tx, ac.ActiveOrganizationID, payload.ResourceID)
	if err != nil {
		return nil, err
	}
	if current != payload.ExpectedVersion {
		return nil, oops.E(oops.CodeFailedPrecondition, nil, "access for this server changed while you were editing; reload and try again")
	}

	// Every level is rewritten, including the ones nobody was given, so a
	// principal moved from "manage" to "use" does not keep its old rule. The
	// narrower exclusion scopes are left alone: the role editor writes them as
	// rule exceptions, and this form cannot show one, so clearing them here
	// would delete another surface's work during an unrelated edit.
	for level, scope := range audienceLevelScopes {
		if err := authz.ReplaceResourceAudience(ctx, tx, authz.Resource{
			OrganizationID: ac.ActiveOrganizationID,
			Scope:          scope,
			ResourceID:     payload.ResourceID,
		}, principalsByLevel[level]); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "replace resource audience").LogError(ctx, s.logger)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit resource audience").LogError(ctx, s.logger)
	}

	entries, version, err := s.resourceAudienceEntries(ctx, ac.ActiveOrganizationID, payload.ResourceID, projectID)
	if err != nil {
		return nil, err
	}

	return &gen.ResourceAudienceResult{Entries: entries, Version: version}, nil
}

// ListAudienceOptions lists the principals an administrator can give access
// to: everyone, the organization's roles, and its members.
func (s *Service) ListAudienceOptions(ctx context.Context, _ *gen.ListAudienceOptionsPayload) (*gen.ListAudienceOptionsResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}
	if err := s.authz.Require(ctx, authz.Check{
		Scope:        authz.ScopeOrgRead,
		ResourceKind: "",
		ResourceID:   ac.ActiveOrganizationID,
		Dimensions:   nil,
	}); err != nil {
		return nil, err
	}

	audiences, err := audience.Resolve(ctx, s.db, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list audience options").LogError(ctx, s.logger)
	}

	options := make([]*gen.AudienceOption, 0, len(audiences))
	for _, item := range audiences {
		// Directory groups and attribute values are resolved by the audience
		// resolver but not yet granted here: they need principal resolution on
		// the request path, which is its own change.
		if item.Kind != "everyone" && item.Kind != "role" {
			continue
		}
		options = append(options, &gen.AudienceOption{
			PrincipalUrn: item.PrincipalURN,
			Kind:         item.Kind,
			DisplayName:  item.DisplayName,
			Description:  audienceDescription(item.Kind, item.MemberCount),
			MemberCount:  item.MemberCount,
		})
	}

	members, err := s.roleMgr.ListMembers(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}
	for _, member := range members.Members {
		options = append(options, &gen.AudienceOption{
			PrincipalUrn: member.PrincipalUrn,
			Kind:         "user",
			DisplayName:  member.Name,
			Description:  conv.PtrEmpty(member.Email),
			MemberCount:  nil,
		})
	}

	return &gen.ListAudienceOptionsResult{Options: options}, nil
}

// resourceAudienceEntries reads every grant that decides access to one
// resource and renders it with a name a person can recognize.
func (s *Service) resourceAudienceEntries(ctx context.Context, organizationID, resourceID, projectID string) ([]*gen.ResourceAudienceEntry, string, error) {
	// Rules are keyed by principal, reach *and* level. A principal can hold an
	// organization-wide grant and a rule naming this server at once, and can
	// hold several levels at once — "connect to the server" alongside "never
	// connect to its destructive tools". Collapsing either would describe
	// access nobody was given, and saving that description back would grant
	// or remove it.
	type ruleKey struct {
		principalURN string
		appliesTo    string
		level        string
	}
	type rule struct {
		level        string
		tools        []string
		dispositions []string
		// Some grant covers the whole resource, so the narrowings of the
		// others say nothing extra.
		unnarrowed bool
	}

	var fingerprint []string
	rules := make(map[ruleKey]rule)
	record := func(principalURN string, level string, appliesTo string, tool string, disposition string) {
		key := ruleKey{principalURN: principalURN, appliesTo: appliesTo, level: level}
		next := rules[key]
		next.level = level

		// Within one level, reach is the union of its grants, and one
		// unnarrowed grant makes the row unnarrowed.
		switch {
		case tool != "":
			next.tools = appendUnique(next.tools, tool)
		case disposition != "":
			next.dispositions = appendUnique(next.dispositions, disposition)
		default:
			next.unnarrowed = true
		}
		if next.unnarrowed {
			next.tools = nil
			next.dispositions = nil
		}

		rules[key] = next
	}

	for level, scope := range audienceLevelScopes {
		grants, err := authz.ListGrantsForResource(ctx, s.db, authz.Resource{
			OrganizationID: organizationID,
			Scope:          scope,
			ResourceID:     resourceID,
		})
		if err != nil {
			return nil, "", oops.E(oops.CodeUnexpected, err, "list resource grants").LogError(ctx, s.logger)
		}
		for _, grant := range grants {
			record(grant.PrincipalUrn, level, audienceAppliesToResource, grant.Selector[authz.SelectorKeyTool], grant.Selector[authz.SelectorKeyDisposition])
			// The version covers exactly what a save replaces: the rules
			// naming this resource, and nothing the other surface owns.
			fingerprint = append(fingerprint, fmt.Sprintf("%s|%s|%s|%s",
				grant.PrincipalUrn, scope,
				grant.Selector[authz.SelectorKeyTool],
				grant.Selector[authz.SelectorKeyDisposition],
			))
		}

		// Organization-wide rules are reported alongside them so the surface
		// can say what this server inherits without pretending to own it.
		wildcards, err := authz.ListGrantsForResource(ctx, s.db, authz.Resource{
			OrganizationID: organizationID,
			Scope:          scope,
			ResourceID:     authz.WildcardResource,
		})
		if err != nil {
			return nil, "", oops.E(oops.CodeUnexpected, err, "list organization grants").LogError(ctx, s.logger)
		}
		for _, grant := range wildcards {
			// A wildcard grant can still be confined to one project, in which
			// case it does not reach this server and is not "every server"
			// either. Leave it out rather than overstate it.
			if scoped := grant.Selector[authz.SelectorKeyProjectID]; scoped != "" && scoped != projectID {
				continue
			}
			record(grant.PrincipalUrn, level, audienceAppliesToAllResources, grant.Selector[authz.SelectorKeyTool], grant.Selector[authz.SelectorKeyDisposition])
		}
	}

	names, err := s.audienceNames(ctx, organizationID)
	if err != nil {
		return nil, "", err
	}

	reach, err := s.audienceReach(ctx, organizationID)
	if err != nil {
		return nil, "", err
	}

	entries := make([]*gen.ResourceAudienceEntry, 0, len(rules))
	for key, rule := range rules {
		name := names[key.principalURN]
		if name.displayName == "" {
			name = describeUnknownPrincipal(key.principalURN)
		}
		entries = append(entries, &gen.ResourceAudienceEntry{
			PrincipalUrn: key.principalURN,
			Kind:         name.kind,
			DisplayName:  name.displayName,
			Description:  name.description,
			MemberCount:  name.memberCount,
			Level:        rule.level,
			AppliesTo:    key.appliesTo,
			Tools:        rule.tools,
			Dispositions: rule.dispositions,
			MemberIds:    reach[key.principalURN],
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		left, right := entries[i], entries[j]
		if left.AppliesTo != right.AppliesTo {
			return left.AppliesTo == audienceAppliesToAllResources
		}
		if left.Level != right.Level {
			return audienceLevelRank(left.Level) < audienceLevelRank(right.Level)
		}
		return left.DisplayName < right.DisplayName
	})

	return entries, audienceVersion(fingerprint), nil
}

// audienceSelectors turns a narrowing into the selectors it stores: one per
// tool and one per disposition, all naming this resource. A rule with no
// narrowing stores nothing here and falls back to the resource selector.
func audienceSelectors(scope authz.Scope, resourceID string, tools, dispositions []string) ([]authz.Selector, error) {
	if len(tools) == 0 && len(dispositions) == 0 {
		return nil, nil
	}
	if len(tools) > 0 && len(dispositions) > 0 {
		// The two are alternatives: a rule reads either "these tools" or
		// "tools annotated this way", never a silent intersection of both.
		return nil, errors.New("choose either tools or annotations, not both")
	}

	selectors := make([]authz.Selector, 0, len(tools)+len(dispositions))
	for _, tool := range tools {
		selector := authz.NewSelector(scope, resourceID)
		selector[authz.SelectorKeyTool] = tool
		selectors = append(selectors, selector)
	}
	for _, disposition := range dispositions {
		selector := authz.NewSelector(scope, resourceID)
		selector[authz.SelectorKeyDisposition] = disposition
		selectors = append(selectors, selector)
	}

	return selectors, nil
}

// rejectAdminRoleBlocks refuses a save that would block the administrator role
// on this resource. Restricting a server to one team is normally written as
// "everyone else: no access", which stores a block — and a block outranks every
// grant, so naming the administrator role there locks every administrator out
// of the page that could undo it. Roles other than admin are left alone: taking
// a team off a server is the point of this surface.
func (s *Service) rejectAdminRoleBlocks(ctx context.Context, organizationID string, principalsByLevel map[string][]authz.PrincipalSelectors) error {
	blocked := make(map[string]struct{})
	for _, level := range audienceBlockLevels {
		for _, entry := range principalsByLevel[level] {
			if entry.Principal.Type == urn.PrincipalTypeRole {
				blocked[entry.Principal.String()] = struct{}{}
			}
		}
	}
	if len(blocked) == 0 {
		return nil
	}

	roles, err := accessrepo.New(s.db).ListActiveOrganizationRoles(ctx, organizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list roles for lockout guard").LogError(ctx, s.logger)
	}
	for _, role := range roles {
		if role.WorkosSlug != authz.SystemRoleAdmin {
			continue
		}
		if _, ok := blocked[role.RoleUrn]; ok {
			return oops.E(oops.CodeInvalid, nil, "blocking the %s role would take this server away from every administrator, including the ones who could give it back; remove its access instead of blocking it", role.WorkosName)
		}
	}
	return nil
}

// audienceReach maps every principal an audience row can name to the
// organization members it currently reaches, so the surface can answer "who
// can actually use this server" rather than "what rules exist".
func (s *Service) audienceReach(ctx context.Context, organizationID string) (map[string][]string, error) {
	members, err := s.roleMgr.ListMembers(ctx, organizationID)
	if err != nil {
		return nil, err
	}

	reach := make(map[string][]string)
	everyone := make([]string, 0, len(members.Members))
	for _, member := range members.Members {
		everyone = append(everyone, member.ID)
		reach[member.PrincipalUrn] = []string{member.ID}
	}
	reach[urn.PrincipalWildcard] = everyone
	reach[authz.AllUsersPrincipal().String()] = everyone

	roles, err := accessrepo.New(s.db).ListActiveOrganizationRoles(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list roles for audience reach").LogError(ctx, s.logger)
	}
	roleURNByID := make(map[string]string, len(roles))
	for _, role := range roles {
		roleURNByID[role.ID.String()] = role.RoleUrn
	}
	for _, member := range members.Members {
		for _, roleID := range member.RoleIds {
			roleURN, ok := roleURNByID[roleID]
			if !ok {
				continue
			}
			reach[roleURN] = append(reach[roleURN], member.ID)
		}
	}

	return reach, nil
}

type audienceName struct {
	kind        string
	displayName string
	description *string
	memberCount *int64
}

// audienceNames resolves every principal an audience row can name. Roles,
// directory groups and directory attributes come from the same resolver the
// plugin assignment surface uses, so both places name an audience identically.
func (s *Service) audienceNames(ctx context.Context, organizationID string) (map[string]audienceName, error) {
	names := make(map[string]audienceName)

	audiences, err := audience.Resolve(ctx, s.db, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve audiences").LogError(ctx, s.logger)
	}
	for _, item := range audiences {
		if item.Kind != "everyone" && item.Kind != "role" {
			continue
		}
		names[item.PrincipalURN] = audienceName{
			kind:        item.Kind,
			displayName: item.DisplayName,
			description: audienceDescription(item.Kind, item.MemberCount),
			memberCount: item.MemberCount,
		}
	}

	members, err := s.roleMgr.ListMembers(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	for _, member := range members.Members {
		names[member.PrincipalUrn] = audienceName{
			kind:        "user",
			displayName: member.Name,
			description: conv.PtrEmpty(member.Email),
			memberCount: nil,
		}
	}

	names[urn.PrincipalWildcard] = audienceName{
		kind:        "everyone",
		displayName: "Everyone",
		description: conv.PtrEmpty("All members of this organization"),
		memberCount: nil,
	}
	names[authz.AllUsersPrincipal().String()] = names[urn.PrincipalWildcard]

	return names, nil
}

// describeUnknownPrincipal keeps a rule visible when the thing it names is
// gone — a deleted role, or a directory group that stopped syncing. Hiding it
// would hide access that is still enforced.
func describeUnknownPrincipal(principalURN string) audienceName {
	switch {
	case strings.HasPrefix(principalURN, string(urn.PrincipalTypeRole)+":"):
		return audienceName{kind: "role", displayName: "Deleted role", description: nil, memberCount: nil}
	case strings.HasPrefix(principalURN, string(urn.PrincipalTypeUser)+":"):
		return audienceName{kind: "user", displayName: "Former member", description: nil, memberCount: nil}
	default:
		return audienceName{kind: "unknown", displayName: principalURN, description: nil, memberCount: nil}
	}
}

func audienceDescription(kind string, memberCount *int64) *string {
	switch kind {
	case "everyone":
		return conv.PtrEmpty("All members of this organization")
	case "role":
		if memberCount == nil {
			return conv.PtrEmpty("Role")
		}
		return conv.PtrEmpty(pluralMembers(*memberCount))
	case "directory_group":
		if memberCount == nil {
			return conv.PtrEmpty("Directory group")
		}
		return conv.PtrEmpty(pluralMembers(*memberCount))
	case "directory_attribute":
		if memberCount == nil {
			return conv.PtrEmpty("Directory attribute")
		}
		return conv.PtrEmpty(pluralMembers(*memberCount))
	default:
		return nil
	}
}

func pluralMembers(count int64) string {
	if count == 1 {
		return "1 member"
	}
	return fmt.Sprintf("%d members", count)
}

// audienceFingerprint reads the rules a save replaces and fingerprints them.
// Taken inside the save's transaction, it is the version the write is checked
// against; taken outside, it is the version a read hands to the client.
func (s *Service) audienceFingerprint(ctx context.Context, db accessrepo.DBTX, organizationID, resourceID string) (string, error) {
	var rows []string
	for _, scope := range audienceLevelScopes {
		grants, err := authz.ListGrantsForResource(ctx, db, authz.Resource{
			OrganizationID: organizationID,
			Scope:          scope,
			ResourceID:     resourceID,
		})
		if err != nil {
			return "", oops.E(oops.CodeUnexpected, err, "list resource grants").LogError(ctx, s.logger)
		}
		for _, grant := range grants {
			rows = append(rows, fmt.Sprintf("%s|%s|%s|%s",
				grant.PrincipalUrn, scope,
				grant.Selector[authz.SelectorKeyTool],
				grant.Selector[authz.SelectorKeyDisposition],
			))
		}
	}
	return audienceVersion(rows), nil
}

// audienceVersion fingerprints the rules a save replaces, so an edit made
// against a stale list is a conflict rather than a silent overwrite.
func audienceVersion(rows []string) string {
	sort.Strings(rows)
	sum := sha256.Sum256([]byte(strings.Join(rows, "\n")))
	return hex.EncodeToString(sum[:8])
}

func appendUnique(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

func audienceLevelRank(level string) int {
	for rank, known := range audienceLevelOrder {
		if known == level {
			return rank
		}
	}
	return len(audienceLevelOrder)
}

// parseAudiencePrincipal accepts the wildcard alongside typed principals, so
// "everyone" is a first-class choice in the picker rather than a special case
// the caller has to encode.
func parseAudiencePrincipal(value string) (urn.Principal, error) {
	if value == urn.PrincipalWildcard {
		return authz.AllUsersPrincipal(), nil
	}
	principal, err := urn.ParsePrincipal(value)
	if err != nil {
		return urn.Principal{}, fmt.Errorf("parse principal: %w", err)
	}
	if principal.Type == urn.PrincipalTypeAgent {
		return urn.Principal{}, errors.New("agent principals cannot be given resource access here")
	}
	return principal, nil
}
