package authz

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

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
// default unless it already holds it.
func SeedPersonalAuthClientDefaultTx(ctx context.Context, dbtx repo.DBTX, organizationID string) error {
	grant := &RoleGrant{Scope: string(ScopeMCPAuth), Selectors: []Selector{PersonalAuthClientSelector()}}
	if _, err := InsertPrincipalGrantsIfAbsentTx(ctx, dbtx, organizationID, AllUsersPrincipal(), []*RoleGrant{grant}); err != nil {
		return fmt.Errorf("seed personal auth client default: %w", err)
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
