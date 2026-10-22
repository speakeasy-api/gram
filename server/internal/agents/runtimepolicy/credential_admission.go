package runtimepolicy

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// AdmitPrincipalCredential performs authoritative parent admission and loads
// credential policy R, live direct agent policy A, and the current owner's live
// policy O as three independent policy sets, plus the authorizing user's live
// policy, which also bounds auth-client checks. R is the credential's stored
// delegated policy, or, for a credential that stores none, the policy derived
// from A, O, and the authorizing user's live policy. The caller must first
// load and validate the credential, stamp its profile with
// contextvalues.WithPrincipalCredentialAuthorization, and call this function
// before minting credentials, resolving upstream authority, or executing an
// operation. Successful results must not be cached across requests.
func AdmitPrincipalCredential(ctx context.Context, db *pgxpool.Pool) (authz.PrincipalCredentialAdmission, error) {
	if !hasPrincipalCredentialAdmissionContext(ctx) {
		return authz.PrincipalCredentialAdmission{}, oops.C(oops.CodeUnauthorized)
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly, DeferrableMode: pgx.NotDeferrable, BeginQuery: "", CommitQuery: "",
	})
	if err != nil {
		return authz.PrincipalCredentialAdmission{}, fmt.Errorf("begin credential admission snapshot: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	admission, err := AdmitPrincipalCredentialWithDBTX(ctx, tx)
	if err != nil {
		return authz.PrincipalCredentialAdmission{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return authz.PrincipalCredentialAdmission{}, fmt.Errorf("commit credential admission snapshot: %w", err)
	}

	return admission, nil
}

// AdmitPrincipalCredentialWithDBTX performs live admission in the caller-owned
// transaction so refresh admission and credential rotation share one snapshot.
func AdmitPrincipalCredentialWithDBTX(ctx context.Context, tx accessrepo.DBTX) (authz.PrincipalCredentialAdmission, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	credential, hasCredential := contextvalues.PrincipalCredentialAuthorization(ctx)
	actor, hasActor := contextvalues.AuthenticatedActor(ctx)
	if !ok || authCtx == nil || !hasCredential || !hasActor ||
		authCtx.ActiveOrganizationID == "" || credential.AuthorizerUserID == "" || actor.Type != urn.PrincipalTypeAgent {
		return authz.PrincipalCredentialAdmission{}, oops.C(oops.CodeUnauthorized)
	}

	var storedPolicy []authz.Grant
	if credential.DerivesPolicyAtAdmission {
		if len(credential.DelegatedGrants) != 0 {
			return authz.PrincipalCredentialAdmission{}, oops.C(oops.CodeUnauthorized)
		}
	} else {
		policy, err := DecodeDelegatedPolicy(DelegatedPolicyVersion(credential.DelegatedGrantsVersion), credential.DelegatedGrants)
		if err != nil {
			if errors.Is(err, ErrInvalidDelegatedPolicy) {
				return authz.PrincipalCredentialAdmission{}, oops.C(oops.CodeUnauthorized)
			}
			return authz.PrincipalCredentialAdmission{}, fmt.Errorf("decode delegated credential policy: %w", err)
		}
		storedPolicy = policy.RuntimeGrants()
	}

	if mode, hasMode := contextvalues.APIKeyAuthorization(ctx); hasMode && mode == contextvalues.APIKeyAuthorizationModePrincipal {
		// An API key row always stores its delegated policy.
		if credential.DerivesPolicyAtAdmission {
			return authz.PrincipalCredentialAdmission{}, oops.C(oops.CodeUnauthorized)
		}
		apiKeyID, parseErr := uuid.Parse(authCtx.APIKeyID)
		if parseErr != nil {
			return authz.PrincipalCredentialAdmission{}, oops.C(oops.CodeUnauthorized)
		}
		_, err := keysrepo.New(tx).GetActivePrincipalAPIKeyForAdmission(ctx, keysrepo.GetActivePrincipalAPIKeyForAdmissionParams{
			ID:                     apiKeyID,
			OrganizationID:         authCtx.ActiveOrganizationID,
			SubjectUrn:             pgtype.Text{String: actor.String(), Valid: true},
			AuthorizerUserID:       credential.AuthorizerUserID,
			DelegatedGrants:        credential.DelegatedGrants,
			DelegatedGrantsVersion: pgtype.Int4{Int32: credential.DelegatedGrantsVersion, Valid: true},
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return authz.PrincipalCredentialAdmission{}, oops.C(oops.CodeUnauthorized)
		}
		if err != nil {
			return authz.PrincipalCredentialAdmission{}, fmt.Errorf("revalidate principal API key: %w", err)
		}
	}

	agent, err := agents.ResolvePrincipal(ctx, tx, authCtx.ActiveOrganizationID, actor)
	if err != nil {
		if errors.Is(err, agents.ErrPrincipalInvalid) || errors.Is(err, agents.ErrPrincipalNotFound) {
			return authz.PrincipalCredentialAdmission{}, oops.C(oops.CodeUnauthorized)
		}
		return authz.PrincipalCredentialAdmission{}, fmt.Errorf("resolve credential parent: %w", err)
	}
	if agents.DeriveLifecycle(agent) != agents.LifecycleActive || agent.OwnerReassignmentRequiredAt.Valid {
		return authz.PrincipalCredentialAdmission{}, oops.C(oops.CodeUnauthorized)
	}

	ownerPrincipals, ownerEligible, err := ResolveEligibleUser(ctx, tx, authCtx.ActiveOrganizationID, agent.OwnerUserID)
	if err != nil {
		return authz.PrincipalCredentialAdmission{}, fmt.Errorf("resolve credential owner: %w", err)
	}
	if !ownerEligible {
		return authz.PrincipalCredentialAdmission{}, oops.C(oops.CodeUnauthorized)
	}

	agentPolicy, err := LoadAgentPolicy(ctx, tx, authCtx.ActiveOrganizationID, actor)
	if err != nil {
		return authz.PrincipalCredentialAdmission{}, fmt.Errorf("load live agent policy: %w", err)
	}
	ownerPolicy, err := authz.LoadGrants(ctx, tx, authCtx.ActiveOrganizationID, ownerPrincipals)
	if err != nil {
		return authz.PrincipalCredentialAdmission{}, fmt.Errorf("load live owner policy: %w", err)
	}

	// The authorizer is the user the credential acts for. Their live policy
	// bounds auth-client use, and derives R for credentials storing none.
	authorizerPolicy := ownerPolicy
	if credential.AuthorizerUserID != agent.OwnerUserID {
		authorizerPrincipals, authorizerEligible, err := ResolveEligibleUser(ctx, tx, authCtx.ActiveOrganizationID, credential.AuthorizerUserID)
		if err != nil {
			return authz.PrincipalCredentialAdmission{}, fmt.Errorf("resolve credential authorizer: %w", err)
		}
		switch {
		case authorizerEligible:
			authorizerPolicy, err = authz.LoadGrants(ctx, tx, authCtx.ActiveOrganizationID, authorizerPrincipals)
			if err != nil {
				return authz.PrincipalCredentialAdmission{}, fmt.Errorf("load live authorizer policy: %w", err)
			}
		case credential.DerivesPolicyAtAdmission:
			return authz.PrincipalCredentialAdmission{}, oops.C(oops.CodeUnauthorized)
		default:
			// A stored policy admits on its own as before; an ineligible
			// authorizer only leaves no auth client usable.
			authorizerPolicy = []authz.Grant{}
		}
	}

	credentialPolicy := storedPolicy
	if credential.DerivesPolicyAtAdmission {
		credentialPolicy, err = DelegatedRuntimeGrantsFor(credential.AuthorizerUserID, agentPolicy, ownerPolicy, authorizerPolicy)
		if err != nil {
			return authz.PrincipalCredentialAdmission{}, fmt.Errorf("derive credential policy: %w", err)
		}
	}

	return authz.PrincipalCredentialAdmission{
		OwnerUserID: agent.OwnerUserID,
		Credential:  credentialPolicy,
		Agent:       agentPolicy,
		Owner:       ownerPolicy,
		Authorizer:  authz.RepresentedUserPolicy{UserID: credential.AuthorizerUserID, Policy: authorizerPolicy},
	}, nil
}

func hasPrincipalCredentialAdmissionContext(ctx context.Context) bool {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	credential, hasCredential := contextvalues.PrincipalCredentialAuthorization(ctx)
	actor, hasActor := contextvalues.AuthenticatedActor(ctx)
	return ok && authCtx != nil && hasCredential && hasActor && authCtx.ActiveOrganizationID != "" && credential.AuthorizerUserID != "" && actor.Type == urn.PrincipalTypeAgent
}
