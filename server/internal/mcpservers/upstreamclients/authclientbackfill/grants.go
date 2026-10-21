package authclientbackfill

import (
	"context"
	"fmt"
	"maps"
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
)

// GrantKinds lists every grant kind in report order.
var GrantKinds = []GrantKind{GrantPersonalDefault, GrantSharedClient, GrantAPIKey}

// serverAccessScopes are the scopes that satisfy mcp:connect.
var serverAccessScopes = []string{string(authz.ScopeMCPConnect), string(authz.ScopeMCPRead), string(authz.ScopeMCPWrite)}

// grantPlan collects the grants an organization is missing, per principal.
type grantPlan struct {
	existing map[string]bool
	missing  map[urn.Principal][]*authz.RoleGrant
	kinds    map[urn.Principal][]GrantKind
	order    []urn.Principal
	counts   map[GrantKind]int
}

func grantKey(principal urn.Principal, scope authz.Scope, selector authz.Selector) (string, error) {
	raw, err := selector.MarshalJSON()
	if err != nil {
		return "", fmt.Errorf("marshal selector: %w", err)
	}
	return principal.String() + "|" + string(scope) + "|" + string(raw), nil
}

// want records grant as missing unless principal already holds it.
func (p *grantPlan) want(kind GrantKind, principal urn.Principal, scope authz.Scope, selector authz.Selector) error {
	key, err := grantKey(principal, scope, selector)
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

// runGrants provisions the default mcp:auth grants and legacy API key
// policies, so the mandatory check allows what today's decisions allow. It
// only ever adds grants.
func runGrants(ctx context.Context, tx pgx.Tx, opts Options, report *Report) error {
	q := New(tx)
	plan := &grantPlan{existing: map[string]bool{}, missing: map[urn.Principal][]*authz.RoleGrant{}, kinds: map[urn.Principal][]GrantKind{}, order: nil, counts: map[GrantKind]int{}}

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
		key, err := grantKey(row.PrincipalUrn, authz.Scope(row.Scope), selector)
		if err != nil {
			return err
		}
		plan.existing[key] = true
	}

	if err := plan.want(GrantPersonalDefault, authz.AllUsersPrincipal(), authz.ScopeMCPAuth, authz.PersonalAuthClientSelector()); err != nil {
		return err
	}

	uses, err := q.ListSharedAuthClientServers(ctx, opts.OrganizationID)
	if err != nil {
		return fmt.Errorf("list shared auth client servers: %w", err)
	}
	if err := planServerAccessGrants(ctx, q, opts, uses, plan); err != nil {
		return err
	}
	if err := planAPIKeyGrants(ctx, q, opts, uses, plan); err != nil {
		return err
	}

	for _, kind := range GrantKinds {
		report.PendingGrants += plan.counts[kind]
		report.PendingGrantsByKind[kind] += plan.counts[kind]
	}
	if !opts.Apply {
		return nil
	}
	for _, principal := range plan.order {
		inserted, err := authz.InsertPrincipalGrantsIfAbsentTx(ctx, tx, opts.OrganizationID, principal, plan.missing[principal])
		if err != nil {
			return fmt.Errorf("grant %s: %w", principal.String(), err)
		}
		report.GrantsWritten += int(inserted)
		if inserted > 0 && principal.Type == urn.PrincipalTypeAPIKey {
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
// clients its servers use, in the same shape: a grant naming a server names
// its clients, and a wildcard grant covers each client type it reaches, so an
// inferred grant never becomes a concrete direct grant that outranks a block.
// Agents also get the personal default, since user:all is not in their
// policy. Connecting still requires mcp:connect, so nothing widens.
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
		grant := authz.Grant{PrincipalUrn: row.PrincipalUrn.String(), Scope: authz.Scope(row.Scope), Selector: selector}
		resourceID := selector[authz.SelectorKeyResourceID]
		if resourceID != authz.WildcardResource {
			for _, use := range index.byID[resourceID] {
				if reachesServer(grant, use) {
					if err := plan.want(GrantSharedClient, row.PrincipalUrn, authz.ScopeMCPAuth, authz.AuthClientSelector(use.AuthClientID.String(), "")); err != nil {
						return err
					}
				}
			}
			continue
		}
		project := selector[authz.SelectorKeyProjectID]
		for _, use := range index.all {
			if reachesServer(grant, use) {
				if err := plan.want(GrantSharedClient, row.PrincipalUrn, authz.ScopeMCPAuth, authz.AuthClientTypeSelector(use.ClientType, project)); err != nil {
					return err
				}
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

type keyGrant struct {
	scope    authz.Scope
	selector authz.Selector
}

// provisionedKeyProjects returns the projects a key's policy covers when
// every grant in it has the shape this backfill writes, so later clients can
// be added. Any other grant makes the policy explicit, and it is left alone.
func provisionedKeyProjects(grants []authz.Grant) ([]string, bool) {
	var projects []string
	for _, grant := range grants {
		project := grant.Selector[authz.SelectorKeyProjectID]
		switch {
		case grant.Scope == authz.ScopeMCPConnect && project != "" && maps.Equal(grant.Selector, authz.ProjectServersSelector(project)):
			projects = append(projects, project)
		case grant.Scope == authz.ScopeMCPAuth && project != "" && maps.Equal(grant.Selector, authz.AuthClientSelector(grant.Selector[authz.SelectorKeyResourceID], project)) && grant.Selector[authz.SelectorKeyResourceID] != authz.WildcardResource:
		default:
			return nil, false
		}
	}
	return projects, len(projects) > 0
}

// planAPIKeyGrants gives each live legacy key that reaches MCP what it can
// reach today. A key with no policy gets every server in its project, or in
// every project for an organization-wide key, and those servers' shared
// clients. A key whose policy is only what this backfill writes gets clients
// added since, in the projects it already covers. EMA clients act for a user,
// which a key never represents, so keys never get them.
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
		policies[row.PrincipalUrn] = append(policies[row.PrincipalUrn], authz.Grant{PrincipalUrn: row.PrincipalUrn.String(), Scope: authz.Scope(row.Scope), Selector: selector})
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
		var reach []string
		if policy, ok := policies[principal]; ok {
			covered, provisioned := provisionedKeyProjects(policy)
			if !provisioned {
				continue
			}
			reach = covered
		} else {
			for _, project := range projects {
				if !key.ProjectID.Valid || key.ProjectID.UUID == project {
					reach = append(reach, project.String())
				}
			}
		}
		for _, project := range reach {
			if key.ProjectID.Valid && project != key.ProjectID.UUID.String() {
				continue
			}
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
