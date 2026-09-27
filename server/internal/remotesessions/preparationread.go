package remotesessions

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/speakeasy-api/gram/server/internal/oauthwire"

	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// Reads use one non-locking snapshot. A stale claim is projected as indeterminate;
// only the write path persists lifecycle transitions.
func (s *Service) readIdentityChaining(ctx context.Context, in PreparationInput) (*PreparationResult, error) {
	project, org, err := s.preparationTenant(ctx, false)
	if err != nil {
		return nil, err
	}
	in, err = normalizePreparationInput(in)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly, DeferrableMode: "", BeginQuery: "", CommitQuery: ""})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read preparation")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	q := repo.New(tx)
	if _, err = q.ReadEMAProject(ctx, repo.ReadEMAProjectParams{ProjectID: project, OrganizationID: org}); err != nil {
		return nil, preparationLookupError(err, "project not found")
	}
	if _, err = q.ReadEMAUserIssuer(ctx, repo.ReadEMAUserIssuerParams{ID: in.UserSessionIssuerID, ProjectID: conv.ToNullUUID(project), OrganizationID: conv.ToPGText(org)}); err != nil {
		return nil, preparationLookupError(err, "user issuer not found")
	}
	issuer, err := q.ReadEMAIssuer(ctx, repo.ReadEMAIssuerParams{ID: in.RemoteSessionIssuerID, ProjectID: conv.ToNullUUID(project), OrganizationID: conv.ToPGText(org)})
	if err != nil {
		return nil, preparationLookupError(err, "remote issuer not found")
	}
	if m := in.ResourceMetadata; m != nil && (m.Resource != in.Resource || !slices.Contains(m.AuthorizationServers, issuer.Issuer)) {
		return nil, oops.E(oops.CodeBadRequest, nil, "resource authorization server association mismatch")
	}
	b, err := q.GetEMABinding(ctx, repo.GetEMABindingParams{ProjectID: project, OrganizationID: org, UserSessionIssuerID: in.UserSessionIssuerID, RemoteSessionIssuerID: in.RemoteSessionIssuerID, Resource: in.Resource})
	if errors.Is(err, pgx.ErrNoRows) {
		state := PreparationStateConfigurationRequired
		if eligibility := PreparationEligibility(issuer.AuthorizationGrantProfilesSupported, issuer.GrantTypesSupported); eligibility != preparationEligible {
			state = eligibility
		} else if preparationMetadataTransient(issuer) {
			state = PreparationStateTransientFailure
		}
		r := preparationDiagnostic(state)
		r.Resource = in.Resource
		r.Issuer = issuer.Issuer
		return r, nil
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read preparation")
	}
	var client repo.RemoteSessionClient
	if b.RemoteSessionClientID.Valid {
		client, err = q.ReadEMAClient(ctx, repo.ReadEMAClientParams{ID: b.RemoteSessionClientID.UUID, ProjectID: conv.ToNullUUID(project), OrganizationID: conv.ToPGText(org)})
		if err != nil {
			return nil, preparationLookupError(err, "selected client not found")
		}
		if client.RemoteSessionIssuerID != issuer.ID {
			return nil, oops.E(oops.CodeBadRequest, nil, "selected client belongs to another issuer")
		}
	}
	if b.ClaimID.Valid {
		state := preparationBindingState(b.State)
		if state == PreparationStateInProgress && (!b.ClaimedAt.Valid || time.Since(b.ClaimedAt.Time) > time.Minute) {
			state = PreparationStateIndeterminate
		}
		return preparationResult(b, issuer, client, state), nil
	}
	// Registration evidence is durable; readiness is not. Recompute completed
	// provider registrations without replaying DCR or replacing effective scopes.
	if b.RemoteSessionClientID.Valid && preparationBindingGrantSource(b.GrantSource) == PreparationGrantSourceProviderReturned {
		return preparationResult(b, issuer, client, preparationRegistrationReadiness(ctx, q, client, issuer, org, true)), nil
	}
	eligibility := PreparationEligibility(issuer.AuthorizationGrantProfilesSupported, issuer.GrantTypesSupported)
	if eligibility != preparationEligible {
		return preparationResult(b, issuer, client, eligibility), nil
	}
	if preparationMetadataTransient(issuer) {
		return preparationResult(b, issuer, client, PreparationStateTransientFailure), nil
	}
	if preparationBindingState(b.State) == PreparationStateReady || preparationBindingState(b.State) == PreparationStatePublishedAcceptanceUnverified {
		if !preparationClientConfigurationValid(ctx, q, client, issuer, org, true) {
			return preparationResult(b, issuer, client, PreparationStateManualSetupRequired), nil
		}
		if !slices.Contains(client.GrantTypes, oauthwire.GrantTypeJWTBearer) {
			return preparationResult(b, issuer, client, preparationMissingGrantsState(client.GrantTypes)), nil
		}
	}
	return preparationResult(b, issuer, client, preparationBindingState(b.State)), nil
}
