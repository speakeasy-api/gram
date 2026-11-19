package delegation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/upstreamtokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type delegationRepository struct {
	db     *pgxpool.Pool
	tokens *upstreamtokens.Tokens
}

func delegationTime(v time.Time) pgtype.Timestamptz {
	return conv.PtrToPGTimestamptz(conv.PtrEmpty(v))
}
func credentialFromRow(row repo.TrustedIssuerSession) delegationCredential {
	generation := int64(1)
	if row.CredentialGeneration.Valid {
		generation = row.CredentialGeneration.Int64
	}
	return delegationCredential{
		id:         row.ID,
		generation: generation, claim: row.RefreshClaimID.UUID,
		assertion:       storedSecret(row.IdentityAssertionEncrypted, row.IdentityAssertionOrgEncrypted),
		assertionExpiry: row.IdentityAssertionExpiresAt.Time,
		refresh:         storedSecret(row.RefreshTokenEncrypted, row.RefreshTokenOrgEncrypted),
		refreshExpiry:   row.RefreshExpiresAt.Time,
		subject:         storedSecret(row.UpstreamSubjectEncrypted, row.UpstreamSubjectOrgEncrypted),
		nonce:           storedSecret(row.NonceEncrypted, row.NonceOrgEncrypted),
		config:          row.CredentialConfigHash.String,
		status:          row.ObservationStatus.String,
		observedAt:      row.ObservedAt.Time,
		obtainedAt:      row.CredentialObtainedAt.Time,
		refreshedAt:     row.LastRefreshSucceededAt.Time,
		retryAfter:      row.RetryAfter.Time,
		refusedAt:       row.OfflineAccessRefusedAt.Time,
		requestConfig:   row.OfflineAccessRequestConfigHash.String,
		updatedAt:       row.UpdatedAt.Time,
	}
}
func (r *delegationRepository) load(ctx context.Context, b Binding) (delegationCredential, error) {
	row, err := repo.New(r.db).GetTrustedDelegationCredential(ctx, repo.GetTrustedDelegationCredentialParams{OrganizationID: b.OrganizationID, ClientID: b.ClientID, IssuerID: b.IssuerID, SubjectUrn: urn.NewUserSubject(b.HumanID).String()})
	if err != nil {
		return delegationCredential{}, fmt.Errorf("load delegation credential: %w", err)
	}
	return credentialFromRow(row), nil
}
func (r *delegationRepository) save(ctx context.Context, b Binding, expected int64, c delegationCredential) (bool, error) {
	return r.write(ctx, b, c, func(q *repo.Queries) (repo.TrustedIssuerSession, error) {
		return q.UpsertTrustedDelegationCredential(ctx, repo.UpsertTrustedDelegationCredentialParams{
			OrganizationID: b.OrganizationID, ClientID: b.ClientID, IssuerID: b.IssuerID, SubjectUrn: urn.NewUserSubject(b.HumanID).String(), ExpectedGeneration: expected,
			IdentityAssertionExpiresAt:     delegationTime(c.assertionExpiry),
			RefreshExpiresAt:               delegationTime(c.refreshExpiry),
			CredentialConfigHash:           conv.ToPGTextEmpty(c.config),
			ObservationStatus:              conv.ToPGTextEmpty(c.status),
			ObservedAt:                     delegationTime(c.observedAt),
			CredentialObtainedAt:           delegationTime(c.obtainedAt),
			LastRefreshSucceededAt:         delegationTime(c.refreshedAt),
			RetryAfter:                     delegationTime(c.retryAfter),
			OfflineAccessRefusedAt:         delegationTime(c.refusedAt),
			OfflineAccessRequestConfigHash: conv.ToPGTextEmpty(c.requestConfig),
		})
	})
}

// write runs a compare-and-swap statement that changes everything about a
// credential but its secrets, then stores, keeps or erases each secret through
// the seam, in one transaction. It reports false when the compare-and-swap
// lost. A secret that cannot be stored rolls back the whole write.
func (r *delegationRepository) write(ctx context.Context, b Binding, c delegationCredential, update func(*repo.Queries) (repo.TrustedIssuerSession, error)) (bool, error) {
	dbtx, err := r.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin delegation credential write: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := update(repo.New(dbtx))
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("update delegation credential: %w", err)
	}

	if _, err := r.tokens.SetTrustedIssuerSession(ctx, dbtx, row.ID, row.OrganizationID, upstreamtokens.TrustedIssuerSessionValues{
		IdentityAssertion: c.assertion.write(),
		RefreshToken:      c.refresh.write(),
		UpstreamSubject:   c.subject.write(),
		Nonce:             c.nonce.write(),
	}); err != nil {
		return false, fmt.Errorf("store delegation credential secrets: %w", err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit delegation credential write: %w", err)
	}
	return true, nil
}
func (r *delegationRepository) claim(ctx context.Context, b Binding, generation int64, claim uuid.UUID, _ time.Time) (bool, error) {
	_, err := repo.New(r.db).ClaimTrustedDelegationRefresh(ctx, repo.ClaimTrustedDelegationRefreshParams{
		OrganizationID: b.OrganizationID, ClientID: b.ClientID, IssuerID: b.IssuerID, SubjectUrn: urn.NewUserSubject(b.HumanID).String(), ExpectedGeneration: generation, RefreshClaimID: claim,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("update delegation credential: %w", err)
	}
	return true, nil
}
func (r *delegationRepository) finish(ctx context.Context, b Binding, generation int64, claim uuid.UUID, c delegationCredential) (bool, error) {
	return r.write(ctx, b, c, func(q *repo.Queries) (repo.TrustedIssuerSession, error) {
		return q.CompleteTrustedDelegationRefresh(ctx, repo.CompleteTrustedDelegationRefreshParams{
			OrganizationID: b.OrganizationID, ClientID: b.ClientID, IssuerID: b.IssuerID, SubjectUrn: urn.NewUserSubject(b.HumanID).String(), ExpectedGeneration: generation, RefreshClaimID: claim, NextRefreshClaimID: uuid.NullUUID{UUID: c.claim, Valid: c.claim != uuid.Nil},
			IdentityAssertionExpiresAt:     delegationTime(c.assertionExpiry),
			RefreshExpiresAt:               delegationTime(c.refreshExpiry),
			CredentialConfigHash:           conv.ToPGTextEmpty(c.config),
			ObservationStatus:              conv.ToPGTextEmpty(c.status),
			ObservedAt:                     delegationTime(c.observedAt),
			CredentialObtainedAt:           delegationTime(c.obtainedAt),
			LastRefreshSucceededAt:         delegationTime(c.refreshedAt),
			RetryAfter:                     delegationTime(c.retryAfter),
			OfflineAccessRefusedAt:         delegationTime(c.refusedAt),
			OfflineAccessRequestConfigHash: conv.ToPGTextEmpty(c.requestConfig),
		})
	})
}
func (r *delegationRepository) clearExpired(ctx context.Context, b Binding, _ time.Time) error {
	_, err := repo.New(r.db).ClearExpiredTrustedDelegationAssertion(ctx, repo.ClearExpiredTrustedDelegationAssertionParams{OrganizationID: b.OrganizationID, ClientID: b.ClientID, IssuerID: b.IssuerID, SubjectUrn: urn.NewUserSubject(b.HumanID).String()})
	if err != nil {
		return fmt.Errorf("clear delegation credential: %w", err)
	}
	return nil
}

// revoke deliberately bypasses live trust checks: authorized erasure must still
// work after trust removal. The update atomically invalidates pending refreshes.
func (r *delegationRepository) revoke(ctx context.Context, b Binding) error {
	_, err := repo.New(r.db).RevokeTrustedDelegationCredential(ctx, repo.RevokeTrustedDelegationCredentialParams{
		OrganizationID: b.OrganizationID, ClientID: b.ClientID, IssuerID: b.IssuerID, SubjectUrn: urn.NewUserSubject(b.HumanID).String(),
	})
	if err != nil {
		return fmt.Errorf("clear delegation credential: %w", err)
	}
	return nil
}

func (r *delegationRepository) markRefreshAttempt(ctx context.Context, b Binding, generation int64, claim uuid.UUID, _ time.Time) (bool, error) {
	count, err := repo.New(r.db).MarkTrustedDelegationRefreshAttempt(ctx, repo.MarkTrustedDelegationRefreshAttemptParams{
		OrganizationID: b.OrganizationID, ClientID: b.ClientID, IssuerID: b.IssuerID, SubjectUrn: urn.NewUserSubject(b.HumanID).String(), ExpectedGeneration: generation, RefreshClaimID: claim,
	})
	if err != nil {
		return false, fmt.Errorf("mark delegation refresh attempt: %w", err)
	}
	return count == 1, nil
}

func (r *delegationRepository) release(ctx context.Context, b Binding, generation int64, claim uuid.UUID) (bool, error) {
	count, err := repo.New(r.db).ReleaseTrustedDelegationRefresh(ctx, repo.ReleaseTrustedDelegationRefreshParams{
		OrganizationID: b.OrganizationID, IssuerID: b.IssuerID, ClientID: b.ClientID, SubjectUrn: urn.NewUserSubject(b.HumanID).String(), ExpectedGeneration: generation, RefreshClaimID: claim,
	})
	if err != nil {
		return false, fmt.Errorf("release delegation refresh claim: %w", err)
	}
	return count == 1, nil
}
