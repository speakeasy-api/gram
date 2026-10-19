package runtimepolicy

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

// AdmitWorkloadSession admits a workload session and loads three independent
// policy sets: the session's ceiling R, the assigned agent's live policy A, and
// its current owner's live policy O. R is the session's stored delegated
// policy, or, for a credential that stores none, the ceiling derived from A.
// The caller must have stamped the workload actor and the session's credential
// profile first. Results must not be cached across requests.
func AdmitWorkloadSession(ctx context.Context, db *pgxpool.Pool) (authz.WorkloadSessionAdmission, error) {
	if !hasWorkloadSessionAdmissionContext(ctx) {
		return authz.WorkloadSessionAdmission{}, oops.C(oops.CodeUnauthorized)
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly, DeferrableMode: pgx.NotDeferrable, BeginQuery: "", CommitQuery: "",
	})
	if err != nil {
		return authz.WorkloadSessionAdmission{}, fmt.Errorf("begin workload admission snapshot: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	admission, err := admitWorkloadSessionInTx(ctx, tx)
	if err != nil {
		return authz.WorkloadSessionAdmission{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return authz.WorkloadSessionAdmission{}, fmt.Errorf("commit workload admission snapshot: %w", err)
	}

	return admission, nil
}

// admitWorkloadSessionInTx reads every row admission depends on from one
// snapshot.
//
// Every refusal is the same unauthorized error. A workload with no assignment,
// an assignment to a deleted agent, one to a suspended or revoked agent, and one
// naming another organization's agent are indistinguishable to the caller, for
// the reason agents.ResolvePrincipal collapses its own cases: a machine probing
// this path must not learn which part of the configuration it tripped on.
func admitWorkloadSessionInTx(ctx context.Context, tx accessrepo.DBTX) (authz.WorkloadSessionAdmission, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	credential, hasCredential := contextvalues.PrincipalCredentialAuthorization(ctx)
	actor, hasActor := contextvalues.AuthenticatedActor(ctx)
	// No AuthorizerUserID requirement, unlike a principal credential: a
	// workload records no approving human, so demanding one here would refuse
	// every workload session.
	if !ok || authCtx == nil || !hasCredential || !hasActor ||
		authCtx.ActiveOrganizationID == "" || actor.Type != urn.PrincipalTypeWorkload {
		return authz.WorkloadSessionAdmission{}, oops.C(oops.CodeUnauthorized)
	}

	workloadIssuerID, subject, err := actor.Workload()
	if err != nil {
		return authz.WorkloadSessionAdmission{}, oops.C(oops.CodeUnauthorized)
	}

	var storedCeiling []authz.Grant
	if credential.DerivesPolicyAtAdmission {
		if len(credential.DelegatedGrants) != 0 {
			return authz.WorkloadSessionAdmission{}, oops.C(oops.CodeUnauthorized)
		}
	} else {
		policy, err := DecodeDelegatedPolicy(DelegatedPolicyVersion(credential.DelegatedGrantsVersion), credential.DelegatedGrants)
		if err != nil {
			if errors.Is(err, ErrInvalidDelegatedPolicy) {
				return authz.WorkloadSessionAdmission{}, oops.C(oops.CodeUnauthorized)
			}
			return authz.WorkloadSessionAdmission{}, fmt.Errorf("decode workload session policy: %w", err)
		}
		storedCeiling = policy.RuntimeGrants()
	}

	agentID, assigned, err := workloadidentity.ResolveAssignedAgent(ctx, tx, workloadidentity.AssignmentParams{
		OrganizationID:   authCtx.ActiveOrganizationID,
		WorkloadIssuerID: workloadIssuerID,
		Subject:          subject,
	})
	if err != nil {
		return authz.WorkloadSessionAdmission{}, fmt.Errorf("resolve workload agent assignment: %w", err)
	}
	if !assigned {
		// Authenticated, admitted, and authorized for nothing.
		return authz.WorkloadSessionAdmission{}, oops.C(oops.CodeUnauthorized)
	}

	agentPrincipal := urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String())
	agent, err := agents.ResolvePrincipal(ctx, tx, authCtx.ActiveOrganizationID, agentPrincipal)
	if err != nil {
		if errors.Is(err, agents.ErrPrincipalInvalid) || errors.Is(err, agents.ErrPrincipalNotFound) {
			return authz.WorkloadSessionAdmission{}, oops.C(oops.CodeUnauthorized)
		}
		return authz.WorkloadSessionAdmission{}, fmt.Errorf("resolve assigned agent: %w", err)
	}
	if agents.DeriveLifecycle(agent) != agents.LifecycleActive || agent.OwnerReassignmentRequiredAt.Valid {
		return authz.WorkloadSessionAdmission{}, oops.C(oops.CodeUnauthorized)
	}

	ownerPrincipals, ownerEligible, err := ResolveEligibleUser(ctx, tx, authCtx.ActiveOrganizationID, agent.OwnerUserID)
	if err != nil {
		return authz.WorkloadSessionAdmission{}, fmt.Errorf("resolve assigned agent owner: %w", err)
	}
	if !ownerEligible {
		return authz.WorkloadSessionAdmission{}, oops.C(oops.CodeUnauthorized)
	}

	agentPolicy, err := LoadAgentPolicy(ctx, tx, authCtx.ActiveOrganizationID, agentPrincipal)
	if err != nil {
		return authz.WorkloadSessionAdmission{}, fmt.Errorf("load live agent policy: %w", err)
	}
	ownerPolicy, err := authz.LoadGrants(ctx, tx, authCtx.ActiveOrganizationID, ownerPrincipals)
	if err != nil {
		return authz.WorkloadSessionAdmission{}, fmt.Errorf("load live workload owner policy: %w", err)
	}

	ceiling := storedCeiling
	if credential.DerivesPolicyAtAdmission {
		ceiling, err = DelegatedRuntimeGrants(agentPolicy, agentPolicy, agentPolicy)
		if err != nil {
			return authz.WorkloadSessionAdmission{}, fmt.Errorf("derive workload ceiling: %w", err)
		}
	}

	return authz.WorkloadSessionAdmission{
		AgentPrincipal: agentPrincipal.String(),
		OwnerUserID:    agent.OwnerUserID,
		Ceiling:        ceiling,
		Agent:          agentPolicy,
		Owner:          ownerPolicy,
	}, nil
}

func hasWorkloadSessionAdmissionContext(ctx context.Context) bool {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	_, hasCredential := contextvalues.PrincipalCredentialAuthorization(ctx)
	actor, hasActor := contextvalues.AuthenticatedActor(ctx)
	return ok && authCtx != nil && hasCredential && hasActor &&
		authCtx.ActiveOrganizationID != "" && actor.Type == urn.PrincipalTypeWorkload
}
