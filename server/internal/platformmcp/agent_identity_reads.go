package platformmcp

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/gen/types"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authclients"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// maxAgentAccessTargets bounds the servers and toolsets one access read
	// matches the agent's grants against. Organizations hold far fewer; past
	// it the result says it was cut off rather than reading without bound.
	maxAgentAccessTargets = 1000

	// maxAgentChecksPerOutcome is the most denied, and separately the most
	// allowed, checks one read groups. It is the largest page listChallenges
	// serves, so each outcome is one read.
	maxAgentChecksPerOutcome = 200

	// defaultAgentChecksWindow is the period read when the caller names none:
	// the same week the dashboard's identity pages open on.
	defaultAgentChecksWindow = 7 * 24 * time.Hour

	// maxAgentChecksWindow matches how long checks are kept.
	maxAgentChecksWindow = 90 * 24 * time.Hour
)

// agentConnectScopes are the agent policy scopes that let it connect to an MCP
// server: mcp:connect, and mcp:read and mcp:write, which include it.
var agentConnectScopes = []authz.Scope{authz.ScopeMCPConnect, authz.ScopeMCPRead, authz.ScopeMCPWrite}

// hiddenAgentCheckScopes are left out of an agent's checks, from the groups
// and from the totals. An agent records one org:hooks_ingest check for every
// hook event it sends, which buries the checks that say what it may reach.
var hiddenAgentCheckScopes = []string{string(authz.ScopeOrgHooksIngest)}

var (
	// ErrAgentNotAvailable covers an unknown agent and one the caller may not
	// inspect alike, so a refusal does not reveal that an agent exists.
	ErrAgentNotAvailable = errors.New("platform mcp agent not available")

	// ErrAgentChecksForbidden is a caller who may read the agent but not list
	// authorization checks, which needs org:read.
	ErrAgentChecksForbidden = errors.New("platform mcp agent checks forbidden")

	// ErrAgentChecksWindowInvalid is a malformed or out-of-range period.
	ErrAgentChecksWindowInvalid = errors.New("platform mcp agent checks window invalid")
)

// Credential modes an agent signs in to an MCP server with.
const (
	// agentCredentialShared: the server asks no person to sign in, so the agent
	// uses the server's shared credential, or none, and acts for no one.
	agentCredentialShared = "shared"

	// agentCredentialOnBehalfOf: the agent signs in with the assigned people's
	// own connections and acts for them.
	agentCredentialOnBehalfOf = "on_behalf_of"

	// agentCredentialNotAssigned: the server asks each person to sign in, and
	// no one's working connection is assigned to the agent there.
	agentCredentialNotAssigned = "not_assigned"

	// agentCredentialCallerProvided: the server passes through the credential
	// the agent's own MCP client sends, so Speakeasy holds none for it.
	agentCredentialCallerProvided = "caller_provided"
)

// How an mcp:connect grant reaches a server, narrowest first.
const (
	agentGrantServer       = "server"
	agentGrantProject      = "project"
	agentGrantOrganization = "organization"
)

// AgentAuthorizationChecks is the dashboard's authorization check listing. It
// keeps the org:read check, the organization member filter and the
// exclude_scopes semantics in one place.
type AgentAuthorizationChecks interface {
	ListChallenges(context.Context, *accessgen.ListChallengesPayload) (*accessgen.ListChallengesResult, error)
}

// AgentIdentityReadService backs the agent identity read tools.
type AgentIdentityReadService struct {
	db     *pgxpool.Pool
	engine *authz.Engine
	checks AgentAuthorizationChecks
	budget OperationBudget
	now    func() time.Time
}

// NewAgentIdentityReadService reads agent policy and connection assignments
// from Postgres and authorization checks through checks.
func NewAgentIdentityReadService(db *pgxpool.Pool, engine *authz.Engine, checks AgentAuthorizationChecks, budget OperationBudget) *AgentIdentityReadService {
	return &AgentIdentityReadService{db: db, engine: engine, checks: checks, budget: budget, now: time.Now}
}

func (s *AgentIdentityReadService) valid() bool {
	return s != nil && s.db != nil && s.engine != nil && s.checks != nil && s.budget.valid() && s.now != nil
}

// AgentIdentityInput names one agent.
type AgentIdentityInput struct {
	AgentID string `json:"agent_id" jsonschema:"exact agent UUID, as list_recent_tool_calls returns it or the agent's dashboard page shows it"`
}

// AgentIdentity summarizes the agent a read is about.
type AgentIdentity struct {
	// ID is the agent's UUID.
	ID string `json:"id"`

	// Name is the agent's name.
	Name string `json:"name"`

	// Lifecycle is active, suspended, revoked or owner_reassignment_required.
	Lifecycle string `json:"lifecycle"`
}

// AgentCredentialPerson is someone whose own connection an agent signs in with.
type AgentCredentialPerson struct {
	// Name is the person's display name, or their email when they have none.
	Name string `json:"name"`

	// ConnectionExpired is true when the assigned connection no longer works.
	ConnectionExpired bool `json:"connection_expired"`
}

// AgentMCPServerAccess is one MCP server an agent's mcp:connect, mcp:read or
// mcp:write grants let it connect to.
type AgentMCPServerAccess struct {
	// ResourceID is the ID mcp:connect grants and checks name for the server.
	ResourceID string `json:"resource_id"`

	// Name is the server's name.
	Name string `json:"name"`

	// ProjectID and ProjectName are the project the server belongs to.
	ProjectID string `json:"project_id"`

	ProjectName string `json:"project_name"`

	// Grant is how the narrowest matching mcp:connect, mcp:read or mcp:write
	// grant reaches the server: server, project, or organization.
	Grant string `json:"grant"`

	// CredentialMode is shared, on_behalf_of, not_assigned, or
	// caller_provided.
	CredentialMode string `json:"credential_mode"`

	// OnBehalfOf lists the people whose connections the agent uses; empty
	// unless CredentialMode is on_behalf_of.
	OnBehalfOf []AgentCredentialPerson `json:"on_behalf_of"`

	// UnusableAssignments counts assigned connections that no longer work for
	// the agent, for example because their owner left the organization.
	UnusableAssignments int `json:"unusable_assignments"`
}

// AgentMCPAccessOutput is the agent's MCP servers and how it signs in to each.
type AgentMCPAccessOutput struct {
	Agent AgentIdentity `json:"agent"`

	// Servers are the MCP servers the agent's mcp:connect, mcp:read and
	// mcp:write grants reach.
	Servers []AgentMCPServerAccess `json:"servers"`

	// MissingServerGrants counts grants naming a server that no longer exists.
	MissingServerGrants int `json:"missing_server_grants"`

	// Truncated is true when the organization holds more servers than one read
	// matches, so some reachable servers may be missing.
	Truncated bool `json:"truncated"`
}

// AgentAuthorizationChecksInput names one agent and an optional period.
type AgentAuthorizationChecksInput struct {
	AgentID string `json:"agent_id" jsonschema:"exact agent UUID, as list_recent_tool_calls returns it or the agent's dashboard page shows it"`

	From *string `json:"from,omitempty" jsonschema:"inclusive RFC3339 start of the period; defaults to seven days before to"`

	To *string `json:"to,omitempty" jsonschema:"exclusive RFC3339 end of the period; defaults to now; at most 90 days after from"`
}

// AgentCheckGroup is the checks of one scope on one resource.
type AgentCheckGroup struct {
	// Scope is the permission checked, such as mcp:connect.
	Scope string `json:"scope"`

	// ResourceKind and ResourceID name what the check was about.
	ResourceKind string `json:"resource_kind"`

	ResourceID string `json:"resource_id"`

	// ResourceName is the MCP server's name when the resource is one.
	ResourceName string `json:"resource_name,omitempty"`

	// Count is how many such checks the read found.
	Count int `json:"count"`

	// LastAt is the latest one, RFC3339.
	LastAt string `json:"last_at"`

	// Reason is why the latest one was decided as it was, such as no_grants.
	Reason string `json:"reason"`
}

// AgentCheckOutcome is the checks of one outcome.
type AgentCheckOutcome struct {
	// Total counts every such check in the period, past the groups too.
	Total int `json:"total"`

	// Groups are grouped by scope and resource, the latest first.
	Groups []AgentCheckGroup `json:"groups"`

	// Truncated is true when Total is larger than the checks grouped.
	Truncated bool `json:"truncated"`
}

// AgentAuthorizationChecksOutput lists denied checks before allowed ones.
type AgentAuthorizationChecksOutput struct {
	Agent AgentIdentity `json:"agent"`

	// From and To are the period read, RFC3339.
	From string `json:"from"`

	To string `json:"to"`

	Denied AgentCheckOutcome `json:"denied"`

	Allowed AgentCheckOutcome `json:"allowed"`

	// HiddenScopes are left out of both outcomes and their totals.
	HiddenScopes []string `json:"hidden_scopes"`
}

// agentConnectTarget is one resource an mcp:connect grant can name.
type agentConnectTarget struct {
	resourceID  uuid.UUID
	serverID    uuid.NullUUID
	projectID   uuid.UUID
	projectName string
	name        string
	issuerID    uuid.NullUUID
}

// MCPAccess reads the MCP servers an agent's mcp:connect, mcp:read and mcp:write grants reach and
// whose credential it uses on each. It needs what the dashboard's Permissions
// tab needs: the agent's owner, or agent:write on the agent.
func (s *AgentIdentityReadService) MCPAccess(ctx context.Context, principal Principal, input AgentIdentityInput) (AgentMCPAccessOutput, error) {
	var zero AgentMCPAccessOutput
	if !s.valid() {
		return zero, ErrUnavailable
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
		return zero, err
	}
	agent, err := s.requireAgent(ctx, principal, input.AgentID, authz.ScopeAgentWrite)
	if err != nil {
		return zero, err
	}

	rows, err := agentsrepo.New(s.db).ListAgentPolicyGrants(ctx, agentsrepo.ListAgentPolicyGrantsParams{OrganizationID: principal.OrganizationID, AgentID: agent.ID})
	if err != nil {
		return zero, fmt.Errorf("list agent policy grants: %w", err)
	}
	grants := make([]authz.Grant, 0, len(rows))
	for _, row := range rows {
		// mcp:read and mcp:write include connecting, so they reach servers too;
		// the engine's scope expansion decides when each grant is matched.
		if !slices.Contains(agentConnectScopes, authz.Scope(row.Scope)) {
			continue
		}
		selector, err := authz.SelectorFromRow(row.Selectors)
		if err != nil {
			return zero, fmt.Errorf("parse agent %s selector: %w", row.Scope, err)
		}
		grants = append(grants, authz.NewGrantWithSelector(authz.Scope(row.Scope), selector))
	}

	output := AgentMCPAccessOutput{Agent: agentIdentity(agent), Servers: []AgentMCPServerAccess{}, MissingServerGrants: 0, Truncated: false}
	if len(grants) == 0 {
		return output, nil
	}
	targets, truncated, err := s.connectTargets(ctx, principal.OrganizationID)
	if err != nil {
		return zero, err
	}
	output.Truncated = truncated

	named := make(map[string]bool)
	for _, target := range targets {
		named[target.resourceID.String()] = true
	}
	for _, grant := range grants {
		resourceID := grant.Selector[authz.SelectorKeyResourceID]
		if resourceID != authz.WildcardResource && !named[resourceID] && !truncated {
			output.MissingServerGrants++
		}
	}

	credentials := newAgentCredentialReader(s.db, principal.OrganizationID, agent.ID, s.now())
	for _, target := range targets {
		reach, err := agentGrantReach(grants, target)
		if err != nil {
			return zero, err
		}
		if reach == "" {
			continue
		}
		access := AgentMCPServerAccess{
			ResourceID:          target.resourceID.String(),
			Name:                target.name,
			ProjectID:           target.projectID.String(),
			ProjectName:         target.projectName,
			Grant:               reach,
			CredentialMode:      agentCredentialShared,
			OnBehalfOf:          []AgentCredentialPerson{},
			UnusableAssignments: 0,
		}
		if err := credentials.describe(ctx, target, &access); err != nil {
			return zero, err
		}
		output.Servers = append(output.Servers, access)
	}
	return output, nil
}

// AuthorizationChecks reads an agent's recent authorization checks, denied
// before allowed, grouped by scope and resource. It needs what the dashboard's
// Authorization checks panel needs: the agent's owner or agent:read on the
// agent to open it, and org:read to list checks.
func (s *AgentIdentityReadService) AuthorizationChecks(ctx context.Context, principal Principal, input AgentAuthorizationChecksInput) (AgentAuthorizationChecksOutput, error) {
	var zero AgentAuthorizationChecksOutput
	if !s.valid() {
		return zero, ErrUnavailable
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
		return zero, err
	}
	agent, err := s.requireAgent(ctx, principal, input.AgentID, authz.ScopeAgentRead)
	if err != nil {
		return zero, err
	}
	// Live organization access is settled before the input is judged, so a
	// caller without org:read learns nothing from a window error.
	if err := s.engine.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: principal.OrganizationID, Dimensions: nil}); err != nil {
		if isAuthorizationRefusal(err) {
			return zero, ErrAgentChecksForbidden
		}
		return zero, fmt.Errorf("authorize agent checks: %w", err)
	}
	from, to, err := agentChecksWindow(input.From, input.To, s.now())
	if err != nil {
		return zero, err
	}

	fromText := from.Format(time.RFC3339Nano)
	toText := to.Format(time.RFC3339Nano)
	read := func(outcome string) (*accessgen.ListChallengesResult, error) {
		result, err := s.checks.ListChallenges(ctx, &accessgen.ListChallengesPayload{
			Outcome:       &outcome,
			PrincipalUrn:  new(urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()).String()),
			Scope:         nil,
			ExcludeScopes: hiddenAgentCheckScopes,
			ProjectID:     nil,
			Resolved:      nil,
			Ids:           nil,
			From:          &fromText,
			To:            &toText,
			Limit:         maxAgentChecksPerOutcome,
			Offset:        0,
			ApikeyToken:   nil,
			SessionToken:  nil,
		})
		if isAuthorizationRefusal(err) {
			return nil, ErrAgentChecksForbidden
		}
		if err != nil {
			return nil, fmt.Errorf("list agent %s checks: %w", outcome, err)
		}
		return result, nil
	}
	denied, err := read("deny")
	if err != nil {
		return zero, err
	}
	allowed, err := read("allow")
	if err != nil {
		return zero, err
	}

	names, err := s.mcpNames(ctx, principal.OrganizationID, denied, allowed)
	if err != nil {
		return zero, err
	}
	return AgentAuthorizationChecksOutput{
		Agent:        agentIdentity(agent),
		From:         fromText,
		To:           toText,
		Denied:       groupAgentChecks(denied, names),
		Allowed:      groupAgentChecks(allowed, names),
		HiddenScopes: slices.Clone(hiddenAgentCheckScopes),
	}, nil
}

// requireAgent loads the agent and admits its unblocked owner, or a caller
// holding scope on it, as the dashboard's agent authorizer does.
func (s *AgentIdentityReadService) requireAgent(ctx context.Context, principal Principal, rawID string, scope authz.Scope) (agentsrepo.Agent, error) {
	var zero agentsrepo.Agent
	agentID, err := uuid.Parse(strings.TrimSpace(rawID))
	if err != nil || agentID == uuid.Nil {
		return zero, ErrAgentNotAvailable
	}
	actor, ok := contextvalues.GetAuthContext(ctx)
	if !ok || actor == nil || principal.OrganizationID == "" || principal.UserID == "" || actor.ActiveOrganizationID != principal.OrganizationID || actor.UserID != principal.UserID {
		return zero, ErrForbidden
	}
	agent, err := agentsrepo.New(s.db).GetAgentByID(ctx, agentsrepo.GetAgentByIDParams{OrganizationID: principal.OrganizationID, ID: agentID})
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, ErrAgentNotAvailable
	}
	if err != nil {
		return zero, fmt.Errorf("read agent: %w", err)
	}
	if agent.OwnerUserID == principal.UserID && !agent.OwnerReassignmentRequiredAt.Valid {
		return agent, nil
	}
	if err := s.engine.Require(ctx, authz.Check{Scope: scope, ResourceKind: authz.ResourceKindAgent, ResourceID: agent.ID.String(), Dimensions: nil}); err != nil {
		if isAuthorizationRefusal(err) {
			return zero, ErrAgentNotAvailable
		}
		return zero, fmt.Errorf("authorize agent read: %w", err)
	}
	return agent, nil
}

// connectTargets lists what an mcp:connect grant can name in the organization.
func (s *AgentIdentityReadService) connectTargets(ctx context.Context, organizationID string) ([]agentConnectTarget, bool, error) {
	queries := platformrepo.New(s.db)
	servers, err := queries.ListPlatformMCPAgentConnectServers(ctx, platformrepo.ListPlatformMCPAgentConnectServersParams{OrganizationID: organizationID, LimitValue: maxAgentAccessTargets + 1})
	if err != nil {
		return nil, false, fmt.Errorf("list agent connect servers: %w", err)
	}
	toolsets, err := queries.ListPlatformMCPAgentConnectToolsets(ctx, platformrepo.ListPlatformMCPAgentConnectToolsetsParams{OrganizationID: organizationID, LimitValue: maxAgentAccessTargets + 1})
	if err != nil {
		return nil, false, fmt.Errorf("list agent connect toolsets: %w", err)
	}
	targets := make([]agentConnectTarget, 0, len(servers)+len(toolsets))
	// Several servers can serve one toolset, and a grant names them all by
	// that toolset's ID, so each resource is one target. The one that asks
	// people to sign in is kept, since it decides whose credential is used.
	byResource := make(map[uuid.UUID]int, len(servers))
	for _, server := range servers {
		target := agentConnectTarget{resourceID: server.ResourceID, serverID: uuid.NullUUID{UUID: server.McpServerID, Valid: true}, projectID: server.ProjectID, projectName: server.ProjectName, name: server.Name, issuerID: server.UserSessionIssuerID}
		at, seen := byResource[server.ResourceID]
		if !seen {
			byResource[server.ResourceID] = len(targets)
			targets = append(targets, target)
			continue
		}
		if !targets[at].issuerID.Valid && target.issuerID.Valid {
			targets[at] = target
		}
	}
	for _, toolset := range toolsets {
		targets = append(targets, agentConnectTarget{resourceID: toolset.ResourceID, serverID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, projectID: toolset.ProjectID, projectName: toolset.ProjectName, name: toolset.Name, issuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}})
	}
	slices.SortFunc(targets, func(a, b agentConnectTarget) int {
		return cmp.Or(cmp.Compare(a.projectName, b.projectName), cmp.Compare(a.name, b.name), cmp.Compare(a.resourceID.String(), b.resourceID.String()))
	})
	// Either list reaching its bound means rows were left out, even when
	// collapsing servers of one toolset brought the targets back under it.
	truncated := len(servers) > maxAgentAccessTargets || len(toolsets) > maxAgentAccessTargets || len(targets) > maxAgentAccessTargets
	return targets[:min(len(targets), maxAgentAccessTargets)], truncated, nil
}

// mcpNames names the MCP servers the checks are about.
func (s *AgentIdentityReadService) mcpNames(ctx context.Context, organizationID string, results ...*accessgen.ListChallengesResult) (map[string]string, error) {
	needed := false
	for _, result := range results {
		for _, check := range result.Challenges {
			needed = needed || conv.PtrValOr(check.ResourceKind, "") == authz.ResourceKindMCP
		}
	}
	names := make(map[string]string)
	if !needed {
		return names, nil
	}
	targets, _, err := s.connectTargets(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	for _, target := range targets {
		names[target.resourceID.String()] = target.name
	}
	return names, nil
}

// agentGrantReach says how the narrowest grant reaches target, or "" when
// none does. Grants are matched with the engine's selector semantics.
func agentGrantReach(grants []authz.Grant, target agentConnectTarget) (string, error) {
	check := authz.MCPCheck(authz.ScopeMCPConnect, target.resourceID.String(), target.projectID.String())
	reach := ""
	for _, grant := range grants {
		allowed, err := authz.GrantsAuthorize([]authz.Grant{grant}, check)
		if err != nil {
			return "", fmt.Errorf("match agent mcp:connect grant: %w", err)
		}
		if !allowed {
			continue
		}
		candidate := agentGrantOrganization
		switch {
		case grant.Selector[authz.SelectorKeyResourceID] != authz.WildcardResource:
			candidate = agentGrantServer
		case grant.Selector[authz.SelectorKeyProjectID] != "":
			candidate = agentGrantProject
		}
		if reach == "" || agentGrantRank(candidate) < agentGrantRank(reach) {
			reach = candidate
		}
	}
	return reach, nil
}

func agentGrantRank(reach string) int {
	switch reach {
	case agentGrantServer:
		return 0
	case agentGrantProject:
		return 1
	default:
		return 2
	}
}

// agentCredentialReader reads an agent's connection assignments once per
// project and user session issuer, which servers can share.
type agentCredentialReader struct {
	db             *pgxpool.Pool
	organizationID string
	agentID        uuid.UUID
	now            time.Time
	assignments    map[string]agentAssignments
}

// agentAssignments is what is assigned to the agent for one issuer.
type agentAssignments struct {
	people   []AgentCredentialPerson
	unusable int
}

func newAgentCredentialReader(db *pgxpool.Pool, organizationID string, agentID uuid.UUID, now time.Time) *agentCredentialReader {
	return &agentCredentialReader{db: db, organizationID: organizationID, agentID: agentID, now: now, assignments: make(map[string]agentAssignments)}
}

// describe sets access's credential mode, as the dashboard's "Acting on behalf
// of" reads it: a server that asks no person to sign in uses a shared
// credential, and otherwise the agent acts for the people whose connections
// are assigned to it.
func (r *agentCredentialReader) describe(ctx context.Context, target agentConnectTarget, access *AgentMCPServerAccess) error {
	if !target.serverID.Valid {
		return nil
	}
	auth, err := authclients.ReadServerAuthentication(ctx, r.db, r.organizationID, target.serverID.UUID)
	if err != nil {
		return fmt.Errorf("read mcp server authentication: %w", err)
	}
	if !target.issuerID.Valid || !offersPersonalSignIn(auth) {
		if passesCallerCredentialThrough(auth) {
			access.CredentialMode = agentCredentialCallerProvided
		}
		return nil
	}
	assigned, err := r.read(ctx, target.projectID, target.issuerID.UUID)
	if err != nil {
		return err
	}
	access.UnusableAssignments = assigned.unusable
	if len(assigned.people) == 0 {
		access.CredentialMode = agentCredentialNotAssigned
		return nil
	}
	access.CredentialMode = agentCredentialOnBehalfOf
	access.OnBehalfOf = slices.Clone(assigned.people)
	return nil
}

func (r *agentCredentialReader) read(ctx context.Context, projectID, issuerID uuid.UUID) (agentAssignments, error) {
	key := projectID.String() + ":" + issuerID.String()
	if known, ok := r.assignments[key]; ok {
		return known, nil
	}
	queries := remotesessionsrepo.New(r.db)
	bindings, err := queries.ListPrincipalRemoteSessionBindings(ctx, remotesessionsrepo.ListPrincipalRemoteSessionBindingsParams{ProjectID: projectID, OrganizationID: r.organizationID, PrincipalID: r.agentID, UserSessionIssuerID: issuerID})
	if err != nil {
		return agentAssignments{}, fmt.Errorf("list agent connection assignments: %w", err)
	}
	sources, err := queries.ListEligiblePrincipalRemoteSessionBindings(ctx, remotesessionsrepo.ListEligiblePrincipalRemoteSessionBindingsParams{ProjectID: projectID, OrganizationID: r.organizationID, PrincipalID: r.agentID, UserSessionIssuerID: issuerID})
	if err != nil {
		return agentAssignments{}, fmt.Errorf("resolve agent connection assignments: %w", err)
	}
	eligible := make(map[uuid.UUID]remotesessionsrepo.RemoteSession, len(sources))
	for _, source := range sources {
		eligible[source.ID] = source
	}

	assigned := agentAssignments{people: []AgentCredentialPerson{}, unusable: 0}
	index := make(map[string]int)
	for _, binding := range bindings {
		source, ok := eligible[binding.RemoteSessionID]
		if !ok || source.SubjectUrn.Kind != urn.SessionSubjectKindUser {
			assigned.unusable++
			continue
		}
		expired := remoteSessionEnded(source, r.now)
		if at, ok := index[source.SubjectUrn.ID]; ok {
			assigned.people[at].ConnectionExpired = assigned.people[at].ConnectionExpired || expired
			continue
		}
		name := cmp.Or(conv.PtrValOr(conv.FromPGText[string](binding.SubjectDisplayName), ""), conv.PtrValOr(conv.FromPGText[string](binding.SubjectEmail), ""), source.SubjectUrn.ID)
		index[source.SubjectUrn.ID] = len(assigned.people)
		assigned.people = append(assigned.people, AgentCredentialPerson{Name: name, ConnectionExpired: expired})
	}
	r.assignments[key] = assigned
	return assigned, nil
}

// offersPersonalSignIn reports whether a server may ask each person to sign in
// upstream. While setup is incomplete its auth clients do not record the
// upstream yet, so it is treated as personal, as the dashboard does.
func offersPersonalSignIn(auth *types.McpServerAuthentication) bool {
	if auth == nil {
		return false
	}
	if auth.Status == "setup_incomplete" {
		return true
	}
	return slices.ContainsFunc(auth.Clients, func(client *types.AuthClient) bool {
		return client != nil && client.Enabled && client.ClientType == authz.AuthClientTypeRemoteSession
	})
}

// isAuthorizationRefusal reports whether err is the caller being refused, as
// opposed to authorization itself failing.
func isAuthorizationRefusal(err error) bool {
	shared, ok := errors.AsType[*oops.ShareableError](err)
	return ok && (shared.Code == oops.CodeForbidden || shared.Code == oops.CodeUnauthorized)
}

// passesCallerCredentialThrough reports whether a server takes the credential
// the caller's MCP client sends rather than one Speakeasy holds.
func passesCallerCredentialThrough(auth *types.McpServerAuthentication) bool {
	if auth == nil {
		return false
	}
	return slices.ContainsFunc(auth.Clients, func(client *types.AuthClient) bool {
		return client != nil && client.Enabled && client.ClientType == authz.AuthClientTypePassThrough
	})
}

// remoteSessionEnded reports whether a connection no longer works, read from
// its expiry fields as the dashboard reads them. With a refresh token the
// refresh deadline matters; without one, the access token's expiry. The
// authorization deadline caps both.
func remoteSessionEnded(session remotesessionsrepo.RemoteSession, now time.Time) bool {
	if session.AuthorizationExpiresAt.Valid && !session.AuthorizationExpiresAt.Time.After(now) {
		return true
	}
	if session.RefreshTokenEncrypted.Valid && session.RefreshTokenEncrypted.String != "" {
		if !session.RefreshExpiresAt.Valid || session.RefreshExpiresAt.Time.After(now) {
			return false
		}
	}
	return session.AccessExpiresAt.Valid && !session.AccessExpiresAt.Time.After(now)
}

func agentIdentity(agent agentsrepo.Agent) AgentIdentity {
	lifecycle := "active"
	switch {
	case agent.RevokedAt.Valid:
		lifecycle = "revoked"
	case agent.SuspendedAt.Valid:
		lifecycle = "suspended"
	case agent.OwnerReassignmentRequiredAt.Valid:
		lifecycle = "owner_reassignment_required"
	}
	return AgentIdentity{ID: agent.ID.String(), Name: agent.Name, Lifecycle: lifecycle}
}

// agentChecksWindow resolves the period, defaulting to the last week.
func agentChecksWindow(rawFrom, rawTo *string, now time.Time) (time.Time, time.Time, error) {
	from, to, err := conv.ParseOptionalTimeWindow(rawFrom, rawTo)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: %w", ErrAgentChecksWindowInvalid, err)
	}
	end := now.UTC()
	if to != nil {
		end = to.UTC()
	}
	start := end.Add(-defaultAgentChecksWindow)
	if from != nil {
		start = from.UTC()
	}
	if !start.Before(end) || end.Sub(start) > maxAgentChecksWindow {
		return time.Time{}, time.Time{}, ErrAgentChecksWindowInvalid
	}
	return start, end, nil
}

// groupAgentChecks groups one outcome's checks by scope and resource. Rows
// arrive newest first, so each group's first row is its latest.
func groupAgentChecks(result *accessgen.ListChallengesResult, names map[string]string) AgentCheckOutcome {
	groups := []AgentCheckGroup{}
	index := make(map[string]int)
	for _, check := range result.Challenges {
		kind := conv.PtrValOr(check.ResourceKind, "")
		resourceID := conv.PtrValOr(check.ResourceID, "")
		key := check.Scope + "\x00" + kind + "\x00" + resourceID
		if at, ok := index[key]; ok {
			groups[at].Count++
			continue
		}
		name := ""
		if kind == authz.ResourceKindMCP {
			name = names[resourceID]
		}
		index[key] = len(groups)
		groups = append(groups, AgentCheckGroup{Scope: check.Scope, ResourceKind: kind, ResourceID: resourceID, ResourceName: name, Count: 1, LastAt: check.Timestamp, Reason: check.Reason})
	}
	return AgentCheckOutcome{Total: result.Total, Groups: groups, Truncated: result.Total > len(result.Challenges)}
}
