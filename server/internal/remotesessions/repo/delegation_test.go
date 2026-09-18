//nolint:glint // Persistence regression tests need raw fixtures and lifecycle writes.
package repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestTrustedDelegationCredentialCASAndCleanup(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	container, clone, err := testenv.NewTestPostgres(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })
	conn, err := clone(t, "delegation_credentials")
	require.NoError(t, err)
	q := repo.New(conn)
	const org = "org_delegation_test"
	const subject = "user:delegation_test"
	issuer, client := uuid.New(), uuid.New()
	_, err = conn.Exec(ctx, `INSERT INTO organization_metadata (id,name,slug) VALUES ($1,'Test organization','delegation-test')`, org)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `INSERT INTO users (id,email,display_name) VALUES ('delegation_test','delegation@example.test','Test user')`)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `INSERT INTO organization_user_relationships (organization_id,user_id) VALUES ($1,'delegation_test')`, org)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `INSERT INTO remote_session_issuers (id,organization_id,slug,issuer) VALUES ($1,$2,'delegation-test','https://issuer.example.test')`, issuer, org)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `INSERT INTO remote_session_clients (id,organization_id,remote_session_issuer_id,client_id) VALUES ($1,$2,$3,'test-client')`, client, org, issuer)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `INSERT INTO user_session_issuers (organization_id,slug,authn_challenge_mode,session_duration,trusted_remote_session_issuer_id,trusted_remote_session_client_id) VALUES ($1,'delegation-test','interactive',interval '1 hour',$2,$3)`, org, issuer, client)
	require.NoError(t, err)
	text := func(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }
	params := repo.UpsertTrustedDelegationCredentialParams{
		OrganizationID: org, ClientID: client, IssuerID: issuer, SubjectUrn: subject,
		IdentityAssertionEncrypted: text("encrypted-assertion"),
		IdentityAssertionExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		RefreshTokenEncrypted:      text("encrypted-refresh"), UpstreamSubjectEncrypted: text("encrypted-subject"),
		NonceEncrypted: text("encrypted-nonce"), CredentialConfigHash: text("config"),
	}
	row, err := q.UpsertTrustedDelegationCredential(ctx, params)
	require.NoError(t, err)
	require.Equal(t, int64(1), row.CredentialGeneration.Int64)
	_, err = q.UpsertTrustedDelegationCredential(ctx, params)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	// Cleanup cannot act through a mismatched issuer binding.
	_, err = conn.Exec(ctx, `UPDATE trusted_issuer_sessions SET identity_assertion_expires_at=clock_timestamp()-interval '1 hour' WHERE id=$1`, row.ID)
	require.NoError(t, err)
	cleanup := repo.ClearExpiredTrustedDelegationAssertionParams{OrganizationID: org, ClientID: client, IssuerID: uuid.New(), SubjectUrn: subject}
	cleared, err := q.ClearExpiredTrustedDelegationAssertion(ctx, cleanup)
	require.NoError(t, err)
	require.Zero(t, cleared)
	cleanup.IssuerID = issuer
	cleared, err = q.ClearExpiredTrustedDelegationAssertion(ctx, cleanup)
	require.NoError(t, err)
	require.Equal(t, int64(1), cleared)
	// Legacy NULL generations behave as generation 1 until a write advances them.
	_, err = conn.Exec(ctx, `UPDATE trusted_issuer_sessions SET credential_generation = NULL WHERE id = $1`, row.ID)
	require.NoError(t, err)
	params.ExpectedGeneration = row.CredentialGeneration.Int64
	claimID := uuid.New()
	claimed, err := q.ClaimTrustedDelegationRefresh(ctx, repo.ClaimTrustedDelegationRefreshParams{OrganizationID: org, ClientID: client, IssuerID: issuer, SubjectUrn: subject, ExpectedGeneration: row.CredentialGeneration.Int64, RefreshClaimID: claimID})
	require.NoError(t, err)
	require.Greater(t, claimed.CredentialGeneration.Int64, row.CredentialGeneration.Int64)
	require.False(t, claimed.LastRefreshAttemptAt.Valid, "claim acquisition is not a provider request")
	attempt := repo.MarkTrustedDelegationRefreshAttemptParams{OrganizationID: org, ClientID: client, IssuerID: issuer, SubjectUrn: subject, ExpectedGeneration: claimed.CredentialGeneration.Int64, RefreshClaimID: uuid.New()}
	marked, err := q.MarkTrustedDelegationRefreshAttempt(ctx, attempt)
	require.NoError(t, err)
	require.Zero(t, marked)
	attempt.RefreshClaimID = claimID
	marked, err = q.MarkTrustedDelegationRefreshAttempt(ctx, attempt)
	require.NoError(t, err)
	require.Equal(t, int64(1), marked)
	var attempted bool
	err = conn.QueryRow(ctx, `SELECT last_refresh_attempt_at IS NOT NULL FROM trusted_issuer_sessions WHERE id=$1`, row.ID).Scan(&attempted)
	require.NoError(t, err)
	require.True(t, attempted)

	// Claim acquisition invalidates callback snapshots that might preserve a spent token.
	_, err = q.UpsertTrustedDelegationCredential(ctx, params)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = conn.Exec(ctx, `UPDATE trusted_issuer_sessions SET last_refresh_attempt_at = clock_timestamp() - interval '2 days' WHERE id=$1`, row.ID)
	require.NoError(t, err)
	_, err = q.ClaimTrustedDelegationRefresh(ctx, repo.ClaimTrustedDelegationRefreshParams{OrganizationID: org, ClientID: client, IssuerID: issuer, SubjectUrn: subject, ExpectedGeneration: claimed.CredentialGeneration.Int64, RefreshClaimID: uuid.New()})
	require.ErrorIs(t, err, pgx.ErrNoRows) // A timeout never releases the claim.
	// Fresh login supersedes the in-flight generation; its late result cannot win.
	params.ExpectedGeneration = claimed.CredentialGeneration.Int64
	params.RefreshTokenEncrypted = text("new-login-refresh")
	replacement, err := q.UpsertTrustedDelegationCredential(ctx, params)
	require.NoError(t, err)
	require.False(t, replacement.RefreshClaimID.Valid)
	require.True(t, replacement.LastRefreshAttemptAt.Valid)
	require.WithinDuration(t, time.Now().Add(-48*time.Hour), replacement.LastRefreshAttemptAt.Time, time.Minute)
	_, err = q.CompleteTrustedDelegationRefresh(ctx, repo.CompleteTrustedDelegationRefreshParams{OrganizationID: org, ClientID: client, IssuerID: issuer, SubjectUrn: subject, ExpectedGeneration: claimed.CredentialGeneration.Int64, RefreshClaimID: claimID, RefreshTokenEncrypted: text("late-refresh")})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	get := repo.GetTrustedDelegationCredentialParams{OrganizationID: org, ClientID: client, IssuerID: issuer, SubjectUrn: subject}
	read, err := q.GetTrustedDelegationCredential(ctx, get)
	require.NoError(t, err)
	require.Equal(t, "new-login-refresh", read.RefreshTokenEncrypted.String)
	get.IssuerID = uuid.New()
	_, err = q.GetTrustedDelegationCredential(ctx, get)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	get.IssuerID = issuer
	// Successful rotation is one atomic replacement and advances its generation.
	claimID = uuid.New()
	claimed, err = q.ClaimTrustedDelegationRefresh(ctx, repo.ClaimTrustedDelegationRefreshParams{OrganizationID: org, ClientID: client, IssuerID: issuer, SubjectUrn: subject, ExpectedGeneration: read.CredentialGeneration.Int64, RefreshClaimID: claimID})
	require.NoError(t, err)
	completed, err := q.CompleteTrustedDelegationRefresh(ctx, repo.CompleteTrustedDelegationRefreshParams{OrganizationID: org, ClientID: client, IssuerID: issuer, SubjectUrn: subject, ExpectedGeneration: claimed.CredentialGeneration.Int64, RefreshClaimID: claimID, RefreshTokenEncrypted: text("rotated-refresh"), UpstreamSubjectEncrypted: text("encrypted-subject"), NonceEncrypted: text("encrypted-nonce")})
	require.NoError(t, err)
	require.Greater(t, completed.CredentialGeneration.Int64, claimed.CredentialGeneration.Int64)
	require.Equal(t, "rotated-refresh", completed.RefreshTokenEncrypted.String)
	require.False(t, completed.RefreshClaimID.Valid)
	require.False(t, completed.IdentityAssertionEncrypted.Valid)
	get.OrganizationID = "org_another_test"
	_, err = q.GetTrustedDelegationCredential(ctx, get)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	get.OrganizationID = org
	// Expiry clears assertion and refresh, plus identity-only residue, even when inactive.
	_, err = conn.Exec(ctx, `UPDATE trusted_issuer_sessions SET identity_assertion_expires_at=clock_timestamp()-interval '1 hour',refresh_expires_at=clock_timestamp()-interval '1 hour' WHERE id=$1`, row.ID)
	require.NoError(t, err)
	count, err := q.CleanupTrustedDelegationCredentialsBatch(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	read, err = q.GetTrustedDelegationCredential(ctx, get)
	require.NoError(t, err)
	require.False(t, read.IdentityAssertionEncrypted.Valid)
	require.False(t, read.RefreshTokenEncrypted.Valid)
	require.False(t, read.UpstreamSubjectEncrypted.Valid)
	require.False(t, read.NonceEncrypted.Valid)
	count, err = q.CleanupTrustedDelegationCredentialsBatch(ctx, 1)
	require.NoError(t, err)
	require.Zero(t, count)
	params.ExpectedGeneration = read.CredentialGeneration.Int64
	_, err = q.UpsertTrustedDelegationCredential(ctx, params)
	require.NoError(t, err)
	// Aggregate timestamps describe current-registration observations only.
	_, err = conn.Exec(ctx, `UPDATE trusted_issuer_sessions SET observation_status='durable_credential_present', observed_at=clock_timestamp(), credential_obtained_at=clock_timestamp()-interval '1 day', last_refresh_succeeded_at=clock_timestamp()-interval '1 hour' WHERE id=$1`, row.ID)
	require.NoError(t, err)
	counts, err := q.CountTrustedDelegationObservations(ctx, repo.CountTrustedDelegationObservationsParams{OrganizationID: org, ClientID: client, ConfigHash: "config", ObservedSince: pgtype.Timestamptz{Time: time.Now().Add(-30 * 24 * time.Hour), Valid: true}})
	require.NoError(t, err)
	require.Len(t, counts, 1)
	require.Equal(t, int64(1), counts[0].ObservationCount)
	require.True(t, counts[0].LastObservedAt.Valid)
	require.True(t, counts[0].LastCredentialObtainedAt.Valid)
	require.True(t, counts[0].LastRefreshSucceededAt.Valid)
	counts, err = q.CountTrustedDelegationObservations(ctx, repo.CountTrustedDelegationObservationsParams{OrganizationID: org, ClientID: client, ConfigHash: "other-config", ObservedSince: pgtype.Timestamptz{Time: time.Now().Add(-30 * 24 * time.Hour), Valid: true}})
	require.NoError(t, err)
	require.Empty(t, counts)
	// Status derives current credential validity without refreshing observation time.
	_, err = conn.Exec(ctx, `UPDATE trusted_issuer_sessions SET refresh_token_encrypted=NULL, refresh_expires_at=clock_timestamp()-interval '1 day', identity_assertion_expires_at=NULL WHERE id=$1`, row.ID)
	require.NoError(t, err)
	counts, err = q.CountTrustedDelegationObservations(ctx, repo.CountTrustedDelegationObservationsParams{OrganizationID: org, ClientID: client, ConfigHash: "config", ObservedSince: pgtype.Timestamptz{Time: time.Now().Add(-30 * 24 * time.Hour), Valid: true}})
	require.NoError(t, err)
	require.Len(t, counts, 1)
	require.Equal(t, "reauthentication_required", counts[0].ObservationStatus)
	observed := counts[0].LastObservedAt
	count, err = q.CleanupTrustedDelegationCredentialsBatch(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	read, err = q.GetTrustedDelegationCredential(ctx, get)
	require.NoError(t, err)
	require.False(t, read.IdentityAssertionEncrypted.Valid)
	require.False(t, read.UpstreamSubjectEncrypted.Valid)
	require.Equal(t, observed, read.ObservedAt)
	// Stale expiry metadata without its secret must not starve later batches.
	_, err = conn.Exec(ctx, `UPDATE trusted_issuer_sessions SET identity_assertion_encrypted='valid-assertion', identity_assertion_expires_at=clock_timestamp()+interval '1 day', upstream_subject_encrypted='subject', refresh_expires_at=clock_timestamp()-interval '1 day' WHERE id=$1`, row.ID)
	require.NoError(t, err)
	count, err = q.CleanupTrustedDelegationCredentialsBatch(ctx, 1)
	require.NoError(t, err)
	require.Zero(t, count)
	// An invalid project-scoped row is maintenance-orphaned, not skipped forever.
	project := uuid.New()
	_, err = conn.Exec(ctx, `INSERT INTO projects (id,organization_id,name,slug) VALUES ($1,$2,'Cleanup test','cleanup-test')`, project, org)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `UPDATE trusted_issuer_sessions SET project_id=$2 WHERE id=$1`, row.ID, project)
	require.NoError(t, err)
	count, err = q.CleanupTrustedDelegationCredentialsBatch(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	var projectHasSecrets bool
	err = conn.QueryRow(ctx, `SELECT identity_assertion_encrypted IS NOT NULL OR upstream_subject_encrypted IS NOT NULL FROM trusted_issuer_sessions WHERE id=$1`, row.ID).Scan(&projectHasSecrets)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	params.ExpectedGeneration = 0
	row, err = q.UpsertTrustedDelegationCredential(ctx, params)
	require.NoError(t, err)
	// Explicit revocation must work after removal of trust, without a live read.
	_, err = conn.Exec(ctx, `UPDATE user_session_issuers SET deleted_at=clock_timestamp() WHERE organization_id=$1`, org)
	require.NoError(t, err)
	revocation := repo.RevokeTrustedDelegationCredentialParams{OrganizationID: org, ClientID: client, IssuerID: uuid.New(), SubjectUrn: subject}
	revoked, err := q.RevokeTrustedDelegationCredential(ctx, revocation)
	require.NoError(t, err)
	require.Zero(t, revoked)
	revocation.IssuerID = issuer
	revoked, err = q.RevokeTrustedDelegationCredential(ctx, revocation)
	require.NoError(t, err)
	require.Equal(t, int64(1), revoked)
	var secretsRemain bool
	var generation int64
	err = conn.QueryRow(ctx, `SELECT identity_assertion_encrypted IS NOT NULL OR refresh_token_encrypted IS NOT NULL OR upstream_subject_encrypted IS NOT NULL OR nonce_encrypted IS NOT NULL, credential_generation FROM trusted_issuer_sessions WHERE id=$1`, row.ID).Scan(&secretsRemain, &generation)
	require.NoError(t, err)
	require.False(t, secretsRemain)
	require.Greater(t, generation, row.CredentialGeneration.Int64)
	// A removed membership blocks all reads before any decryption, and cleanup erases residue.
	_, err = conn.Exec(ctx, `UPDATE organization_user_relationships SET deleted_at=clock_timestamp() WHERE organization_id=$1`, org)
	require.NoError(t, err)
	_, err = q.GetTrustedDelegationCredential(ctx, get)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	count, err = q.CleanupTrustedDelegationCredentialsBatch(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	var hasSecrets bool
	err = conn.QueryRow(ctx, `SELECT identity_assertion_encrypted IS NOT NULL OR refresh_token_encrypted IS NOT NULL OR upstream_subject_encrypted IS NOT NULL OR nonce_encrypted IS NOT NULL FROM trusted_issuer_sessions WHERE id=$1`, row.ID).Scan(&hasSecrets)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	for _, lifecycle := range []string{"deleted", "orphaned", "secret-free-orphan"} {
		t.Run(lifecycle, func(t *testing.T) {
			id := uuid.New()
			_, err := conn.Exec(ctx, `INSERT INTO trusted_issuer_sessions (id,organization_id,remote_session_client_id,subject_urn,identity_assertion_encrypted,refresh_token_encrypted,upstream_subject_encrypted,nonce_encrypted,deleted_at) VALUES ($1,$2,$3,'user:cleanup-test','assertion','refresh','subject','nonce',CASE WHEN $4='deleted' THEN clock_timestamp() ELSE NULL END)`, id, org, client, lifecycle)
			require.NoError(t, err)
			if lifecycle == "orphaned" {
				_, err = conn.Exec(ctx, `UPDATE trusted_issuer_sessions SET remote_session_client_id=NULL, organization_id=NULL WHERE id=$1`, id)
				require.NoError(t, err)
			}
			if lifecycle == "secret-free-orphan" {
				_, err = conn.Exec(ctx, `UPDATE trusted_issuer_sessions SET remote_session_client_id=NULL, identity_assertion_encrypted=NULL, refresh_token_encrypted=NULL, upstream_subject_encrypted=NULL, nonce_encrypted=NULL WHERE id=$1`, id)
				require.NoError(t, err)
			}
			count, err := q.CleanupTrustedDelegationCredentialsBatch(ctx, 1)
			require.NoError(t, err)
			require.Equal(t, int64(1), count)
			err = conn.QueryRow(ctx, `SELECT identity_assertion_encrypted IS NOT NULL OR refresh_token_encrypted IS NOT NULL OR upstream_subject_encrypted IS NOT NULL OR nonce_encrypted IS NOT NULL FROM trusted_issuer_sessions WHERE id=$1`, id).Scan(&hasSecrets)
			require.ErrorIs(t, err, pgx.ErrNoRows)
			count, err = q.CleanupTrustedDelegationCredentialsBatch(ctx, 1)
			require.NoError(t, err)
			require.Zero(t, count)
		})
	}
}
