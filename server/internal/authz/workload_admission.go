package authz

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// WorkloadSessionAdmission contains the policy sets a workload session acts
// under, loaded independently and never cached across requests.
//
// Each operation must satisfy the immutable session ceiling, the assigned
// agent's live policy, and the current owner's live effective permissions.
type WorkloadSessionAdmission struct {
	// AgentPrincipal is the agent the workload inherited from. Admission
	// refuses a result that names none.
	AgentPrincipal string

	// OwnerUserID currently owns that agent and supplies the owner policy.
	OwnerUserID string

	// Ceiling is the session's immutable policy R, fixed at issuance.
	Ceiling []Grant

	// Agent is the assigned agent's live policy A.
	Agent []Grant

	// Owner is the current owner's live effective policy O.
	Owner []Grant
}

// WorkloadSessionAdmitter supplies application-owned workload admission without
// making the generic authorization engine depend on agent policy.
type WorkloadSessionAdmitter func(context.Context, *pgxpool.Pool) (WorkloadSessionAdmission, error)

// AdmitWorkloadSession fails closed unless application-owned admission is
// configured and succeeds, then preserves R, A, and O as independent policies.
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
	agent, err := urn.ParsePrincipal(admission.AgentPrincipal)
	if err != nil || agent.Type != urn.PrincipalTypeAgent {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	// Record the agent whose policy was admitted, so every later use reads this
	// one value instead of resolving the assignment again.
	ctx = contextvalues.WithWorkloadAgent(ctx, agent)
	ctx = contextvalues.WithPrincipalCredentialOwner(ctx, admission.OwnerUserID)
	return admittedPoliciesToContext(ctx, RepresentedUserPolicy{UserID: "", Policy: nil}, admission.Ceiling, admission.Agent, admission.Owner), nil
}
