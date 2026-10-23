package authclientbackfill

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// GrantKind is the default a provisioned grant belongs to.
type GrantKind string

const (
	// GrantPersonalDefault is mcp:auth on remote_session clients for the
	// represented user's own connection, held by user:all and by agents.
	GrantPersonalDefault GrantKind = "personal_default"

	// GrantSharedClient is mcp:auth on shared clients for each principal
	// that can connect to a server using them, mirroring today's reach.
	GrantSharedClient GrantKind = "shared_client"

	// GrantAPIKey is a legacy key's own policy: mcp:connect on every server
	// in each project it reaches, and mcp:auth on their shared clients.
	GrantAPIKey GrantKind = "api_key"

	// GrantAttachment is mcp:auth for an agent on the one connection an
	// attachment binds it to: that user's connection through that upstream
	// OAuth client, whichever auth client uses it.
	GrantAttachment GrantKind = "attachment"
)

// GrantKinds lists every grant kind in report order.
var GrantKinds = []GrantKind{GrantPersonalDefault, GrantSharedClient, GrantAPIKey, GrantAttachment}

// AttachmentRemediation reasons name why an attachment was not carried over.
// Each needs its owner to reconnect or configure the agent; the backfill never
// substitutes another user or a broader grant.
const (
	// AttachmentNotByUser is an attachment no user made.
	AttachmentNotByUser = "not_attached_by_user"
	// AttachmentOwnerChanged is an attachment made by someone who no longer
	// owns the agent; the attachment runtime no longer serves it.
	AttachmentOwnerChanged = "owner_changed"
	// AttachmentForeignConnection is an attachment to a connection the
	// attaching user does not own.
	AttachmentForeignConnection = "foreign_connection"
	// AttachmentConnectionGone is an attachment whose connection was removed
	// or authorized again since.
	AttachmentConnectionGone = "connection_gone"
	// AttachmentAgentInactive is an attachment of an agent that is suspended,
	// revoked or waiting for a new owner, or whose owner is no longer an
	// active member.
	AttachmentAgentInactive = "agent_inactive"
	// AttachmentConfigurationGone is an attachment whose sign-in, project,
	// upstream client or issuer was removed, or no longer reaches the
	// attachment's project.
	AttachmentConfigurationGone = "configuration_gone"
	// AttachmentClientUnrecorded is an attachment whose upstream OAuth client
	// no personal auth client in the organization uses yet.
	AttachmentClientUnrecorded = "client_unrecorded"
)

// AttachmentRemediation is one attachment the grants phase could not carry
// over, by id only.
type AttachmentRemediation struct {
	BindingID string `json:"binding_id"`
	AgentID   string `json:"agent_id"`
	Reason    string `json:"reason"`
}

// serverAccessScopes are the scopes that satisfy mcp:connect.
var serverAccessScopes = []string{string(authz.ScopeMCPConnect), string(authz.ScopeMCPRead), string(authz.ScopeMCPWrite)}

// grantPlan collects the grants an organization is missing, per principal.
type grantPlan struct {
	// existing holds every grant already held or ever provisioned, so a
	// grant an admin removed is never written again.
	existing map[string]bool
	// provisioned holds the grants this backfill wrote, live or removed, that
	// no admin has taken over.
	provisioned map[string]bool
	// history counts, per principal, the grants ever provisioned to it, and
	// taken those an admin took over.
	history map[string]int
	taken   map[string]int
	missing map[urn.Principal][]*authz.RoleGrant
	kinds   map[urn.Principal][]GrantKind
	order   []urn.Principal
	counts  map[GrantKind]int
}

func grantKey(principalURN string, scope authz.Scope, selector authz.Selector) (string, error) {
	raw, err := selector.MarshalJSON()
	if err != nil {
		return "", fmt.Errorf("marshal selector: %w", err)
	}
	return principalURN + "|" + string(scope) + "|" + string(raw), nil
}

// want records grant as missing unless principal already holds it.
func (p *grantPlan) want(kind GrantKind, principal urn.Principal, scope authz.Scope, selector authz.Selector) error {
	key, err := grantKey(principal.String(), scope, selector)
	if err != nil {
		return err
	}
	if p.existing[key] {
		return nil
	}
	p.existing[key] = true
	if _, ok := p.missing[principal]; !ok {
		p.order = append(p.order, principal)
	}
	p.missing[principal] = append(p.missing[principal], &authz.RoleGrant{Scope: string(scope), Selectors: []authz.Selector{selector}})
	p.kinds[principal] = append(p.kinds[principal], kind)
	p.counts[kind]++
	return nil
}

// runGrants provisions the default mcp:auth grants so the mandatory check
// allows what today's decisions allow: personal defaults always, shared client
// grants with withShared, and legacy API key policies with withKeys, which
// implies withShared. It only ever adds grants.
func runGrants(ctx context.Context, tx pgx.Tx, opts Options, report *Report, withShared, withKeys bool) error {
	q := New(tx)
	plan := &grantPlan{existing: map[string]bool{}, provisioned: map[string]bool{}, history: map[string]int{}, taken: map[string]int{}, missing: map[urn.Principal][]*authz.RoleGrant{}, kinds: map[urn.Principal][]GrantKind{}, order: nil, counts: map[GrantKind]int{}}

	held, err := q.ListAuthClientGrantRows(ctx, ListAuthClientGrantRowsParams{
		OrganizationID: opts.OrganizationID,
		Scopes:         []string{string(authz.ScopeMCPAuth), string(authz.ScopeMCPConnect)},
	})
	if err != nil {
		return fmt.Errorf("list held grants: %w", err)
	}
	for _, row := range held {
		selector, err := authz.SelectorFromRow(row.Selectors)
		if err != nil {
			// A row the engine cannot read grants nothing either.
			continue
		}
		key, err := grantKey(row.PrincipalUrn.String(), authz.Scope(row.Scope), selector)
		if err != nil {
			return err
		}
		plan.existing[key] = true
	}

	provisions, err := q.ListAuthPermissionProvisions(ctx, opts.OrganizationID)
	if err != nil {
		return fmt.Errorf("list provisioned grants: %w", err)
	}
	for _, row := range provisions {
		selector, err := authz.SelectorFromRow(row.Selectors)
		if err != nil {
			continue
		}
		key, err := grantKey(row.PrincipalUrn, authz.Scope(row.Scope), selector)
		if err != nil {
			return err
		}
		plan.existing[key] = true
		plan.history[row.PrincipalUrn]++
		if row.TakenOver {
			plan.taken[row.PrincipalUrn]++
			continue
		}
		plan.provisioned[key] = true
	}

	if err := plan.want(GrantPersonalDefault, authz.AllUsersPrincipal(), authz.ScopeMCPAuth, authz.PersonalAuthClientSelector()); err != nil {
		return err
	}

	uses, err := q.ListSharedAuthClientServers(ctx, opts.OrganizationID)
	if err != nil {
		return fmt.Errorf("list shared auth client servers: %w", err)
	}
	// Clients and links that may be awaiting retirement are never granted;
	// agents still get their personal default.
	if !withShared {
		uses = nil
	}
	if err := planServerAccessGrants(ctx, q, opts, uses, plan); err != nil {
		return err
	}
	if err := planAttachmentGrants(ctx, q, opts, plan, report); err != nil {
		return err
	}
	// A key's policy is written once, so never from a run whose earlier
	// phases failed to record some of its clients.
	if withShared && withKeys {
		if err := planAPIKeyGrants(ctx, q, opts, uses, plan); err != nil {
			return err
		}
	}

	for _, kind := range GrantKinds {
		report.PendingGrants += plan.counts[kind]
		report.PendingGrantsByKind[kind] += plan.counts[kind]
	}
	if !opts.Apply {
		return nil
	}
	for _, principal := range plan.order {
		granted := false
		for i, grant := range plan.missing[principal] {
			inserted, err := authz.InsertPrincipalGrantsIfAbsentTx(ctx, tx, opts.OrganizationID, principal, []*authz.RoleGrant{grant})
			if err != nil {
				return fmt.Errorf("grant %s: %w", principal.String(), err)
			}
			// A grant someone else wrote meanwhile stays theirs.
			if inserted == 0 {
				continue
			}
			raw, err := grant.Selectors[0].MarshalJSON()
			if err != nil {
				return fmt.Errorf("marshal selector: %w", err)
			}
			if err := q.RecordAuthPermissionProvision(ctx, RecordAuthPermissionProvisionParams{
				OrganizationID: opts.OrganizationID,
				PrincipalUrn:   principal.String(),
				Scope:          grant.Scope,
				Selectors:      raw,
				Kind:           string(plan.kinds[principal][i]),
			}); err != nil {
				return fmt.Errorf("record provisioned grant: %w", err)
			}
			report.GrantsWritten++
			granted = true
		}
		if granted && principal.Type == urn.PrincipalTypeAPIKey {
			report.APIKeysGranted = append(report.APIKeysGranted, principal.ID)
		}
	}
	return nil
}

// serverUses indexes shared client links by the ids mcp:connect is checked on.
type serverUses struct {
	all  []ListSharedAuthClientServersRow
	byID map[string][]ListSharedAuthClientServersRow
}

func indexUses(uses []ListSharedAuthClientServersRow) serverUses {
	index := serverUses{all: uses, byID: map[string][]ListSharedAuthClientServersRow{}}
	for _, use := range uses {
		index.byID[use.McpServerID.String()] = append(index.byID[use.McpServerID.String()], use)
		if use.ToolsetID.Valid {
			index.byID[use.ToolsetID.UUID.String()] = append(index.byID[use.ToolsetID.UUID.String()], use)
		}
	}
	return index
}

// planServerAccessGrants mirrors each server access grant onto the shared
// clients its servers use today, naming each client: a wildcard server grant
// never becomes a grant on clients it does not reach now or on clients created
// later. A user's grant is recorded as provisioned, which keeps it from
// outranking an inherited block. Agents also get the personal default, since
// user:all is not in their policy.
func planServerAccessGrants(ctx context.Context, q *Queries, opts Options, uses []ListSharedAuthClientServersRow, plan *grantPlan) error {
	rows, err := q.ListServerAccessGrants(ctx, ListServerAccessGrantsParams{OrganizationID: opts.OrganizationID, Scopes: serverAccessScopes})
	if err != nil {
		return fmt.Errorf("list server access grants: %w", err)
	}
	index := indexUses(uses)
	for _, row := range rows {
		selector, err := authz.SelectorFromRow(row.Selectors)
		if err != nil {
			continue
		}
		if row.PrincipalUrn.Type == urn.PrincipalTypeAgent {
			if err := plan.want(GrantPersonalDefault, row.PrincipalUrn, authz.ScopeMCPAuth, authz.PersonalAuthClientSelector()); err != nil {
				return err
			}
		}
		grant := authz.Grant{PrincipalUrn: row.PrincipalUrn.String(), Scope: authz.Scope(row.Scope), Selector: selector, Inferred: false}
		candidates := index.all
		if resourceID := selector[authz.SelectorKeyResourceID]; resourceID != authz.WildcardResource {
			candidates = index.byID[resourceID]
		}
		for _, use := range candidates {
			if !reachesServer(grant, use) {
				continue
			}
			if err := plan.want(GrantSharedClient, row.PrincipalUrn, authz.ScopeMCPAuth, authz.AuthClientSelector(use.AuthClientID.String(), "")); err != nil {
				return err
			}
		}
	}

	agentRoles, err := q.ListAgentRolePrincipals(ctx, opts.OrganizationID)
	if err != nil {
		return fmt.Errorf("list agent roles: %w", err)
	}
	for _, role := range agentRoles {
		principal, err := urn.ParsePrincipal(role)
		if err != nil {
			continue
		}
		if err := plan.want(GrantPersonalDefault, principal, authz.ScopeMCPAuth, authz.PersonalAuthClientSelector()); err != nil {
			return err
		}
	}
	return nil
}

// reachesServer reports whether grant allows connecting to use's server,
// checked on the server and on its toolset.
func reachesServer(grant authz.Grant, use ListSharedAuthClientServersRow) bool {
	ids := []uuid.UUID{use.McpServerID}
	if use.ToolsetID.Valid {
		ids = append(ids, use.ToolsetID.UUID)
	}
	return slices.ContainsFunc(ids, func(id uuid.UUID) bool {
		return authz.GrantAllows(grant, authz.MCPCheck(authz.ScopeMCPConnect, id.String(), use.ProjectID.String()))
	})
}

// keyReach returns the projects a key's policy should cover. A key with no
// policy and no provisioning history gets its full reach. A key whose policy is
// exactly what was provisioned, with nothing removed or taken over, keeps the
// projects it covers and gets clients recorded since. Any other key is left
// alone.
func (p *grantPlan) keyReach(principal urn.Principal, policy []authz.Grant, keyProject uuid.NullUUID, projects []uuid.UUID) ([]string, bool) {
	urnString := principal.String()
	if len(policy) == 0 {
		if p.history[urnString] > 0 {
			return nil, false
		}
		var reach []string
		for _, project := range projects {
			if !keyProject.Valid || keyProject.UUID == project {
				reach = append(reach, project.String())
			}
		}
		return reach, true
	}
	// Only a policy that is exactly what was provisioned, untouched since,
	// is topped up.
	if p.taken[urnString] > 0 || p.history[urnString] != len(policy) {
		return nil, false
	}
	var reach []string
	for _, grant := range policy {
		key, err := grantKey(urnString, grant.Scope, grant.Selector)
		if err != nil || !p.provisioned[key] {
			return nil, false
		}
		if grant.Scope == authz.ScopeMCPConnect {
			reach = append(reach, grant.Selector[authz.SelectorKeyProjectID])
		}
	}
	return reach, len(reach) > 0
}

type keyGrant struct {
	scope    authz.Scope
	selector authz.Selector
}

// planAPIKeyGrants gives each live legacy key that reaches MCP what it can
// reach today, as keyReach decides: every server in its project, or in every
// project for an organization-wide key, and those servers' shared clients.
// EMA clients act for a user, which a key never represents, so keys never get
// them.
func planAPIKeyGrants(ctx context.Context, q *Queries, opts Options, uses []ListSharedAuthClientServersRow, plan *grantPlan) error {
	keys, err := q.ListActiveLegacyAPIKeys(ctx, opts.OrganizationID)
	if err != nil {
		return fmt.Errorf("list api keys: %w", err)
	}
	rows, err := q.ListAPIKeyGrantRows(ctx, opts.OrganizationID)
	if err != nil {
		return fmt.Errorf("list api key policies: %w", err)
	}
	policies := map[urn.Principal][]authz.Grant{}
	for _, row := range rows {
		selector, err := authz.SelectorFromRow(row.Selectors)
		if err != nil {
			selector = authz.Selector{}
		}
		policies[row.PrincipalUrn] = append(policies[row.PrincipalUrn], authz.Grant{PrincipalUrn: row.PrincipalUrn.String(), Scope: authz.Scope(row.Scope), Selector: selector, Inferred: false})
	}
	projects, err := q.ListLiveProjects(ctx, opts.OrganizationID)
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}
	for _, key := range keys {
		principal := urn.NewAPIKeyPrincipal(key.ID)
		if !authz.APIKeyReachesMCP(key.Scopes) {
			continue
		}
		reach, ok := plan.keyReach(principal, policies[principal], key.ProjectID, projects)
		if !ok {
			continue
		}
		for _, project := range reach {
			wants := []keyGrant{{scope: authz.ScopeMCPConnect, selector: authz.ProjectServersSelector(project)}}
			for _, use := range uses {
				if use.ProjectID.String() == project && use.ClientType != TypeEMA {
					wants = append(wants, keyGrant{scope: authz.ScopeMCPAuth, selector: authz.AuthClientSelector(use.AuthClientID.String(), project)})
				}
			}
			for _, w := range wants {
				if err := authz.ValidateAPIKeyPolicyGrant(w.scope, w.selector); err != nil {
					return fmt.Errorf("api key %s policy: %w", key.ID, err)
				}
				if err := plan.want(GrantAPIKey, principal, w.scope, w.selector); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// planAttachmentGrants carries each attachment over to the grant that serves
// the same access without it: the agent may use its authorizing user's
// connection through the attached upstream OAuth client. The connection owner
// is that user, concretely, so the grant never reaches another user's
// connection. An attachment the runtime would no longer serve, or whose client
// is not recorded, is reported for remediation instead.
func planAttachmentGrants(ctx context.Context, q *Queries, opts Options, plan *grantPlan, report *Report) error {
	rows, err := q.ListLiveAgentAttachments(ctx, opts.OrganizationID)
	if err != nil {
		return fmt.Errorf("list agent attachments: %w", err)
	}
	// Attachment grants name upstream_client, so they are planned only once
	// the organization's write gate allows it. Until then they wait: nothing
	// is written without the dimension, and the run reports them pending.
	writable := authz.UpstreamSelectorWritesAllowed(ctx, opts.OrganizationID)
	for _, row := range rows {
		reason := attachmentRemediationReason(row)
		if reason != "" {
			report.AttachmentRemediations = append(report.AttachmentRemediations, AttachmentRemediation{BindingID: row.BindingID.String(), AgentID: row.AgentID.String(), Reason: reason})
			continue
		}
		if !writable {
			report.AttachmentsWaiting++
			continue
		}
		principal := urn.NewPrincipal(urn.PrincipalTypeAgent, row.AgentID.String())
		if err := plan.want(GrantAttachment, principal, authz.ScopeMCPAuth, authz.ConnectionAuthClientSelector(row.RemoteSessionClientID.String(), row.OwnerUserID)); err != nil {
			return err
		}
	}
	return nil
}

func attachmentRemediationReason(row ListLiveAgentAttachmentsRow) string {
	attacher, err := urn.ParseSessionSubject(row.AttachedBySubjectID)
	switch {
	case err != nil || attacher.Kind != urn.SessionSubjectKindUser:
		return AttachmentNotByUser
	case attacher.ID != row.OwnerUserID:
		return AttachmentOwnerChanged
	case row.ConnectionSubject != row.AttachedBySubjectID:
		return AttachmentForeignConnection
	case !row.AgentActive:
		return AttachmentAgentInactive
	case !row.ConnectionLive:
		return AttachmentConnectionGone
	case !row.ConfigurationLive:
		return AttachmentConfigurationGone
	case !row.ClientRecorded:
		return AttachmentClientUnrecorded
	default:
		return ""
	}
}
