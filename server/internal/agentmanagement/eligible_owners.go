package agentmanagement

import (
	"context"
	"slices"
	"strings"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

const (
	// defaultEligibleOwnersLimit keeps owner selection bounded without extra input.
	defaultEligibleOwnersLimit = 50
	// maxEligibleOwnersLimit caps profile data returned by a single request.
	maxEligibleOwnersLimit = 100
	// maxEligibleOwnersCursorLength bounds an opaque user ID supplied as a cursor.
	maxEligibleOwnersCursorLength = 1024
	// maxEligibleOwnersSearchLength bounds display-name search input.
	maxEligibleOwnersSearchLength = 200
)

// ListEligibleOwners reveals only active members the caller may provision for,
// without granting access to the organization's general member directory.
func (s *Service) ListEligibleOwners(ctx context.Context, payload *gen.ListEligibleOwnersPayload) (*gen.ListEligibleOwnersResult, error) {
	human, err := s.authorizer.RequireHuman(ctx, s.db)
	if err != nil {
		return nil, s.serviceError(ctx, err, "list eligible agent owners")
	}
	if !slices.ContainsFunc(human.grants, func(grant authz.Grant) bool {
		return grant.Scope == authz.ScopeAgentWrite || grant.Scope == authz.ScopeRoot
	}) {
		return nil, oops.C(oops.CodeForbidden)
	}
	limit := payload.Limit
	if limit == 0 {
		limit = defaultEligibleOwnersLimit
	}
	search := conv.PtrValOr(payload.Search, "")
	if limit < 1 || limit > maxEligibleOwnersLimit || len(search) > maxEligibleOwnersSearchLength || len(conv.PtrValOr(payload.Cursor, "")) > maxEligibleOwnersCursorLength {
		return nil, oops.C(oops.CodeBadRequest)
	}
	search = strings.TrimSpace(search)
	allOwners, ownerIDs := eligibleOwnerTargets(human.grants, human.Auth.UserID)
	rows, err := orgrepo.New(s.db).ListEligibleAgentOwners(ctx, orgrepo.ListEligibleAgentOwnersParams{
		OrganizationID: human.Auth.ActiveOrganizationID,
		AllOwners:      allOwners, OwnerUserIds: ownerIDs,
		Cursor: conv.PtrToPGText(payload.Cursor), Search: search, LimitValue: int32(limit + 1),
	})
	if err != nil {
		return nil, s.serviceError(ctx, err, "list eligible agent owners")
	}
	result := &gen.ListEligibleOwnersResult{Owners: make([]*gen.AgentEligibleOwner, 0, len(rows)), NextCursor: nil}
	if len(rows) > limit {
		rows = rows[:limit]
		result.NextCursor = new(rows[len(rows)-1].UserID)
	}
	for _, row := range rows {
		result.Owners = append(result.Owners, &gen.AgentEligibleOwner{UserID: row.UserID, DisplayName: row.DisplayName})
	}
	return result, nil
}

// eligibleOwnerTargets compiles owner-scoped write grants into SQL
// constraints. Agent write has no exclusion scope; strict containment proves
// authority for both wildcard and concrete owners.
func eligibleOwnerTargets(grants []authz.Grant, callerID string) (bool, []string) {
	grants = authz.BindSelectorValue(grants, authz.SelectorKeyOwner, authz.AgentOwnerSelf, callerID)
	if authz.GrantsContainSelector(grants, authz.ScopeAgentWrite, authz.AgentOwnerSelector(authz.ScopeAgentWrite, authz.WildcardResource)) {
		return true, []string{}
	}
	owners := make([]string, 0)
	for _, grant := range grants {
		if grant.Scope != authz.ScopeAgentWrite && grant.Scope != authz.ScopeRoot {
			continue
		}
		ownerID := grant.Selector[authz.SelectorKeyOwner]
		if ownerID == "" || ownerID == authz.WildcardResource || slices.Contains(owners, ownerID) {
			continue
		}
		if authz.GrantsContainSelector(grants, authz.ScopeAgentWrite, authz.AgentOwnerSelector(authz.ScopeAgentWrite, ownerID)) {
			owners = append(owners, ownerID)
		}
	}
	return false, owners
}
