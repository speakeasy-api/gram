package workloadidentity

import (
	"context"
	"errors"
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
	// A missing, tenant-scoped issuer cannot admit new sessions. Explicitly
	// allow cleanup of retained sessions after hard deletion; never infer legacy.
	if _, err := q.LockWorkloadIssuerForRevocation(ctx, repo.LockWorkloadIssuerForRevocationParams{OrganizationID: organizationID, IssuerID: issuerID}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
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
// this helper locks and advances the agent timestamp so owner-loss updates cannot
// reverse that order. CreateUserSession checks the cutoff under its agent lock.
func RevokeAgentWorkloadSessionsTx(ctx context.Context, tx pgx.Tx, organizationID string, agentID uuid.UUID) error {
	if tx == nil || organizationID == "" || agentID == uuid.Nil {
		return fmt.Errorf("agent workload session revocation requires a tenant and agent")
	}
	q := repo.New(tx)
	// A hard-deleted agent cannot pass the locked issuance query. Continue
	// retiring retained sessions, but never treat a missing lock as a live agent.
	if _, err := q.LockWorkloadAgentForRevocation(ctx, repo.LockWorkloadAgentForRevocationParams{OrganizationID: organizationID, AgentID: agentID}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lock workload agent for revocation: %w", err)
	}
	if err := q.RevokeAgentWorkloadSessions(ctx, repo.RevokeAgentWorkloadSessionsParams{OrganizationID: organizationID, AgentID: agentID}); err != nil {
		return fmt.Errorf("revoke agent workload sessions: %w", err)
	}
	return nil
}
