package workloadidentity

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity/repo"
)

// RevokeWorkloadSessionsTx permanently retires sessions for an exact workload.
// The issuer lock must also be held by issuance until its session is committed.
// Call before removing the assignment; commit revocation and mutation together.
func RevokeWorkloadSessionsTx(ctx context.Context, tx pgx.Tx, organizationID string, issuerID uuid.UUID, subject string) error {
	if tx == nil || organizationID == "" || issuerID == uuid.Nil || subject == "" {
		return fmt.Errorf("workload session revocation requires a tenant and exact identity")
	}
	if err := ValidateSubjectRule(MatchKindExact, subject, false); err != nil {
		return fmt.Errorf("validate exact workload revocation subject: %w", err)
	}
	q := repo.New(tx)
	if err := q.LockWorkloadIssuerForRevocation(ctx, repo.LockWorkloadIssuerForRevocationParams{OrganizationID: organizationID, IssuerID: issuerID}); err != nil {
		return fmt.Errorf("lock workload issuer for revocation: %w", err)
	}
	if err := q.RevokeWorkloadSessions(ctx, repo.RevokeWorkloadSessionsParams{OrganizationID: organizationID, SubjectUrn: urn.NewWorkloadSubject(issuerID, subject).String()}); err != nil {
		return fmt.Errorf("revoke workload sessions: %w", err)
	}
	return nil
}

// RevokeAgentWorkloadSessionsTx retires the workload sessions currently assigned
// to an agent, including external wildcard assignments with exact precedence.
// Call before changing assignments. Issuance locks the issuer before the agent;
// this helper only locks the agent so owner-loss updates cannot reverse that order.
func RevokeAgentWorkloadSessionsTx(ctx context.Context, tx pgx.Tx, organizationID string, agentID uuid.UUID) error {
	if tx == nil || organizationID == "" || agentID == uuid.Nil {
		return fmt.Errorf("agent workload session revocation requires a tenant and agent")
	}
	q := repo.New(tx)
	if err := q.LockWorkloadAgentForRevocation(ctx, repo.LockWorkloadAgentForRevocationParams{OrganizationID: organizationID, AgentID: agentID}); err != nil {
		return fmt.Errorf("lock workload agent for revocation: %w", err)
	}
	if err := q.RevokeAgentWorkloadSessions(ctx, repo.RevokeAgentWorkloadSessionsParams{OrganizationID: organizationID, AgentID: agentID}); err != nil {
		return fmt.Errorf("revoke agent workload sessions: %w", err)
	}
	return nil
}
