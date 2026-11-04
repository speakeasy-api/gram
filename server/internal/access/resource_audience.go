package access

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/plugins/audience"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins/roledelivery"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
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

// The levels whose scope keeps this page readable: manage implies view.
var audienceViewLevels = []string{audienceLevelView, audienceLevelManage}

// Agent restrictions use the same server-local exclusions as other principals.
// They constrain live parent policy and are carried into delegated credentials.
var agentAudienceLevels = []string{audienceLevelUse, audienceLevelView, audienceLevelManage, audienceLevelBlocked, audienceLevelBlockedView, audienceLevelBlockedManage}

// Widest first: a principal holding several scopes is reported at its highest
// level, and its own block outranks its own grants.
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
	return s.roleMgr.resourceProjectID(ctx, organizationID, resourceID)
}

func (r *RoleManager) resourceProjectID(ctx context.Context, organizationID, resourceID string) (string, error) {
	parsed, err := uuid.Parse(resourceID)
	if err != nil {
		return "", oops.E(oops.CodeBadRequest, err, "resource id is not a valid identifier")
	}
	projectID, err := accessrepo.New(r.db).FindMCPResourceProject(ctx, accessrepo.FindMCPResourceProjectParams{
		OrganizationID: organizationID,
		ResourceID:     parsed,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", oops.E(oops.CodeNotFound, err, "server not found")
	case err != nil:
		return "", oops.E(oops.CodeUnexpected, err, "resolve resource project").LogError(ctx, r.logger)
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
	// Organization admins can always read the rules they are allowed to write
	// (SetResourceAudience requires only org:admin). Without this, a block that
	// reaches an admin through user:all or one of their roles would hide the
	// server's rules from them, and the organization-wide assignment review
	// could never load.
	if err := s.authz.RequireAny(ctx,
		authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil},
		authz.MCPCheck(authz.ScopeMCPRead, payload.ResourceID, projectID),
	); err != nil {
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

	entries, version, err := s.roleMgr.resourceAudienceEntries(ctx, ac.ActiveOrganizationID, payload.ResourceID, projectID)
	if err != nil {
		return nil, err
	}
	connectAudience, err := s.roleMgr.resourceConnectAudience(ctx, ac.ActiveOrganizationID, payload.ResourceID, projectID)
	if err != nil {
		return nil, err
	}

	principals := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Kind == "role" {
			principals = append(principals, entry.PrincipalUrn)
		}
	}
	matches, err := plugins.RolePluginsForResource(ctx, s.db, s.authz, ac.ActiveOrganizationID, uuid.MustParse(projectID), uuid.MustParse(payload.ResourceID), principals)
	if err != nil {
		return nil, fmt.Errorf("read audience role plugins: %w", err)
	}
	var rolePlugins []*gen.ResourceAudienceRolePlugin
	if matches != nil {
		rolePlugins = make([]*gen.ResourceAudienceRolePlugin, 0, len(matches))
	}
	for _, match := range matches {
		rolePlugins = append(rolePlugins, &gen.ResourceAudienceRolePlugin{PrincipalUrn: match.PrincipalUrn, PluginID: match.PluginID.String(), Name: match.Name, Slug: match.Slug})
	}
	return &gen.ResourceAudienceResult{Entries: entries, Version: version, RolePlugins: rolePlugins, ConnectAudience: connectAudience}, nil
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
	return s.roleMgr.saveResourceAudience(ctx, ac.ActiveOrganizationID, RoleAuditActor{
		Principal:   urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		DisplayName: ac.Email,
	}, payload.ResourceID, payload.Entries, payload.ExpectedVersion)
}

// saveResourceAudience replaces the rules naming one resource, with every
// guard the audience editor relies on: principal validation, the agent
// retention rules, both lockout guards, optimistic concurrency, role delivery
// and an audit entry committed with the change. Callers must already have
// checked that the actor administers the organization.
func (r *RoleManager) saveResourceAudience(ctx context.Context, organizationID string, actor RoleAuditActor, resourceID string, payloadEntries []*gen.SetResourceAudienceEntry, expectedVersion string) (*gen.ResourceAudienceResult, error) {
	if actor.Principal.Type != urn.PrincipalTypeUser || actor.Principal.ID == "" {
		return nil, oops.E(oops.CodeUnexpected, nil, "resource audience actor must be a user").LogError(ctx, r.logger)
	}
	actorUserID := actor.Principal.ID
	// The resource has to exist in this organization before grants naming it
	// are written, and the project it belongs to decides which organization-
	// wide rules the response reports.
	projectID, err := r.resourceProjectID(ctx, organizationID, resourceID)
	if err != nil {
		return nil, err
	}

	// A suspended or revoked agent keeps the access it already has but must not
	// be given more, and ValidatePrincipal only rejects deleted agents. The
	// rules already stored for this resource are therefore the exception set:
	// re-saving the audience may retain them, but no new one may be added.
	retainableAgents, err := r.storedAgentRules(ctx, organizationID, resourceID, projectID)
	if err != nil {
		return nil, err
	}
	assignableAgents, err := r.assignableAgentPrincipals(ctx, organizationID)
	if err != nil {
		return nil, err
	}

	if err := preserveUnassignableAgentBlocks(retainableAgents, assignableAgents, payloadEntries); err != nil {
		return nil, err
	}

	principalsByLevel := make(map[string][]authz.PrincipalSelectors, len(audienceLevelScopes))
	seen := make(map[string]struct{}, len(payloadEntries))
	for _, entry := range payloadEntries {
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
		if principal.Type == urn.PrincipalTypeAgent {
			if !slices.Contains(agentAudienceLevels, entry.Level) {
				return nil, oops.E(oops.CodeInvalid, nil, "agents cannot be given %q access; remove the agent's rule instead", entry.Level)
			}
			if _, assignable := assignableAgents[principal.String()]; !assignable && !strings.HasPrefix(entry.Level, "blocked") {
				// A suspended or revoked agent may keep the access it already
				// has, which has to mean the identical rule: matching on the
				// principal alone would let a tool-limited grant be rewritten
				// as an unrestricted one, widening what the agent gets back
				// when it resumes. Removing a grant is always allowed, since
				// that is just leaving it out of the payload.
				rules, known := retainableAgents[principal.String()]
				if !known {
					return nil, oops.E(oops.CodeInvalid, nil, "agent %q is suspended or revoked and cannot be given access", entry.PrincipalUrn)
				}
				if _, unchanged := rules[audienceRuleKey(entry.Level, entry.Tools, entry.Dispositions)]; !unchanged {
					return nil, oops.E(oops.CodeInvalid, nil, "agent %q is suspended or revoked; its access can be kept as it is or removed, but not changed", entry.PrincipalUrn)
				}
			}
		}
		// A principal may appear once per level — "connect to the server" and
		// "never connect to its destructive tools" are different rules, and
		// the second is the only way this surface can subtract — but not twice
		// at the same level.
		if _, duplicate := seen[principal.String()+"|"+entry.Level]; duplicate {
			return nil, oops.E(oops.CodeInvalid, nil, "principal %q is listed more than once at the same level", entry.PrincipalUrn)
		}
		seen[principal.String()+"|"+entry.Level] = struct{}{}
		if err := authz.ValidatePrincipal(ctx, r.db, organizationID, principal); err != nil {
			if errors.Is(err, authz.ErrPrincipalInvalid) || errors.Is(err, authz.ErrPrincipalNotFound) {
				return nil, oops.E(oops.CodeInvalid, err, "unknown principal %q", entry.PrincipalUrn)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "validate principal").LogError(ctx, r.logger)
		}
		selectors, err := audienceSelectors(audienceLevelScopes[entry.Level], resourceID, entry.Tools, entry.Dispositions)
		if err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid narrowing for %q", entry.PrincipalUrn)
		}
		principalsByLevel[entry.Level] = append(principalsByLevel[entry.Level], authz.PrincipalSelectors{
			Principal: principal,
			Selectors: selectors,
		})
	}

	// Lockout guardrail: the administrator role is what undoes a rule written
	// here, so a view block naming it takes this page away from everyone who
	// could give it back. The caller guard below does not catch this — the
	// person restricting a server is rarely an administrator themselves.
	if err := r.rejectAdminRoleBlocks(ctx, organizationID, principalsByLevel); err != nil {
		return nil, err
	}

	// Lockout guardrail: a block on view subtracts the read that renders this
	// page, so writing one against an audience the caller is part of would
	// take away their own ability to undo it. Blocks on connect and manage
	// leave the page readable, so neither needs a guard. A caller who keeps
	// their own view or manage rule on this resource is not locked out by a
	// role or everyone block, because a direct grant naming the resource
	// outranks both for the page's server-level read, even when narrowed;
	// their own block still applies.
	if slices.ContainsFunc(audienceLockoutLevels, func(level string) bool {
		return len(principalsByLevel[level]) > 0
	}) {
		callerPrincipals, err := authz.ResolveUserPrincipals(ctx, r.db, organizationID, actorUserID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "resolve caller principals").LogError(ctx, r.logger)
		}
		callerURN := urn.NewPrincipal(urn.PrincipalTypeUser, actorUserID).String()
		keepsOwnView := slices.ContainsFunc(audienceViewLevels, func(level string) bool {
			return slices.ContainsFunc(principalsByLevel[level], func(entry authz.PrincipalSelectors) bool {
				return entry.Principal.String() == callerURN
			})
		})
		held := make(map[string]struct{}, len(callerPrincipals))
		for _, principal := range callerPrincipals {
			held[principal.String()] = struct{}{}
		}
		for _, level := range audienceLockoutLevels {
			for _, entry := range principalsByLevel[level] {
				if _, ok := held[entry.Principal.String()]; !ok {
					continue
				}
				if keepsOwnView && entry.Principal.String() != callerURN {
					continue
				}
				return nil, oops.E(oops.CodeInvalid, nil, "you cannot block your own access to this resource")
			}
		}
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin resource audience transaction").LogError(ctx, r.logger)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Optimistic concurrency: a save replaces every rule naming the resource,
	// so an edit made against a list someone else has since changed is a
	// conflict, not a silent overwrite. The lock makes the check and the
	// replacement one step: two saves holding the same version cannot both
	// pass it and then take turns writing.
	if err := accessrepo.New(tx).LockResourceAudience(ctx, accessrepo.LockResourceAudienceParams{
		OrganizationID: organizationID,
		ResourceID:     resourceID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock resource audience").LogError(ctx, r.logger)
	}
	// Capture both removed and proposed roles before replacing any level. Lock
	// role rows before delivery acquires project admission and plugin locks,
	// matching the role editor's ordering.
	roles := map[string]struct{}{}
	for level, scope := range audienceLevelScopes {
		grants, err := authz.ListGrantsForResource(ctx, tx, authz.Resource{
			OrganizationID: organizationID,
			Scope:          scope,
			ResourceID:     resourceID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list resource roles").LogError(ctx, r.logger)
		}
		for _, grant := range grants {
			if strings.HasPrefix(grant.PrincipalUrn, "role:") {
				roles[grant.PrincipalUrn] = struct{}{}
			}
		}
		for _, entry := range principalsByLevel[level] {
			if entry.Principal.Type == urn.PrincipalTypeRole {
				roles[entry.Principal.String()] = struct{}{}
			}
		}
	}
	roleURNs := make([]string, 0, len(roles))
	for role := range roles {
		roleURNs = append(roleURNs, role)
	}
	sort.Strings(roleURNs)
	for _, role := range roleURNs {
		roleID, err := uuid.Parse(role[strings.LastIndex(role, ":")+1:])
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "parse resource role ID").LogError(ctx, r.logger)
		}
		if _, err := accessrepo.New(tx).LockOrganizationRoleByID(ctx, accessrepo.LockOrganizationRoleByIDParams{
			OrganizationID: organizationID, ID: roleID,
		}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeUnexpected, err, "lock resource role").LogError(ctx, r.logger)
		}
	}
	current, err := r.audienceFingerprint(ctx, tx, organizationID, resourceID)
	if err != nil {
		return nil, err
	}
	if current != expectedVersion {
		return nil, oops.E(oops.CodeFailedPrecondition, nil, "access for this server changed while you were editing; reload and try again")
	}

	rulesBefore, err := audienceRulesSnapshot(ctx, tx, organizationID, resourceID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "snapshot resource audience").LogError(ctx, r.logger)
	}

	before := make(map[string][]authz.Grant, len(roleURNs))
	for _, role := range roleURNs {
		grants, err := roledelivery.Snapshot(ctx, tx, organizationID, role)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "snapshot role delivery grants").LogError(ctx, r.logger)
		}
		before[role] = grants
	}

	// Every level is rewritten, including the ones nobody was given, so a
	// principal moved from "manage" to "use" does not keep its old rule. The
	// narrower exclusion scopes are left alone: the role editor writes them as
	// rule exceptions, and this form cannot show one, so clearing them here
	// would delete another surface's work during an unrelated edit.
	for level, scope := range audienceLevelScopes {
		if err := authz.ReplaceResourceAudience(ctx, tx, authz.Resource{
			OrganizationID: organizationID,
			Scope:          scope,
			ResourceID:     resourceID,
		}, principalsByLevel[level]); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "replace resource audience").LogError(ctx, r.logger)
		}
	}

	// Lock the complete project union before delivering any role. Role order
	// need not match project order, and RoleChanged holds locks until commit.
	if err := lockAudienceRoleDeliveryProjects(ctx, tx, organizationID, roleURNs); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock audience role delivery projects").LogError(ctx, r.logger)
	}

	changedProjects := map[uuid.UUID]struct{}{}
	for _, role := range roleURNs {
		projects, err := roledelivery.RoleChanged(ctx, tx, organizationID, role, before[role], r.roleDeliveryGuard)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "update role audience plugin servers").LogError(ctx, r.logger)
		}
		for _, projectID := range projects {
			changedProjects[projectID] = struct{}{}
		}
	}
	for projectID := range changedProjects {
		if err := r.roleDeliveryPublication.Project(ctx, tx, organizationID, projectID, actorUserID); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "request role delivery publication").LogError(ctx, r.logger)
		}
	}

	// The audit entry commits with the rules it describes. A save that
	// changes nothing records nothing.
	rulesAfter, err := audienceRulesSnapshot(ctx, tx, organizationID, resourceID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "snapshot resource audience").LogError(ctx, r.logger)
	}
	if !slices.EqualFunc(rulesBefore, rulesAfter, sameAudienceRule) {
		if r.audit == nil {
			return nil, oops.E(oops.CodeUnexpected, nil, "resource audience audit logger is not configured").LogError(ctx, r.logger)
		}
		if err := r.audit.LogAccessResourceAudienceUpdate(ctx, tx, audit.LogAccessResourceAudienceUpdateEvent{
			OrganizationID:         organizationID,
			ProjectID:              uuid.MustParse(projectID),
			Actor:                  actor.Principal,
			ActorDisplayName:       actor.DisplayName,
			ActorSlug:              nil,
			ResourceID:             resourceID,
			AudienceSnapshotBefore: rulesBefore,
			AudienceSnapshotAfter:  rulesAfter,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log resource audience update").LogError(ctx, r.logger)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit resource audience").LogError(ctx, r.logger)
	}
	r.signalRoleDeliveryPublish(ctx, slices.Collect(maps.Keys(changedProjects)), actorUserID)

	entries, version, err := r.resourceAudienceEntries(ctx, organizationID, resourceID, projectID)
	if err != nil {
		return nil, err
	}

	connectAudience, err := r.resourceConnectAudience(ctx, organizationID, resourceID, projectID)
	if err != nil {
		return nil, err
	}

	return &gen.ResourceAudienceResult{Entries: entries, Version: version, RolePlugins: nil, ConnectAudience: connectAudience}, nil
}

// audienceRulesSnapshot reads the rules naming one resource in the shape a
// save takes them, one per principal and level, for the audit log.
func audienceRulesSnapshot(ctx context.Context, db accessrepo.DBTX, organizationID, resourceID string) ([]*gen.SetResourceAudienceEntry, error) {
	type ruleKey struct{ principalURN, level string }
	rules := make(map[ruleKey]*gen.SetResourceAudienceEntry)
	unnarrowed := make(map[ruleKey]bool)
	for level, scope := range audienceLevelScopes {
		grants, err := authz.ListGrantsForResource(ctx, db, authz.Resource{
			OrganizationID: organizationID,
			Scope:          scope,
			ResourceID:     resourceID,
		})
		if err != nil {
			return nil, fmt.Errorf("list %s grants: %w", level, err)
		}
		for _, grant := range grants {
			key := ruleKey{principalURN: grant.PrincipalUrn, level: level}
			rule, ok := rules[key]
			if !ok {
				rule = &gen.SetResourceAudienceEntry{PrincipalUrn: grant.PrincipalUrn, Level: level, Tools: nil, Dispositions: nil}
				rules[key] = rule
			}
			tool, disposition := grant.Selector[authz.SelectorKeyTool], grant.Selector[authz.SelectorKeyDisposition]
			switch {
			case unnarrowed[key]:
			case tool != "":
				rule.Tools = appendUnique(rule.Tools, tool)
			case disposition != "":
				rule.Dispositions = appendUnique(rule.Dispositions, disposition)
			default:
				unnarrowed[key] = true
				rule.Tools, rule.Dispositions = nil, nil
			}
		}
	}

	snapshot := make([]*gen.SetResourceAudienceEntry, 0, len(rules))
	for _, rule := range rules {
		slices.Sort(rule.Tools)
		slices.Sort(rule.Dispositions)
		snapshot = append(snapshot, rule)
	}
	slices.SortFunc(snapshot, func(a, b *gen.SetResourceAudienceEntry) int {
		if c := strings.Compare(a.PrincipalUrn, b.PrincipalUrn); c != 0 {
			return c
		}
		return audienceLevelRank(a.Level) - audienceLevelRank(b.Level)
	})
	return snapshot, nil
}

func sameAudienceRule(a, b *gen.SetResourceAudienceEntry) bool {
	return a.PrincipalUrn == b.PrincipalUrn && a.Level == b.Level && slices.Equal(a.Tools, b.Tools) && slices.Equal(a.Dispositions, b.Dispositions)
}

// lockAudienceRoleDeliveryProjects follows RoleChanged's project ordering across
// the entire audience edit, rather than acquiring each role's projects in turn.
// The caller must already hold all affected role locks.
func lockAudienceRoleDeliveryProjects(ctx context.Context, tx pgx.Tx, organizationID string, roles []string) error {
	projects := map[uuid.UUID]struct{}{}
	for _, role := range roles {
		plugins, err := pluginsrepo.New(tx).ListRoleDeliveryPlugins(ctx, pluginsrepo.ListRoleDeliveryPluginsParams{
			OrganizationID: organizationID, PrincipalUrn: role,
		})
		if err != nil {
			return fmt.Errorf("list role delivery plugins: %w", err)
		}
		for _, plugin := range plugins {
			projects[plugin.ProjectID] = struct{}{}
		}
	}
	projectIDs := make([]uuid.UUID, 0, len(projects))
	for projectID := range projects {
		projectIDs = append(projectIDs, projectID)
	}
	slices.SortFunc(projectIDs, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	for _, projectID := range projectIDs {
		if err := admission.LockProject(ctx, tx, projectID); err != nil {
			return fmt.Errorf("lock role delivery project admission: %w", err)
		}
	}
	return nil
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

	// Suspended and revoked agents keep the access they already have but cannot
	// be given more, so the picker lists only the assignable ones.
	agents, err := accessrepo.New(s.db).ListAssignableAgents(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list assignable agents").LogError(ctx, s.logger)
	}
	for _, agent := range agents {
		options = append(options, &gen.AudienceOption{
			PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()).String(),
			Kind:         "agent",
			DisplayName:  agent.Name,
			Description:  audienceDescription("agent", nil),
			MemberCount:  nil,
		})
	}

	return &gen.ListAudienceOptionsResult{Options: options}, nil
}

// resourceAudienceEntries reads every grant that decides access to one
// resource and renders it with a name a person can recognize.
func (r *RoleManager) resourceAudienceEntries(ctx context.Context, organizationID, resourceID, projectID string) ([]*gen.ResourceAudienceEntry, string, error) {
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
		grants, err := authz.ListGrantsForResource(ctx, r.db, authz.Resource{
			OrganizationID: organizationID,
			Scope:          scope,
			ResourceID:     resourceID,
		})
		if err != nil {
			return nil, "", oops.E(oops.CodeUnexpected, err, "list resource grants").LogError(ctx, r.logger)
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
		wildcards, err := authz.ListGrantsForResource(ctx, r.db, authz.Resource{
			OrganizationID: organizationID,
			Scope:          scope,
			ResourceID:     authz.WildcardResource,
		})
		if err != nil {
			return nil, "", oops.E(oops.CodeUnexpected, err, "list organization grants").LogError(ctx, r.logger)
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

	names, err := r.audienceNames(ctx, organizationID)
	if err != nil {
		return nil, "", err
	}

	reach, err := r.audienceReach(ctx, organizationID)
	if err != nil {
		return nil, "", err
	}

	agentReach, err := r.agentAudienceReach(ctx, organizationID)
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
			AgentIds:     agentReach[key.principalURN],
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

// rejectAdminRoleBlocks refuses a save that would block the administrator
// role's view of this resource. A role block outranks every grant except one
// made to a person by name for this resource, so blocking the administrator
// role's read locks administrators out of the page that could undo it. Blocks
// on connect and manage leave the page readable, so administrators can still
// be taken off a server's use or management. Roles other than admin are left
// alone: taking a team off a server is the point of this surface.
func (r *RoleManager) rejectAdminRoleBlocks(ctx context.Context, organizationID string, principalsByLevel map[string][]authz.PrincipalSelectors) error {
	blocked := make(map[string]struct{})
	for _, level := range audienceLockoutLevels {
		for _, entry := range principalsByLevel[level] {
			if entry.Principal.Type == urn.PrincipalTypeRole {
				blocked[entry.Principal.String()] = struct{}{}
			}
		}
	}
	if len(blocked) == 0 {
		return nil
	}

	roles, err := accessrepo.New(r.db).ListActiveOrganizationRoles(ctx, organizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list roles for lockout guard").LogError(ctx, r.logger)
	}
	for _, role := range roles {
		if role.WorkosSlug != authz.SystemRoleAdmin {
			continue
		}
		if _, ok := blocked[role.RoleUrn]; ok {
			return oops.E(oops.CodeInvalid, nil, "blocking the %s role from viewing this server would take its access page away from every administrator, including the ones who could give it back; block its use or management instead", role.WorkosName)
		}
	}
	return nil
}

// agentAudienceReach maps every principal an audience row can name to the
// agents it currently reaches. It is the agent counterpart to audienceReach:
// a rule naming a role authorizes that role's agent members at runtime, so a
// surface that resolved only people would report no access while the role
// still granted it.
func (r *RoleManager) agentAudienceReach(ctx context.Context, organizationID string) (map[string][]string, error) {
	assignments, err := accessrepo.New(r.db).ListAgentRoleAssignments(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list agent role assignments for audience reach").LogError(ctx, r.logger)
	}

	reach := make(map[string][]string, len(assignments))
	for _, assignment := range assignments {
		reach[assignment.PrincipalUrn] = append(reach[assignment.PrincipalUrn], assignment.AgentID.String())
	}

	// An agent principal reaches itself, so a rule naming one reports its own
	// agent the way a rule naming a person reports that person.
	agents, err := accessrepo.New(r.db).ListAgentNames(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list agents for audience reach").LogError(ctx, r.logger)
	}
	for _, agent := range agents {
		principal := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()).String()
		reach[principal] = append(reach[principal], agent.ID.String())
	}

	return reach, nil
}

// storedAgentRules maps each agent principal this resource's own rules name to
// the exact rules it holds. Retaining access for a suspended or revoked agent
// is checked against these rather than against the principal alone, so the
// rule has to come back unchanged.
//
// Only rules naming this resource count: the payload replaces those and leaves
// organization-wide rules to the surface that owns them.
func (r *RoleManager) storedAgentRules(ctx context.Context, organizationID, resourceID, projectID string) (map[string]map[string]struct{}, error) {
	entries, _, err := r.resourceAudienceEntries(ctx, organizationID, resourceID, projectID)
	if err != nil {
		return nil, err
	}

	stored := make(map[string]map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.Kind != "agent" || entry.AppliesTo != audienceAppliesToResource {
			continue
		}
		if stored[entry.PrincipalUrn] == nil {
			stored[entry.PrincipalUrn] = make(map[string]struct{}, 1)
		}
		stored[entry.PrincipalUrn][audienceRuleKey(entry.Level, entry.Tools, entry.Dispositions)] = struct{}{}
	}
	return stored, nil
}

// assignableAgentPrincipals returns the agents that may be given new access.
func (r *RoleManager) assignableAgentPrincipals(ctx context.Context, organizationID string) (map[string]struct{}, error) {
	agents, err := accessrepo.New(r.db).ListAssignableAgents(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list assignable agents").LogError(ctx, r.logger)
	}

	assignable := make(map[string]struct{}, len(agents))
	for _, agent := range agents {
		assignable[urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()).String()] = struct{}{}
	}
	return assignable, nil
}

// audienceReach maps every principal an audience row can name to the
// organization members it currently reaches, so the surface can answer "who
// can actually use this server" rather than "what rules exist".
func (r *RoleManager) audienceReach(ctx context.Context, organizationID string) (map[string][]string, error) {
	members, err := r.ListMembers(ctx, organizationID)
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

	roles, err := accessrepo.New(r.db).ListActiveOrganizationRoles(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list roles for audience reach").LogError(ctx, r.logger)
	}
	roleURNByID := make(map[string]string, len(roles))
	for _, role := range roles {
		roleURNByID[role.ID.String()] = role.RoleUrn
	}
	for _, member := range members.Members {
		for _, roleID := range memberRoleIDs(member) {
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
func (r *RoleManager) audienceNames(ctx context.Context, organizationID string) (map[string]audienceName, error) {
	names := make(map[string]audienceName)

	audiences, err := audience.Resolve(ctx, r.db, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve audiences").LogError(ctx, r.logger)
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

	members, err := r.ListMembers(ctx, organizationID)
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

	// Every non-deleted agent, not only the assignable ones: a suspended or
	// revoked agent keeps the rules it holds, and rendering it as deleted
	// would misreport access that is still enforced.
	agents, err := accessrepo.New(r.db).ListAgentNames(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list agents for audience names").LogError(ctx, r.logger)
	}
	for _, agent := range agents {
		names[urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()).String()] = audienceName{
			kind:        "agent",
			displayName: agent.Name,
			description: audienceDescription("agent", nil),
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
	case strings.HasPrefix(principalURN, string(urn.PrincipalTypeAgent)+":"):
		return audienceName{kind: "agent", displayName: "Deleted agent", description: nil, memberCount: nil}
	default:
		return audienceName{kind: "unknown", displayName: principalURN, description: nil, memberCount: nil}
	}
}

func audienceDescription(kind string, memberCount *int64) *string {
	switch kind {
	case "everyone":
		return conv.PtrEmpty("All members of this organization")
	case "agent":
		return conv.PtrEmpty("Agent")
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
func (r *RoleManager) audienceFingerprint(ctx context.Context, db accessrepo.DBTX, organizationID, resourceID string) (string, error) {
	var rows []string
	for _, scope := range audienceLevelScopes {
		grants, err := authz.ListGrantsForResource(ctx, db, authz.Resource{
			OrganizationID: organizationID,
			Scope:          scope,
			ResourceID:     resourceID,
		})
		if err != nil {
			return "", oops.E(oops.CodeUnexpected, err, "list resource grants").LogError(ctx, r.logger)
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
	return principal, nil
}

// Connect decisions a resource's connect audience reports for each member.
const (
	connectDecisionAllowed          = "allowed"
	connectDecisionAllowedOverBlock = "allowed_over_block"
	connectDecisionBlocked          = "blocked"
)

// resourceConnectAudience reports who can connect to one resource once every
// rule is applied. Each member's decision comes from authz.ExplainGrantCheck
// over that member's own live grants (direct, role, everyone and directory
// mapped), so blocks and the direct grants that outrank them are decided
// exactly as the MCP endpoint decides them. A gateway has no connect check of
// its own and reports nil.
func (r *RoleManager) resourceConnectAudience(ctx context.Context, organizationID, resourceID, projectID string) (*gen.ResourceConnectAudience, error) {
	parsed, err := uuid.Parse(resourceID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "resource id is not a valid identifier")
	}
	resource, err := accessrepo.New(r.db).FindMCPResourceVisibility(ctx, accessrepo.FindMCPResourceVisibilityParams{
		OrganizationID: organizationID,
		ResourceID:     parsed,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "server not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "resolve resource visibility").LogError(ctx, r.logger)
	}
	if resource.ResourceType == explainedResourceGateway {
		return nil, nil
	}

	members, err := r.ListMembers(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	userIDs := make([]string, 0, len(members.Members))
	for _, member := range members.Members {
		userIDs = append(userIDs, member.ID)
	}
	grantsByUser, err := authz.LoadUserGrants(ctx, r.db, organizationID, userIDs)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load member grants for connect audience").LogError(ctx, r.logger)
	}

	check := authz.MCPCheck(authz.ScopeMCPConnect, resourceID, projectID)
	audienceMembers := make([]*gen.ResourceConnectAudienceMember, 0, len(userIDs))
	for _, userID := range userIDs {
		member, err := connectAudienceMember(userID, grantsByUser[userID], check)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "evaluate member connect access").LogError(ctx, r.logger)
		}
		if member != nil {
			audienceMembers = append(audienceMembers, member)
		}
	}
	slices.SortFunc(audienceMembers, func(a, b *gen.ResourceConnectAudienceMember) int {
		return strings.Compare(a.UserID, b.UserID)
	})

	return &gen.ResourceConnectAudience{Visibility: resource.Visibility, Members: audienceMembers}, nil
}

// connectAudienceMember decides one member's connect access. A member no
// allow or block reaches is left out: on a private resource they cannot
// connect, and no rule on this page is why.
func connectAudienceMember(userID string, grants []authz.Grant, check authz.Check) (*gen.ResourceConnectAudienceMember, error) {
	explanation, err := authz.ExplainGrantCheck(grants, check)
	if err != nil {
		return nil, fmt.Errorf("explain connect check: %w", err)
	}
	hasEffect := func(effect authz.GrantEffect) bool {
		return slices.ContainsFunc(explanation.Contributions, func(contribution authz.GrantContribution) bool {
			return contribution.Effect == effect
		})
	}

	if !explanation.Allowed {
		if !hasEffect(authz.GrantEffectBlocks) {
			return nil, nil
		}
		return &gen.ResourceConnectAudienceMember{UserID: userID, Decision: connectDecisionBlocked, ToolAccess: explainedToolAccessNone}, nil
	}

	toolAccess, err := effectiveToolAccess(grants, check, explanation)
	if err != nil {
		return nil, err
	}
	decision := connectDecisionAllowed
	if hasEffect(authz.GrantEffectOverrides) {
		decision = connectDecisionAllowedOverBlock
	}
	return &gen.ResourceConnectAudienceMember{UserID: userID, Decision: decision, ToolAccess: toolAccess}, nil
}
