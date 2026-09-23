package workloadidentity

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/workloadidentity/repo"
)

// AssignmentParams addresses one workload principal. Every field is part of the
// key; omitting one never widens the search.
//
// No project tier, unlike AdmissionParams: admission asks whether a tenant
// recognises a machine, which a project may answer for itself, while the
// principal and its authority are organization-scoped.
type AssignmentParams struct {
	// OrganizationID scopes every row considered. Empty resolves nothing.
	OrganizationID string
	// WorkloadIssuerID is the row that vouches for the subject, never the
	// issuer URL, so a discovery refresh cannot repoint an assignment.
	WorkloadIssuerID uuid.UUID
	// Subject is the sub claim the issuer asserted, exactly as it arrived.
	Subject string
}

// ResolveAssignedAgent returns the agent a workload principal inherits its
// permission policy from, and whether one is assigned at all.
//
// A workload holds no grants of its own, so this is where its authority comes
// from: no assigned agent means no authority, and every check is denied. The
// agent's own lifecycle and policy are evaluated by the caller, since an agent
// that is suspended, revoked or deleted still has a row here.
//
// Several rules can cover one subject — a wildcard over an issuer's whole fleet
// and an exact assignment naming one principal — so the most specific wins:
// exact before wildcard, and a longer stem before a shorter one. That is what
// lets a fleet share one agent while individual principals are pinned elsewhere,
// and it is why one agent per workload is resolved here rather than guaranteed by
// a unique index.
//
// Not found and an error must never be collapsed: not found is a decision, an
// error is the absence of one.
func ResolveAssignedAgent(ctx context.Context, db repo.DBTX, params AssignmentParams) (uuid.UUID, bool, error) {
	// Checked before the store, like IsAdmitted's organization: a tenancy hole
	// should not depend on a data property a seed could break.
	switch {
	case params.OrganizationID == "", params.WorkloadIssuerID == uuid.Nil, params.Subject == "":
		return uuid.Nil, false, nil
	}

	agentID, err := repo.New(db).ResolveWorkloadAgentAssignment(ctx, repo.ResolveWorkloadAgentAssignmentParams{
		OrganizationID:   params.OrganizationID,
		WorkloadIssuerID: params.WorkloadIssuerID,
		Subject:          params.Subject,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return uuid.Nil, false, nil
	case err != nil:
		return uuid.Nil, false, fmt.Errorf("resolve workload agent assignment: %w", err)
	}

	return agentID, true, nil
}
