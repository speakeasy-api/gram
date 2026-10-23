package authz

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// AuthPermissionKindCreationDefault marks a default written when an
// organization was created rather than by the backfill, so a backfill
// rollback leaves it.
const AuthPermissionKindCreationDefault = "creation_default"

// AuthPermissionKindExplicitPolicy marks a principal whose whole policy an
// admin wrote, so the backfill never provisions for it again.
const AuthPermissionKindExplicitPolicy = "explicit_policy"

// PersonalAuthClientSelector is the default every organization grants
// user:all: each caller may use personal (remote_session) clients only for
// the connection its represented user owns.
func PersonalAuthClientSelector() Selector {
	return Selector{
		SelectorKeyResourceKind:    ResourceKindMCPAuthClient,
		SelectorKeyResourceID:      WildcardResource,
		SelectorKeyClientType:      AuthClientTypeRemoteSession,
		SelectorKeyConnectionOwner: SelectorValueRepresentedUser,
	}
}

// AuthClientSelector names one auth client, narrowed to a project when
// projectID is set.
func AuthClientSelector(clientID, projectID string) Selector {
	selector := Selector{SelectorKeyResourceKind: ResourceKindMCPAuthClient, SelectorKeyResourceID: clientID}
	if projectID != "" {
		selector[SelectorKeyProjectID] = projectID
	}
	return selector
}

// ConnectionAuthClientSelector names one logical connection: ownerUserID's
// connection through the upstream OAuth client, whichever personal auth
// client uses it.
func ConnectionAuthClientSelector(upstreamClientID, ownerUserID string) Selector {
	return Selector{
		SelectorKeyResourceKind:    ResourceKindMCPAuthClient,
		SelectorKeyResourceID:      WildcardResource,
		SelectorKeyClientType:      AuthClientTypeRemoteSession,
		SelectorKeyUpstreamClient:  upstreamClientID,
		SelectorKeyConnectionOwner: ownerUserID,
	}
}

// AuthClientTypeSelector covers every client of one type, narrowed to a
// project when projectID is set.
func AuthClientTypeSelector(clientType, projectID string) Selector {
	selector := Selector{SelectorKeyResourceKind: ResourceKindMCPAuthClient, SelectorKeyResourceID: WildcardResource, SelectorKeyClientType: clientType}
	if projectID != "" {
		selector[SelectorKeyProjectID] = projectID
	}
	return selector
}

// ProjectServersSelector covers every server in one project.
func ProjectServersSelector(projectID string) Selector {
	return Selector{SelectorKeyResourceKind: ResourceKindMCP, SelectorKeyResourceID: WildcardResource, SelectorKeyProjectID: projectID}
}

// GrantAllows reports whether grant alone would allow check, as Require
// matches it. Blocks are not considered.
func GrantAllows(grant Grant, check Check) bool {
	return grantMatchingCheck(&grant, check.expand()) != nil
}

// SeedPersonalAuthClientDefaultTx grants user:all the personal auth client
// default unless it already holds it, recording it as provisioned so a later
// backfill never restores it once removed.
func SeedPersonalAuthClientDefaultTx(ctx context.Context, dbtx repo.DBTX, organizationID string) error {
	selector := PersonalAuthClientSelector()
	grant := &RoleGrant{Scope: string(ScopeMCPAuth), Selectors: []Selector{selector}}
	inserted, err := InsertPrincipalGrantsIfAbsentTx(ctx, dbtx, organizationID, AllUsersPrincipal(), []*RoleGrant{grant})
	if err != nil {
		return fmt.Errorf("seed personal auth client default: %w", err)
	}
	if inserted == 0 {
		return nil
	}
	raw, err := selector.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal personal auth client default: %w", err)
	}
	if err := repo.New(dbtx).RecordAuthPermissionDefault(ctx, repo.RecordAuthPermissionDefaultParams{
		OrganizationID: organizationID,
		PrincipalUrn:   AllUsersPrincipal().String(),
		Scope:          string(ScopeMCPAuth),
		Selectors:      raw,
		Kind:           AuthPermissionKindCreationDefault,
	}); err != nil {
		return fmt.Errorf("record personal auth client default: %w", err)
	}
	return nil
}

// MarkAuthPermissionTakenOverTx records that an explicit write took a grant
// over from the default auth permission backfill. Call it after the write.
func MarkAuthPermissionTakenOverTx(ctx context.Context, dbtx repo.DBTX, organizationID, principalURN, scope string, selectors []byte) error {
	if err := repo.New(dbtx).MarkAuthPermissionTakenOver(ctx, repo.MarkAuthPermissionTakenOverParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principalURN,
		Scope:          scope,
		Selectors:      selectors,
	}); err != nil {
		return fmt.Errorf("mark auth permission taken over: %w", err)
	}
	return nil
}

// LockAuthPermissionBackfillTx waits for any running auth client backfill in
// the organization and keeps new runs out until the transaction ends, so a
// run never acts on a plan made before the transaction's writes. Take it
// before the transaction writes anything.
func LockAuthPermissionBackfillTx(ctx context.Context, dbtx repo.DBTX, organizationID string) error {
	if err := repo.New(dbtx).LockAuthPermissionBackfill(ctx, organizationID); err != nil {
		return fmt.Errorf("lock auth permission backfill: %w", err)
	}
	return nil
}

// TakeOverPrincipalPolicyTx records that an explicit write replaced
// principal's whole policy, even with an empty one: every grant the backfill
// provisioned for it is taken over, and a marker row keeps a principal with
// no provisioning history from reading as untouched. The transaction must
// hold LockAuthPermissionBackfillTx.
func TakeOverPrincipalPolicyTx(ctx context.Context, dbtx repo.DBTX, organizationID string, principal urn.Principal) error {
	q := repo.New(dbtx)
	if err := q.MarkPrincipalAuthPermissionsTakenOver(ctx, repo.MarkPrincipalAuthPermissionsTakenOverParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principal.String(),
	}); err != nil {
		return fmt.Errorf("mark principal auth permissions taken over: %w", err)
	}
	// The marker names every MCP server in the organization, a grant the
	// backfill never writes since it scopes each one to a project.
	raw, err := Selector{SelectorKeyResourceKind: ResourceKindMCP, SelectorKeyResourceID: WildcardResource}.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal policy takeover marker: %w", err)
	}
	if err := q.RecordAuthPermissionPolicyTakeover(ctx, repo.RecordAuthPermissionPolicyTakeoverParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principal.String(),
		Scope:          string(ScopeMCPConnect),
		Selectors:      raw,
		Kind:           AuthPermissionKindExplicitPolicy,
	}); err != nil {
		return fmt.Errorf("record policy takeover: %w", err)
	}
	return nil
}

// upsertGrant writes an explicit grant and takes it over from the backfill.
func upsertGrant(ctx context.Context, q *repo.Queries, params repo.UpsertPrincipalGrantParams) error {
	if _, err := q.UpsertPrincipalGrant(ctx, params); err != nil {
		return fmt.Errorf("upsert grant: %w", err)
	}
	if err := q.MarkAuthPermissionTakenOver(ctx, repo.MarkAuthPermissionTakenOverParams{
		OrganizationID: params.OrganizationID,
		PrincipalUrn:   params.PrincipalUrn.String(),
		Scope:          params.Scope,
		Selectors:      params.Selectors,
	}); err != nil {
		return fmt.Errorf("mark auth permission taken over: %w", err)
	}
	return nil
}

// InsertPrincipalGrantsIfAbsentTx adds grants to principal, leaving any it
// already holds untouched, and returns how many it added.
func InsertPrincipalGrantsIfAbsentTx(ctx context.Context, dbtx repo.DBTX, organizationID string, principal urn.Principal, grants []*RoleGrant) (int64, error) {
	if organizationID == "" {
		return 0, fmt.Errorf("organization id is required")
	}
	if _, err := principal.Value(); err != nil {
		return 0, fmt.Errorf("invalid grant principal: %w", err)
	}
	rows, err := flattenRoleGrants(grants)
	if err != nil {
		return 0, err
	}
	q := repo.New(dbtx)
	var inserted int64
	for _, row := range rows {
		n, err := q.InsertPrincipalGrantIfAbsent(ctx, repo.InsertPrincipalGrantIfAbsentParams{
			OrganizationID: organizationID,
			PrincipalUrn:   principal,
			Scope:          string(row.Scope),
			Selectors:      row.SelectorRaw,
		})
		if err != nil {
			return inserted, fmt.Errorf("insert grant %q for %s: %w", row.Scope, principal.Label(), err)
		}
		inserted += n
	}
	return inserted, nil
}
