package authz

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// WorkloadSessionAdmission contains the policy sets a workload session acts
// under, loaded independently and never cached across requests.
//
// Two sets rather than the three a principal credential carries. A workload
// holds no grants of its own and inherits the live policy of the agent assigned
// to it, so there is no separate workload policy to load. The agent owner's own
// policy is deliberately absent: a workload is a principal in its own right
// rather than something acting on an owner's behalf, so the owner bounds which
// agent may be assigned, not what the machine may reach.
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
}

// WorkloadSessionAdmitter supplies application-owned workload admission without
// making the generic authorization engine depend on agent policy.
type WorkloadSessionAdmitter func(context.Context, *pgxpool.Pool) (WorkloadSessionAdmission, error)

// WorkloadSessionDBTXAdmitter admits a workload session on a caller-owned snapshot.
type WorkloadSessionDBTXAdmitter func(context.Context, accessrepo.DBTX) (WorkloadSessionAdmission, error)

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
	return applyWorkloadSessionAdmission(ctx, admission)
}

// AdmitWorkloadSessionWithDBTX fails closed without transaction-bound admission.
func (e *Engine) AdmitWorkloadSessionWithDBTX(ctx context.Context, db accessrepo.DBTX) (context.Context, error) {
	if e.admitWorkloadSessionWithDBTX == nil {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	admission, err := e.admitWorkloadSessionWithDBTX(ctx, db)
	if err != nil {
		return ctx, err
	}
	return applyWorkloadSessionAdmission(ctx, admission)
}

func applyWorkloadSessionAdmission(ctx context.Context, admission WorkloadSessionAdmission) (context.Context, error) {
	// An admission naming no agent authorizes nothing. Checked here as well as
	// in the admitter so a future admitter cannot widen the boundary by
	// returning a zero value.
	if admission.AgentPrincipal == "" || admission.OwnerUserID == "" {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	ctx = contextvalues.WithPrincipalCredentialOwner(ctx, admission.OwnerUserID)
	return workloadPoliciesToContext(ctx, admission.Ceiling, admission.Agent), nil
}
