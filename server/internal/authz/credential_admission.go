package authz

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// AdmitPrincipalCredential performs authoritative parent admission and loads
// immutable credential policy R, live direct agent policy A, and the current
// owner's live policy O as three independent policy sets. The caller must first
// load and validate the directly active credential row, stamp its immutable
// profile with contextvalues.WithPrincipalCredentialAuthorization, and call
// this method before minting credentials, resolving upstream authority, or
// executing an operation. Successful results must not be cached across requests.
func (e *Engine) AdmitPrincipalCredential(ctx context.Context) (context.Context, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	credential, hasCredential := contextvalues.PrincipalCredentialAuthorization(ctx)
	actor, hasActor := contextvalues.AuthenticatedActor(ctx)
	if !ok || authCtx == nil || !hasCredential || !hasActor ||
		authCtx.ActiveOrganizationID == "" || credential.AuthorizerUserID == "" || actor.Type != urn.PrincipalTypeAgent {
		return ctx, oops.C(oops.CodeUnauthorized)
	}

	policy, err := DecodeDelegatedPolicy(DelegatedPolicyVersion(credential.DelegatedGrantsVersion), credential.DelegatedGrants)
	if err != nil {
		if errors.Is(err, ErrInvalidDelegatedPolicy) {
			return ctx, oops.C(oops.CodeUnauthorized)
		}
		return ctx, fmt.Errorf("decode delegated credential policy: %w", err)
	}

	tx, err := e.db.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly, DeferrableMode: pgx.NotDeferrable, BeginQuery: "", CommitQuery: "",
	})
	if err != nil {
		return ctx, fmt.Errorf("begin credential admission snapshot: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	if mode, hasMode := contextvalues.APIKeyAuthorization(ctx); hasMode && mode == contextvalues.APIKeyAuthorizationModePrincipal {
		apiKeyID, parseErr := uuid.Parse(authCtx.APIKeyID)
		if parseErr != nil {
			return ctx, oops.C(oops.CodeUnauthorized)
		}
		_, err = keysrepo.New(tx).GetActivePrincipalAPIKeyForAdmission(ctx, keysrepo.GetActivePrincipalAPIKeyForAdmissionParams{
			ID:                     apiKeyID,
			OrganizationID:         authCtx.ActiveOrganizationID,
			SubjectUrn:             pgtype.Text{String: actor.String(), Valid: true},
			AuthorizerUserID:       credential.AuthorizerUserID,
			DelegatedGrants:        credential.DelegatedGrants,
			DelegatedGrantsVersion: pgtype.Int4{Int32: credential.DelegatedGrantsVersion, Valid: true},
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ctx, oops.C(oops.CodeUnauthorized)
		}
		if err != nil {
			return ctx, fmt.Errorf("revalidate principal API key: %w", err)
		}
	}

	agent, err := agents.ResolvePrincipal(ctx, tx, authCtx.ActiveOrganizationID, actor)
	if err != nil {
		if errors.Is(err, agents.ErrPrincipalInvalid) || errors.Is(err, agents.ErrPrincipalNotFound) {
			return ctx, oops.C(oops.CodeUnauthorized)
		}
		return ctx, fmt.Errorf("resolve credential parent: %w", err)
	}
	if agents.DeriveLifecycle(agent) != agents.LifecycleActive || agent.OwnerReassignmentRequiredAt.Valid {
		return ctx, oops.C(oops.CodeUnauthorized)
	}

	ownerPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, agent.OwnerUserID)
	ownerPrincipals, err := ResolveUserPrincipals(ctx, tx, authCtx.ActiveOrganizationID, agent.OwnerUserID)
	if err != nil {
		if errors.Is(err, ErrPrincipalInvalid) || errors.Is(err, ErrPrincipalNotFound) {
			return ctx, oops.C(oops.CodeUnauthorized)
		}
		return ctx, fmt.Errorf("resolve credential owner: %w", err)
	}
	ownerEligible := false
	for _, principal := range ownerPrincipals {
		if principal.String() == ownerPrincipal.String() {
			ownerEligible = true
			break
		}
	}
	if !ownerEligible {
		return ctx, oops.C(oops.CodeUnauthorized)
	}

	agentPolicy, err := loadResolvedAgentPolicy(ctx, tx, authCtx.ActiveOrganizationID, actor)
	if err != nil {
		return ctx, fmt.Errorf("load live agent policy: %w", err)
	}
	ownerPolicy, err := LoadGrants(ctx, tx, authCtx.ActiveOrganizationID, ownerPrincipals)
	if err != nil {
		return ctx, fmt.Errorf("load live owner policy: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return ctx, fmt.Errorf("commit credential admission snapshot: %w", err)
	}

	ctx = contextvalues.WithPrincipalCredentialOwner(ctx, agent.OwnerUserID)
	return principalCredentialPoliciesToContext(ctx, policy.RuntimeGrants(), agentPolicy, ownerPolicy), nil
}
