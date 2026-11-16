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
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
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
	fixtures := testrepo.New(conn)
	require.NoError(t, fixtures.SeedDelegationLoaderOrganizationFixture(ctx, testrepo.SeedDelegationLoaderOrganizationFixtureParams{OrganizationID: org, Name: "Test organization", Slug: "delegation-test"}))
	require.NoError(t, fixtures.InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{ID: "delegation_test", Email: "delegation@example.test", DisplayName: "Test user"}))
	require.NoError(t, fixtures.CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{OrganizationID: org, UserID: pgtype.Text{String: "delegation_test", Valid: true}}))
	require.NoError(t, fixtures.SeedDelegationLoaderIssuerFixture(ctx, testrepo.SeedDelegationLoaderIssuerFixtureParams{
		ID: issuer, OrganizationID: pgtype.Text{String: org, Valid: true}, Slug: "delegation-test", Issuer: "https://issuer.example.test",
		AuthorizationEndpoint: pgtype.Text{}, TokenEndpoint: pgtype.Text{}, JwksUri: pgtype.Text{},
	}))
	require.NoError(t, fixtures.SeedDelegationLoaderClientFixture(ctx, testrepo.SeedDelegationLoaderClientFixtureParams{
		ID: client, OrganizationID: org, RemoteSessionIssuerID: issuer, ClientID: "test-client", Scope: nil,
	}))
	require.NoError(t, fixtures.SeedDelegationUserSessionIssuerFixture(ctx, testrepo.SeedDelegationUserSessionIssuerFixtureParams{
		OrganizationID: pgtype.Text{String: org, Valid: true}, Slug: "delegation-test",
		TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: issuer, Valid: true}, TrustedRemoteSessionClientID: uuid.NullUUID{UUID: client, Valid: true},
	}))
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
	require.NoError(t, fixtures.ExpireDelegationAssertionFixture(ctx, row.ID))
	cleanup := repo.ClearExpiredTrustedDelegationAssertionParams{OrganizationID: org, ClientID: client, IssuerID: uuid.New(), SubjectUrn: subject}
	cleared, err := q.ClearExpiredTrustedDelegationAssertion(ctx, cleanup)
	require.NoError(t, err)
	require.Zero(t, cleared)
	cleanup.IssuerID = issuer
	cleared, err = q.ClearExpiredTrustedDelegationAssertion(ctx, cleanup)
	require.NoError(t, err)
	require.Equal(t, int64(1), cleared)
	// Legacy NULL generations behave as generation 1 until a write advances them.
	require.NoError(t, fixtures.ClearDelegationGenerationFixture(ctx, row.ID))
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
	attempted, err := fixtures.GetDelegationRefreshClaimFixture(ctx, testrepo.GetDelegationRefreshClaimFixtureParams{OrganizationID: org, ClientID: client, Subject: subject})
	require.NoError(t, err)
	require.True(t, attempted.LastRefreshAttemptAt.Valid)

	// Claim acquisition invalidates callback snapshots that might preserve a spent token.
	_, err = q.UpsertTrustedDelegationCredential(ctx, params)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.NoError(t, fixtures.BackdateDelegationRefreshAttemptFixture(ctx, row.ID))
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

	// Release remains possible after live trust removal and changes only its own claim.
	claimID = uuid.New()
	claimed, err = q.ClaimTrustedDelegationRefresh(ctx, repo.ClaimTrustedDelegationRefreshParams{OrganizationID: org, ClientID: client, IssuerID: issuer, SubjectUrn: subject, ExpectedGeneration: completed.CredentialGeneration.Int64, RefreshClaimID: claimID})
	require.NoError(t, err)
	require.NoError(t, fixtures.RevokeDelegationUserIssuersFixture(ctx, org))
	release := repo.ReleaseTrustedDelegationRefreshParams{OrganizationID: org, IssuerID: issuer, ClientID: client, SubjectUrn: subject, ExpectedGeneration: claimed.CredentialGeneration.Int64, RefreshClaimID: claimID}
	for _, wrong := range []string{"organization", "issuer", "client", "subject", "generation", "claim"} {
		bad := release
		switch wrong {
		case "organization":
			bad.OrganizationID = "org_other"
		case "issuer":
			bad.IssuerID = uuid.New()
		case "client":
			bad.ClientID = uuid.New()
		case "subject":
			bad.SubjectUrn = "user:other"
		case "generation":
			bad.ExpectedGeneration--
		case "claim":
			bad.RefreshClaimID = uuid.New()
		}
		n, err := q.ReleaseTrustedDelegationRefresh(ctx, bad)
		require.NoError(t, err)
		require.Zero(t, n, wrong)
	}
	released, err := q.ReleaseTrustedDelegationRefresh(ctx, release)
	require.NoError(t, err)
	require.Equal(t, int64(1), released)
	released, err = q.ReleaseTrustedDelegationRefresh(ctx, release)
	require.NoError(t, err)
	require.Zero(t, released)
	require.NoError(t, fixtures.RestoreDelegationUserIssuersFixture(ctx, org))
	completed, err = q.GetTrustedDelegationCredential(ctx, get)
	require.NoError(t, err)
	require.Equal(t, "rotated-refresh", completed.RefreshTokenEncrypted.String)
	require.False(t, completed.RefreshClaimID.Valid)
	get.OrganizationID = "org_another_test"
	_, err = q.GetTrustedDelegationCredential(ctx, get)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	get.OrganizationID = org
	// Expiry clears assertion and refresh, plus identity-only residue, even when inactive.
	require.NoError(t, fixtures.ExpireDelegationCredentialFixture(ctx, row.ID))
	// A tenant cleanup must not consume another tenant's expiration budget.
	foreignCount, err := q.CleanupTrustedDelegationCredentialsBatch(ctx, repo.CleanupTrustedDelegationCredentialsBatchParams{OrganizationID: text("org_other"), BatchSize: 500})
	require.NoError(t, err)
	require.Zero(t, foreignCount)
	count, err := q.CleanupTrustedDelegationCredentialsBatch(ctx, repo.CleanupTrustedDelegationCredentialsBatchParams{OrganizationID: text(org), BatchSize: 1})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	read, err = q.GetTrustedDelegationCredential(ctx, get)
	require.NoError(t, err)
	require.False(t, read.IdentityAssertionEncrypted.Valid)
	require.False(t, read.RefreshTokenEncrypted.Valid)
	require.False(t, read.UpstreamSubjectEncrypted.Valid)
	require.False(t, read.NonceEncrypted.Valid)
	count, err = q.CleanupTrustedDelegationCredentialsBatch(ctx, repo.CleanupTrustedDelegationCredentialsBatchParams{OrganizationID: text(org), BatchSize: 1})
	require.NoError(t, err)
	require.Zero(t, count)
	params.ExpectedGeneration = read.CredentialGeneration.Int64
	_, err = q.UpsertTrustedDelegationCredential(ctx, params)
	require.NoError(t, err)
	// Aggregate timestamps describe current-registration observations only.
	require.NoError(t, fixtures.MarkDelegationObservedFixture(ctx, row.ID))
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
	require.NoError(t, fixtures.ExpireDelegationRefreshFixture(ctx, row.ID))
	counts, err = q.CountTrustedDelegationObservations(ctx, repo.CountTrustedDelegationObservationsParams{OrganizationID: org, ClientID: client, ConfigHash: "config", ObservedSince: pgtype.Timestamptz{Time: time.Now().Add(-30 * 24 * time.Hour), Valid: true}})
	require.NoError(t, err)
	require.Len(t, counts, 1)
	require.Equal(t, "reauthentication_required", counts[0].ObservationStatus)
	observed := counts[0].LastObservedAt
	count, err = q.CleanupTrustedDelegationCredentialsBatch(ctx, repo.CleanupTrustedDelegationCredentialsBatchParams{OrganizationID: text(org), BatchSize: 1})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	read, err = q.GetTrustedDelegationCredential(ctx, get)
	require.NoError(t, err)
	require.False(t, read.IdentityAssertionEncrypted.Valid)
	require.False(t, read.UpstreamSubjectEncrypted.Valid)
	require.Equal(t, observed, read.ObservedAt)
	// Stale expiry metadata without its secret must not starve later batches.
	require.NoError(t, fixtures.SetStaleDelegationRefreshExpiryFixture(ctx, row.ID))
	count, err = q.CleanupTrustedDelegationCredentialsBatch(ctx, repo.CleanupTrustedDelegationCredentialsBatchParams{OrganizationID: text(org), BatchSize: 1})
	require.NoError(t, err)
	require.Zero(t, count)
	// An invalid project-scoped row is maintenance-orphaned, not skipped forever.
	project := uuid.New()
	_, err = fixtures.CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{ID: project, Name: "Cleanup test", Slug: "cleanup-test", OrganizationID: org})
	require.NoError(t, err)
	require.NoError(t, fixtures.SetDelegationProjectFixture(ctx, testrepo.SetDelegationProjectFixtureParams{ID: row.ID, ProjectID: uuid.NullUUID{UUID: project, Valid: true}}))
	count, err = q.CleanupTrustedDelegationCredentialsBatch(ctx, repo.CleanupTrustedDelegationCredentialsBatchParams{OrganizationID: text(org), BatchSize: 1})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	_, err = fixtures.GetDelegationAssertionOrSubjectPresentFixture(ctx, row.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	params.ExpectedGeneration = 0
	row, err = q.UpsertTrustedDelegationCredential(ctx, params)
	require.NoError(t, err)
	// Explicit revocation must work after removal of trust, without a live read.
	require.NoError(t, fixtures.RevokeDelegationUserIssuersFixture(ctx, org))
	revocation := repo.RevokeTrustedDelegationCredentialParams{OrganizationID: org, ClientID: client, IssuerID: uuid.New(), SubjectUrn: subject}
	revoked, err := q.RevokeTrustedDelegationCredential(ctx, revocation)
	require.NoError(t, err)
	require.Zero(t, revoked)
	revocation.IssuerID = issuer
	revoked, err = q.RevokeTrustedDelegationCredential(ctx, revocation)
	require.NoError(t, err)
	require.Equal(t, int64(1), revoked)
	revokedState, err := fixtures.GetDelegationSecretsAndGenerationFixture(ctx, row.ID)
	require.NoError(t, err)
	require.False(t, revokedState.Present)
	require.True(t, revokedState.CredentialGeneration.Valid)
	require.Greater(t, revokedState.CredentialGeneration.Int64, row.CredentialGeneration.Int64)
	// A removed membership blocks all reads before any decryption, and cleanup erases residue.
	require.NoError(t, fixtures.ForceSoftDeleteOrganizationUserRelationshipsFixture(ctx, org))
	_, err = q.GetTrustedDelegationCredential(ctx, get)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	count, err = q.CleanupTrustedDelegationCredentialsBatch(ctx, repo.CleanupTrustedDelegationCredentialsBatchParams{OrganizationID: text(org), BatchSize: 1})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	_, err = fixtures.GetDelegationSecretsPresentFixture(ctx, row.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	for _, lifecycle := range []string{"deleted", "orphaned", "secret-free-orphan", "secret-free-deleted", "secret-free-org-orphan"} {
		{
			t.Logf("cleanup lifecycle: %s", lifecycle)
			id := uuid.New()
			err := fixtures.SeedDelegationLifecycleSessionFixture(ctx, testrepo.SeedDelegationLifecycleSessionFixtureParams{
				ID: id, OrganizationID: text(org), RemoteSessionClientID: uuid.NullUUID{UUID: client, Valid: true}, Lifecycle: lifecycle,
			})
			require.NoError(t, err)
			if lifecycle == "orphaned" {
				require.NoError(t, fixtures.OrphanDelegationSessionFixture(ctx, id))
			}
			if lifecycle == "secret-free-orphan" {
				require.NoError(t, fixtures.StripDelegationOrphanSecretsFixture(ctx, id))
			}
			if lifecycle == "secret-free-deleted" || lifecycle == "secret-free-org-orphan" {
				require.NoError(t, fixtures.StripDelegationLifecycleSecretsFixture(ctx, testrepo.StripDelegationLifecycleSecretsFixtureParams{ID: id, Lifecycle: lifecycle}))
			}
			cleanupOrg := text(org)
			if lifecycle == "orphaned" || lifecycle == "secret-free-org-orphan" {
				cleanupOrg = pgtype.Text{}
			}
			count, err := q.CleanupTrustedDelegationCredentialsBatch(ctx, repo.CleanupTrustedDelegationCredentialsBatchParams{OrganizationID: cleanupOrg, BatchSize: 1})
			require.NoError(t, err)
			require.Equal(t, int64(1), count)
			_, err = fixtures.GetDelegationSecretsPresentFixture(ctx, id)
			require.ErrorIs(t, err, pgx.ErrNoRows)
			count, err = q.CleanupTrustedDelegationCredentialsBatch(ctx, repo.CleanupTrustedDelegationCredentialsBatchParams{OrganizationID: text(org), BatchSize: 1})
			require.NoError(t, err)
			require.Zero(t, count)
		}
	}
}
