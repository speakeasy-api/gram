package authz

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// WorkloadSessionAdmission contains the policy sets a workload session acts
// under, loaded independently and never cached across requests.
//
// The owner's policy is not inherited. When a credential records a delegating
// human, their live policy further restricts the workload's authority.
type WorkloadSessionAdmission struct {
	// AgentPrincipal is the agent the workload inherited from. Admission
	// refuses a result that names none.
	AgentPrincipal string
	// OwnerUserID owns that agent. Recorded as credential owner provenance for
	// attribution only; the owner's own policy is never evaluated.
	OwnerUserID string
	// Ceiling is the session's immutable policy R, fixed at issuance.
	Ceiling []Grant
	// Agent is the assigned agent's live policy A.
	Agent []Grant
	// Authorizer is evaluated only when AuthorizerUserID is present. An empty
	// selected-human policy denies; absence means autonomous execution.
	AuthorizerUserID string
	Authorizer       []Grant
}

// WorkloadSessionAdmitter supplies application-owned workload admission without
// making the generic authorization engine depend on agent policy.
type WorkloadSessionAdmitter func(context.Context, *pgxpool.Pool) (WorkloadSessionAdmission, error)

// AdmitWorkloadSession fails closed unless application-owned admission is
// configured and succeeds, then preserves R and A as independent policies.
func (e *Engine) AdmitWorkloadSession(ctx context.Context) (context.Context, error) {
	if e.admitWorkloadSession == nil {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	admission, err := e.admitWorkloadSession(ctx, e.db)
	if err != nil {
		return ctx, err
	}
	// An admission naming no agent authorizes nothing. Checked here as well as
	// in the admitter so a future admitter cannot widen the boundary by
	// returning a zero value.
	if admission.AgentPrincipal == "" || admission.OwnerUserID == "" {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	ctx = contextvalues.WithPrincipalCredentialOwner(ctx, admission.OwnerUserID)
	// Intersect ceiling and agent policies, plus human policy for delegated
	// sessions. No owner set: an empty owner set would deny every check.
	if admission.AuthorizerUserID != "" {
		return admittedPoliciesToContext(ctx, admission.Ceiling, admission.Agent, admission.Authorizer), nil
	}
	return admittedPoliciesToContext(ctx, admission.Ceiling, admission.Agent), nil
}
