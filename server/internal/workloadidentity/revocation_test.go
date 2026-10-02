package workloadidentity_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	sessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
	identityrepo "github.com/speakeasy-api/gram/server/internal/workloadidentity/repo"
	policyrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

func workloadSessionIssuer(t *testing.T, conn *pgxpool.Pool, org string) uuid.UUID {
	t.Helper()
	row, err := sessionsrepo.New(conn).CreateOrganizationUserSessionIssuer(t.Context(), sessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID: conv.ToPGText(org), Slug: "sessions-" + uuid.NewString(), AuthnChallengeMode: "interactive",
		SessionDuration: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
	return row.ID
}

func workloadSessionParams(issuerID, workloadIssuerID uuid.UUID, subject string) sessionsrepo.CreateUserSessionParams {
	return sessionsrepo.CreateUserSessionParams{
		UserSessionIssuerID: issuerID, SubjectUrn: urn.NewWorkloadSubject(workloadIssuerID, subject), Jti: uuid.NewString(),
		ExpiresAt:        pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		RefreshExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}
}

func TestRevokeWorkloadSessionsTx_ExactTenantScopedAndRollback(t *testing.T) {
	t.Parallel()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	f := newAssignmentFixture(t, conn)
	seedAssignment(t, conn, f.tenant.organizationID, f.issuerID, testSubject, f.agentID)
	seedAssignment(t, conn, f.tenant.organizationID, f.issuerID, testSubject+":other", f.agentID)
	issuer := workloadSessionIssuer(t, conn, f.tenant.organizationID)
	q := sessionsrepo.New(conn)
	params := workloadSessionParams(issuer, f.issuerID, testSubject)
	original, err := q.CreateUserSession(t.Context(), params)
	require.NoError(t, err)
	other, err := q.CreateUserSession(t.Context(), workloadSessionParams(issuer, f.issuerID, testSubject+":other"))
	require.NoError(t, err)
	read := func(id uuid.UUID) error {
		_, err := q.GetUserSessionByID(t.Context(), sessionsrepo.GetUserSessionByIDParams{ID: id, OrganizationID: f.tenant.organizationID})
		if err != nil {
			return fmt.Errorf("read workload session: %w", err)
		}
		return nil
	}
	rollback := errors.New("rollback revocation")
	err = pgx.BeginFunc(t.Context(), conn, func(tx pgx.Tx) error {
		if err := workloadidentity.RevokeWorkloadSessionsTx(t.Context(), tx, f.tenant.organizationID, f.issuerID, testSubject); err != nil {
			return fmt.Errorf("revoke before rollback: %w", err)
		}
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	require.NoError(t, read(original.ID))
	require.NoError(t, pgx.BeginFunc(t.Context(), conn, func(tx pgx.Tx) error {
		return workloadidentity.RevokeWorkloadSessionsTx(t.Context(), tx, "another-organization", f.issuerID, testSubject)
	}))
	require.NoError(t, read(original.ID))
	require.NoError(t, pgx.BeginFunc(t.Context(), conn, func(tx pgx.Tx) error {
		return workloadidentity.RevokeWorkloadSessionsTx(t.Context(), tx, f.tenant.organizationID, f.issuerID, testSubject)
	}))
	require.ErrorIs(t, read(original.ID), pgx.ErrNoRows)
	require.NoError(t, read(other.ID))
}

func TestRevokeAgentWorkloadSessionsTx_UsesWinningAssignment(t *testing.T) {
	t.Parallel()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	f := newAssignmentFixture(t, conn)
	allowWildcardAdmission(t, conn, f.issuerID, true)
	secondAgent := seedAgent(t, conn, f.tenant.organizationID)
	seedAssignmentRule(t, conn, f.tenant.organizationID, f.issuerID, "subject:*", workloadidentity.MatchKindWildcard, f.agentID)
	seedAssignment(t, conn, f.tenant.organizationID, f.issuerID, "subject:exact", secondAgent)
	issuer := workloadSessionIssuer(t, conn, f.tenant.organizationID)
	q := sessionsrepo.New(conn)
	wildcard, err := q.CreateUserSession(t.Context(), workloadSessionParams(issuer, f.issuerID, "subject:wildcard"))
	require.NoError(t, err)
	exact, err := q.CreateUserSession(t.Context(), workloadSessionParams(issuer, f.issuerID, "subject:exact"))
	require.NoError(t, err)
	require.NoError(t, pgx.BeginFunc(t.Context(), conn, func(tx pgx.Tx) error {
		return workloadidentity.RevokeAgentWorkloadSessionsTx(t.Context(), tx, f.tenant.organizationID, f.agentID)
	}))
	_, err = q.GetUserSessionByID(t.Context(), sessionsrepo.GetUserSessionByIDParams{ID: wildcard.ID, OrganizationID: f.tenant.organizationID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = q.GetUserSessionByID(t.Context(), sessionsrepo.GetUserSessionByIDParams{ID: exact.ID, OrganizationID: f.tenant.organizationID})
	require.NoError(t, err)
	wildcardAfter, err := q.CreateUserSession(t.Context(), workloadSessionParams(issuer, f.issuerID, "subject:later"))
	require.NoError(t, err)
	require.NoError(t, pgx.BeginFunc(t.Context(), conn, func(tx pgx.Tx) error {
		policy := policyrepo.New(tx)
		if err := policy.RevokeWorkloadAssignmentSessions(t.Context(), policyrepo.RevokeWorkloadAssignmentSessionsParams{OrganizationID: f.tenant.organizationID, WorkloadIssuerID: f.issuerID, MatchKind: "wildcard", Subject: "subject:*"}); err != nil {
			return fmt.Errorf("revoke wildcard assignment sessions: %w", err)
		}
		_, err := policy.SoftDeleteWorkloadAgentAssignmentForSubject(t.Context(), policyrepo.SoftDeleteWorkloadAgentAssignmentForSubjectParams{OrganizationID: f.tenant.organizationID, WorkloadIssuerID: f.issuerID, MatchKind: "wildcard", Subject: "subject:*"})
		if err != nil {
			return fmt.Errorf("withdraw wildcard assignment: %w", err)
		}
		return nil
	}))
	_, err = q.GetUserSessionByID(t.Context(), sessionsrepo.GetUserSessionByIDParams{ID: wildcardAfter.ID, OrganizationID: f.tenant.organizationID})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	_, err = q.GetUserSessionPrincipalCredentialByJTI(t.Context(), sessionsrepo.GetUserSessionPrincipalCredentialByJTIParams{UserSessionIssuerID: issuer, Jti: exact.Jti})
	require.NoError(t, err, "withdrawing the broad rule must preserve the unchanged exact exception's session")
}

func TestWorkloadSessionIssuance_ConcurrentAssignmentCannotReviveOldAuthority(t *testing.T) {
	t.Parallel()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	f := newAssignmentFixture(t, conn)
	seedAssignment(t, conn, f.tenant.organizationID, f.issuerID, testSubject, f.agentID)
	issuer := workloadSessionIssuer(t, conn, f.tenant.organizationID)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	mutation := testenv.BeginTx(t, ctx, conn)
	defer func() { _ = mutation.Rollback(ctx) }()
	require.NoError(t, workloadidentity.RevokeWorkloadSessionsTx(ctx, mutation, f.tenant.organizationID, f.issuerID, testSubject))
	issuance := testenv.BeginTx(t, ctx, conn)
	defer func() { _ = issuance.Rollback(ctx) }()
	// Establish the issuance transaction before the assignment changes.
	_, err = identityrepo.New(issuance).ResolveWorkloadAgentAssignment(ctx, identityrepo.ResolveWorkloadAgentAssignmentParams{OrganizationID: f.tenant.organizationID, WorkloadIssuerID: f.issuerID, Subject: testSubject})
	require.NoError(t, err)
	params := workloadSessionParams(issuer, f.issuerID, testSubject)
	done := make(chan error, 1)
	go func() { _, err := sessionsrepo.New(issuance).CreateUserSession(ctx, params); done <- err }()
	_, err = policyrepo.New(mutation).UpsertWorkloadAgentAssignment(ctx, policyrepo.UpsertWorkloadAgentAssignmentParams{
		OrganizationID: f.tenant.organizationID, WorkloadIssuerID: f.issuerID, Subject: testSubject, MatchKind: "exact", AgentID: f.agentID,
	})
	require.NoError(t, err)
	require.NoError(t, mutation.Commit(ctx))
	mintErr := <-done
	if mintErr != nil {
		require.ErrorIs(t, mintErr, pgx.ErrNoRows)
	} else {
		require.NoError(t, issuance.Commit(ctx))
		_, err = sessionsrepo.New(conn).GetUserSessionPrincipalCredentialByJTI(ctx, sessionsrepo.GetUserSessionPrincipalCredentialByJTIParams{UserSessionIssuerID: issuer, Jti: params.Jti})
		require.ErrorIs(t, err, pgx.ErrNoRows, "a session from the old assignment snapshot must not acquire replacement authority")
	}
	_ = issuance.Rollback(ctx)
	_, err = sessionsrepo.New(conn).CreateUserSession(ctx, workloadSessionParams(issuer, f.issuerID, testSubject))
	require.NoError(t, err, "a fresh issuance after the mutation remains supported")
}

func TestWorkloadSessionIssuance_WithdrawalCannotExposeOlderWildcard(t *testing.T) {
	t.Parallel()
	for _, narrow := range []struct {
		name, subject string
		kind          workloadidentity.MatchKind
	}{
		{name: "exact", subject: "subject:narrow:one", kind: workloadidentity.MatchKindExact},
		{name: "narrow wildcard", subject: "subject:narrow:*", kind: workloadidentity.MatchKindWildcard},
	} {
		t.Run(narrow.name, func(t *testing.T) {
			t.Parallel()
			conn, err := infra.CloneTestDatabase(t, "testdb")
			require.NoError(t, err)
			f := newAssignmentFixture(t, conn)
			allowWildcardAdmission(t, conn, f.issuerID, true)
			broadAgent := seedAgent(t, conn, f.tenant.organizationID)
			seedAssignmentRule(t, conn, f.tenant.organizationID, f.issuerID, "subject:*", workloadidentity.MatchKindWildcard, broadAgent)
			seedAssignmentRule(t, conn, f.tenant.organizationID, f.issuerID, narrow.subject, narrow.kind, f.agentID)
			issuer := workloadSessionIssuer(t, conn, f.tenant.organizationID)
			params := workloadSessionParams(issuer, f.issuerID, "subject:narrow:one")
			original, err := sessionsrepo.New(conn).CreateUserSession(t.Context(), params)
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			mutation := testenv.BeginTx(t, ctx, conn)
			defer func() { _ = mutation.Rollback(ctx) }()
			q := policyrepo.New(mutation)
			_, err = q.LockWorkloadIssuerForWrite(ctx, policyrepo.LockWorkloadIssuerForWriteParams{OrganizationID: f.tenant.organizationID, ID: f.issuerID})
			require.NoError(t, err)
			issuance := testenv.BeginTx(t, ctx, conn)
			defer func() { _ = issuance.Rollback(ctx) }()
			_, err = identityrepo.New(issuance).ResolveWorkloadAgentAssignment(ctx, identityrepo.ResolveWorkloadAgentAssignmentParams{OrganizationID: f.tenant.organizationID, WorkloadIssuerID: f.issuerID, Subject: "subject:narrow:one"})
			require.NoError(t, err)
			racing := workloadSessionParams(issuer, f.issuerID, "subject:narrow:one")
			done := make(chan error, 1)
			go func() { _, err := sessionsrepo.New(issuance).CreateUserSession(ctx, racing); done <- err }()
			require.NoError(t, q.RevokeWorkloadAssignmentSessions(ctx, policyrepo.RevokeWorkloadAssignmentSessionsParams{
				OrganizationID: f.tenant.organizationID, WorkloadIssuerID: f.issuerID, MatchKind: string(narrow.kind), Subject: narrow.subject,
			}))
			_, err = q.SoftDeleteWorkloadAgentAssignmentForSubject(ctx, policyrepo.SoftDeleteWorkloadAgentAssignmentForSubjectParams{
				OrganizationID: f.tenant.organizationID, WorkloadIssuerID: f.issuerID, MatchKind: string(narrow.kind), Subject: narrow.subject,
			})
			require.NoError(t, err)
			require.NoError(t, mutation.Commit(ctx))
			mintErr := <-done
			if mintErr != nil {
				require.ErrorIs(t, mintErr, pgx.ErrNoRows)
			} else {
				require.NoError(t, issuance.Commit(ctx))
				_, err = sessionsrepo.New(conn).GetUserSessionPrincipalCredentialByJTI(ctx, sessionsrepo.GetUserSessionPrincipalCredentialByJTIParams{UserSessionIssuerID: issuer, Jti: racing.Jti})
				require.ErrorIs(t, err, pgx.ErrNoRows, "an in-flight issuance cannot inherit the older broad assignment")
			}
			_ = issuance.Rollback(ctx)
			_, err = sessionsrepo.New(conn).GetUserSessionByID(ctx, sessionsrepo.GetUserSessionByIDParams{ID: original.ID, OrganizationID: f.tenant.organizationID})
			require.ErrorIs(t, err, pgx.ErrNoRows, "the withdrawn winner's existing sessions are revoked transactionally")
			fresh, err := sessionsrepo.New(conn).CreateUserSession(ctx, workloadSessionParams(issuer, f.issuerID, "subject:narrow:one"))
			require.NoError(t, err)
			_, err = sessionsrepo.New(conn).GetUserSessionPrincipalCredentialByJTI(ctx, sessionsrepo.GetUserSessionPrincipalCredentialByJTIParams{UserSessionIssuerID: issuer, Jti: fresh.Jti})
			require.NoError(t, err, "fresh issuance under the surviving broad assignment remains supported")
		})
	}
}

func TestWorkloadSessionsRequireIssuerProject(t *testing.T) {
	t.Parallel()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	f := newAssignmentFixture(t, conn)
	projectIssuer := seedIssuer(t, conn, f.tenant.organizationID, uuid.NullUUID{UUID: f.tenant.projectID, Valid: true}, "project-trust", testIssuerURL, epoch)
	seedAssignment(t, conn, f.tenant.organizationID, projectIssuer, testSubject, f.agentID)
	issuerRow, err := sessionsrepo.New(conn).CreateUserSessionIssuer(t.Context(), sessionsrepo.CreateUserSessionIssuerParams{ProjectID: f.tenant.projectID, OrganizationID: conv.ToPGText(f.tenant.organizationID), Slug: "project-sessions", AuthnChallengeMode: "interactive", SessionDuration: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true}})
	require.NoError(t, err)
	issuer := issuerRow.ID
	q := sessionsrepo.New(conn)
	params := workloadSessionParams(issuer, projectIssuer, testSubject)
	session, err := q.CreateUserSession(t.Context(), params)
	require.NoError(t, err)
	_, err = q.GetUserSessionPrincipalCredentialByJTI(t.Context(), sessionsrepo.GetUserSessionPrincipalCredentialByJTIParams{UserSessionIssuerID: issuer, Jti: session.Jti})
	require.NoError(t, err)
	sibling := newProject(t, conn, f.tenant.organizationID)
	siblingIssuer, err := q.CreateUserSessionIssuer(t.Context(), sessionsrepo.CreateUserSessionIssuerParams{ProjectID: sibling, OrganizationID: conv.ToPGText(f.tenant.organizationID), Slug: "sibling-sessions", AuthnChallengeMode: "interactive", SessionDuration: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true}})
	require.NoError(t, err)
	params.UserSessionIssuerID = siblingIssuer.ID
	params.Jti = uuid.NewString()
	_, err = q.CreateUserSession(t.Context(), params)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	// Simulate a credential persisted before the stricter tenant predicate.
	err = identityrepo.New(conn).FixtureMoveWorkloadSessionProject(t.Context(), identityrepo.FixtureMoveWorkloadSessionProjectParams{ProjectID: uuid.NullUUID{UUID: sibling, Valid: true}, SessionID: session.ID})
	require.NoError(t, err)
	_, err = q.GetUserSessionPrincipalCredentialByJTI(t.Context(), sessionsrepo.GetUserSessionPrincipalCredentialByJTIParams{UserSessionIssuerID: issuer, Jti: session.Jti})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestAgentRevocationFencesRacingSessionIssuance(t *testing.T) {
	t.Parallel()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	f := newAssignmentFixture(t, conn)
	seedAssignment(t, conn, f.tenant.organizationID, f.issuerID, testSubject, f.agentID)
	issuer := workloadSessionIssuer(t, conn, f.tenant.organizationID)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	issuance := testenv.BeginTx(t, ctx, conn)
	defer func() { _ = issuance.Rollback(ctx) }()
	_, err = identityrepo.New(issuance).ResolveWorkloadAgentAssignment(ctx, identityrepo.ResolveWorkloadAgentAssignmentParams{OrganizationID: f.tenant.organizationID, WorkloadIssuerID: f.issuerID, Subject: testSubject})
	require.NoError(t, err)
	mutation := testenv.BeginTx(t, ctx, conn)
	defer func() { _ = mutation.Rollback(ctx) }()
	require.NoError(t, workloadidentity.RevokeAgentWorkloadSessionsTx(ctx, mutation, f.tenant.organizationID, f.agentID))
	done := make(chan error, 1)
	go func() {
		_, err := sessionsrepo.New(issuance).CreateUserSession(ctx, workloadSessionParams(issuer, f.issuerID, testSubject))
		done <- err
	}()
	require.NoError(t, mutation.Commit(ctx))
	require.ErrorIs(t, <-done, pgx.ErrNoRows, "old issuance transaction must not commit past the agent revocation fence")
	require.NoError(t, issuance.Rollback(ctx))
	_, err = sessionsrepo.New(conn).CreateUserSession(ctx, workloadSessionParams(issuer, f.issuerID, testSubject))
	require.NoError(t, err, "fresh authority is still issuable after a policy change")
}
