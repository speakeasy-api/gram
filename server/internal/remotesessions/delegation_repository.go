package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"time"
)

type delegationRepository struct{ db *pgxpool.Pool }

func delegationText(v string) pgtype.Text { return pgtype.Text{String: v, Valid: v != ""} }
func delegationTime(v time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{InfinityModifier: pgtype.Finite, Time: v, Valid: !v.IsZero()}
}
func credentialFromRow(row repo.TrustedIssuerSession) delegationCredential {
	generation := int64(1)
	if row.CredentialGeneration.Valid {
		generation = row.CredentialGeneration.Int64
	}
	return delegationCredential{
		generation: generation, claim: row.RefreshClaimID.UUID,
		assertion:       row.IdentityAssertionEncrypted.String,
		assertionExpiry: row.IdentityAssertionExpiresAt.Time,
		refresh:         row.RefreshTokenEncrypted.String,
		refreshExpiry:   row.RefreshExpiresAt.Time,
		subject:         row.UpstreamSubjectEncrypted.String,
		nonce:           row.NonceEncrypted.String,
		config:          row.CredentialConfigHash.String,
		status:          row.ObservationStatus.String,
		observedAt:      row.ObservedAt.Time,
		obtainedAt:      row.CredentialObtainedAt.Time,
		refreshedAt:     row.LastRefreshSucceededAt.Time,
		retryAfter:      row.RetryAfter.Time,
		refusedAt:       row.OfflineAccessRefusedAt.Time,
		requestConfig:   row.OfflineAccessRequestConfigHash.String,
	}
}
func (r *delegationRepository) load(ctx context.Context, b DelegationBinding) (delegationCredential, error) {
	row, err := repo.New(r.db).GetTrustedDelegationCredential(ctx, repo.GetTrustedDelegationCredentialParams{OrganizationID: b.OrganizationID, ClientID: b.ClientID, IssuerID: b.IssuerID, SubjectUrn: urn.NewUserSubject(b.HumanID).String()})
	if err != nil {
		return delegationCredential{}, fmt.Errorf("load delegation credential: %w", err)
	}
	return credentialFromRow(row), nil
}
func (r *delegationRepository) save(ctx context.Context, b DelegationBinding, expected int64, c delegationCredential) (bool, error) {
	_, err := repo.New(r.db).UpsertTrustedDelegationCredential(ctx, repo.UpsertTrustedDelegationCredentialParams{
		OrganizationID: b.OrganizationID, ClientID: b.ClientID, IssuerID: b.IssuerID, SubjectUrn: urn.NewUserSubject(b.HumanID).String(), ExpectedGeneration: expected,
		IdentityAssertionEncrypted:     delegationText(c.assertion),
		IdentityAssertionExpiresAt:     delegationTime(c.assertionExpiry),
		RefreshTokenEncrypted:          delegationText(c.refresh),
		RefreshExpiresAt:               delegationTime(c.refreshExpiry),
		UpstreamSubjectEncrypted:       delegationText(c.subject),
		NonceEncrypted:                 delegationText(c.nonce),
		CredentialConfigHash:           delegationText(c.config),
		ObservationStatus:              delegationText(c.status),
		ObservedAt:                     delegationTime(c.observedAt),
		CredentialObtainedAt:           delegationTime(c.obtainedAt),
		LastRefreshSucceededAt:         delegationTime(c.refreshedAt),
		RetryAfter:                     delegationTime(c.retryAfter),
		OfflineAccessRefusedAt:         delegationTime(c.refusedAt),
		OfflineAccessRequestConfigHash: delegationText(c.requestConfig),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("update delegation credential: %w", err)
	}
	return true, nil
}
func (r *delegationRepository) claim(ctx context.Context, b DelegationBinding, generation int64, claim uuid.UUID, _ time.Time) (bool, error) {
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
func (r *delegationRepository) finish(ctx context.Context, b DelegationBinding, generation int64, claim uuid.UUID, c delegationCredential) (bool, error) {
	_, err := repo.New(r.db).CompleteTrustedDelegationRefresh(ctx, repo.CompleteTrustedDelegationRefreshParams{
		OrganizationID: b.OrganizationID, ClientID: b.ClientID, IssuerID: b.IssuerID, SubjectUrn: urn.NewUserSubject(b.HumanID).String(), ExpectedGeneration: generation, RefreshClaimID: claim, NextRefreshClaimID: uuid.NullUUID{UUID: c.claim, Valid: c.claim != uuid.Nil},
		IdentityAssertionEncrypted:     delegationText(c.assertion),
		IdentityAssertionExpiresAt:     delegationTime(c.assertionExpiry),
		RefreshTokenEncrypted:          delegationText(c.refresh),
		RefreshExpiresAt:               delegationTime(c.refreshExpiry),
		UpstreamSubjectEncrypted:       delegationText(c.subject),
		NonceEncrypted:                 delegationText(c.nonce),
		CredentialConfigHash:           delegationText(c.config),
		ObservationStatus:              delegationText(c.status),
		ObservedAt:                     delegationTime(c.observedAt),
		CredentialObtainedAt:           delegationTime(c.obtainedAt),
		LastRefreshSucceededAt:         delegationTime(c.refreshedAt),
		RetryAfter:                     delegationTime(c.retryAfter),
		OfflineAccessRefusedAt:         delegationTime(c.refusedAt),
		OfflineAccessRequestConfigHash: delegationText(c.requestConfig),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("update delegation credential: %w", err)
	}
	return true, nil
}
func (r *delegationRepository) clearExpired(ctx context.Context, b DelegationBinding, _ time.Time) error {
	_, err := repo.New(r.db).ClearExpiredTrustedDelegationAssertion(ctx, repo.ClearExpiredTrustedDelegationAssertionParams{OrganizationID: b.OrganizationID, ClientID: b.ClientID, IssuerID: b.IssuerID, SubjectUrn: urn.NewUserSubject(b.HumanID).String()})
	if err != nil {
		return fmt.Errorf("clear delegation credential: %w", err)
	}
	return nil
}

// revoke deliberately bypasses live trust checks: authorized erasure must still
// work after trust removal. The update atomically invalidates pending refreshes.
func (r *delegationRepository) revoke(ctx context.Context, b DelegationBinding) error {
	_, err := repo.New(r.db).RevokeTrustedDelegationCredential(ctx, repo.RevokeTrustedDelegationCredentialParams{
		OrganizationID: b.OrganizationID, ClientID: b.ClientID, IssuerID: b.IssuerID, SubjectUrn: urn.NewUserSubject(b.HumanID).String(),
	})
	if err != nil {
		return fmt.Errorf("clear delegation credential: %w", err)
	}
	return nil
}

func (r *delegationRepository) markRefreshAttempt(ctx context.Context, b DelegationBinding, generation int64, claim uuid.UUID, _ time.Time) (bool, error) {
	count, err := repo.New(r.db).MarkTrustedDelegationRefreshAttempt(ctx, repo.MarkTrustedDelegationRefreshAttemptParams{
		OrganizationID: b.OrganizationID, ClientID: b.ClientID, IssuerID: b.IssuerID, SubjectUrn: urn.NewUserSubject(b.HumanID).String(), ExpectedGeneration: generation, RefreshClaimID: claim,
	})
	if err != nil {
		return false, fmt.Errorf("mark delegation refresh attempt: %w", err)
	}
	return count == 1, nil
}
