package keys

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ExtendAgentKeys adds server grants to the live keys the caller issued for
// an agent. A key stores the grants it was issued with, so a server the agent
// is given later never reaches the key on its own: without this, adding a
// server to an agent leaves its gateway exactly as it was until a new key is
// issued and swapped into the runtime.
//
// Only the caller's own keys change: a key acts for the user who issued it,
// and this caller's authority says nothing about what someone else's key may
// reach. Each new policy is checked like issuance, against the agent's, its
// owner's and the caller's live policies.
func (s *Service) ExtendAgentKeys(ctx context.Context, payload *gen.ExtendAgentKeysPayload) (*gen.ListKeysResult, error) {
	if err := s.requireAgentCredentialsHuman(ctx); err != nil {
		return nil, err
	}
	agentID, err := parseAgentID(payload.AgentID)
	if err != nil {
		return nil, err
	}
	additions, err := connectGrantsFromForms(payload.RequestedGrants)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "access agent API keys").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	bounds, err := s.loadAgentKeyBounds(ctx, tx, agentID)
	if err != nil {
		return nil, err
	}
	human := bounds.human
	subjectURN := agentSubjectURN(agentID)
	kr := repo.New(tx)
	rows, err := kr.ListAgentAPIKeys(ctx, repo.ListAgentAPIKeysParams{
		OrganizationID: human.Auth.ActiveOrganizationID,
		SubjectUrn:     conv.ToPGText(subjectURN),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list agent API keys").LogError(ctx, s.logger)
	}

	now := time.Now()
	extended := make([]*gen.Key, 0, len(rows))
	for _, row := range rows {
		if row.CreatedByUserID != human.Auth.UserID || !row.DelegatedGrantsVersion.Valid || !row.ExpiresAt.Valid || !row.ExpiresAt.Time.After(now) {
			continue
		}
		version := runtimepolicy.DelegatedPolicyVersion(row.DelegatedGrantsVersion.Int32)
		current, err := runtimepolicy.DecodeDelegatedPolicy(version, row.DelegatedGrants)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "decode agent API key policy").LogError(ctx, s.logger)
		}
		requested, changed := withGrants(current.Requested, additions)
		if !changed {
			continue
		}
		policy, err := runtimepolicy.NewDelegatedPolicy(version, requested)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid delegated grants")
		}
		if err := s.requireWithinAgentKeyBounds(ctx, bounds, policy); err != nil {
			return nil, err
		}
		for _, grant := range policy.Effective {
			if err := authz.RequireUpstreamSelectorWrites(ctx, human.Auth.ActiveOrganizationID, grant.Selector); err != nil {
				return nil, oops.E(oops.CodeForbidden, err, "check upstream_client write gate")
			}
		}
		policyJSON, err := runtimepolicy.EncodeDelegatedPolicy(version, policy)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid delegated grants")
		}

		updated, err := kr.ExtendAgentAPIKeyDelegatedGrants(ctx, repo.ExtendAgentAPIKeyDelegatedGrantsParams{
			DelegatedGrants:         policyJSON,
			ID:                      row.ID,
			OrganizationID:          human.Auth.ActiveOrganizationID,
			SubjectUrn:              conv.ToPGText(subjectURN),
			CreatedByUserID:         human.Auth.UserID,
			PreviousDelegatedGrants: row.DelegatedGrants,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeConflict, err, "the key %q changed while its servers were being updated; try again", row.Name)
		}
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "extend agent API key").LogError(ctx, s.logger)
		}
		if err := s.audit.LogKeyPolicyUpdate(ctx, tx, audit.LogKeyPolicyUpdateEvent{
			OrganizationID:     human.Auth.ActiveOrganizationID,
			ProjectID:          row.ProjectID,
			Actor:              urn.NewPrincipal(urn.PrincipalTypeUser, human.Auth.UserID),
			ActorDisplayName:   human.Auth.Email,
			ActorSlug:          nil,
			KeyURN:             urn.NewAPIKey(updated.ID),
			KeyName:            updated.Name,
			PolicyGrantsBefore: row.DelegatedGrants,
			PolicyGrantsAfter:  policyJSON,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "add agent API key update audit log").LogError(ctx, s.logger)
		}
		key, err := agentKeyModel(updated, nil)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "decode agent API key").LogError(ctx, s.logger)
		}
		extended = append(extended, key)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "save extended agent API keys").LogError(ctx, s.logger)
	}
	return &gen.ListKeysResult{Keys: extended}, nil
}

// connectGrantsFromForms accepts only what the agent editor grants a server:
// an mcp:connect allow on servers. Anything broader is a new delegation and
// goes through issuing a key, where the person sees what it carries.
func connectGrantsFromForms(forms []*gen.AgentPolicyGrantForm) ([]authz.Grant, error) {
	if len(forms) == 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "at least one grant is required")
	}
	grants := make([]authz.Grant, 0, len(forms))
	for _, form := range forms {
		if form == nil || form.Selector == nil {
			return nil, oops.E(oops.CodeBadRequest, runtimepolicy.ErrInvalidDelegatedPolicy, "grant and selector are required")
		}
		if form.Effect != "allow" || authz.Scope(form.Scope) != authz.ScopeMCPConnect || form.Selector.ResourceKind != "mcp" {
			return nil, oops.E(oops.CodeBadRequest, runtimepolicy.ErrInvalidDelegatedPolicy, "only mcp:connect allow grants on MCP servers can be added to existing keys")
		}
		grants = append(grants, authz.NewGrantWithSelector(authz.ScopeMCPConnect, selectorFromForm(form.Selector)))
	}
	return grants, nil
}

// withGrants returns requested plus every addition it does not already hold,
// and whether anything was added.
func withGrants(requested []runtimepolicy.DelegatedPolicyGrant, additions []authz.Grant) ([]authz.Grant, bool) {
	merged := make([]authz.Grant, 0, len(requested)+len(additions))
	for _, grant := range requested {
		merged = append(merged, authz.NewGrantWithSelector(grant.Scope, grant.Selector))
	}
	changed := false
	for _, addition := range additions {
		held := slices.ContainsFunc(merged, func(grant authz.Grant) bool {
			return grant.Scope == addition.Scope && (maps.Equal(grant.Selector, addition.Selector) || coversProjectServers(grant.Selector, addition.Selector))
		})
		if held {
			continue
		}
		merged = append(merged, addition)
		changed = true
	}
	return merged, changed
}

// coversProjectServers reports whether held is an "All servers" grant that
// already reaches the server named by want. Such a key follows servers added
// to its project on its own, so naming one beside the wildcard adds nothing.
func coversProjectServers(held, want authz.Selector) bool {
	if held[authz.SelectorKeyResourceID] != "*" || held[authz.SelectorKeyResourceKind] != want[authz.SelectorKeyResourceKind] {
		return false
	}
	project, scoped := held[authz.SelectorKeyProjectID]
	return !scoped || project == want[authz.SelectorKeyProjectID]
}
