package remotesessions

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urls"
)

// Preparation mutations coordinate tenant authorization, lifecycle locks, and
// durable registration claims. Read-only snapshots, readiness policy, and view
// projection live in preparationread.go, preparationpolicy.go, and preparationview.go.

func setPreparationBinding(ctx context.Context, q *repo.Queries, b repo.RemoteSessionEmaBinding, previous int64) (repo.RemoteSessionEmaBinding, error) {
	result, err := q.SetEMABinding(ctx, repo.SetEMABindingParams{ID: b.ID, ProjectID: b.ProjectID, OrganizationID: b.OrganizationID, RemoteSessionClientID: b.RemoteSessionClientID, Generation: b.Generation, ExpectedGeneration: previous, State: conv.ToPGText(preparationBindingState(b.State)), GrantSource: conv.ToPGText(preparationBindingGrantSource(b.GrantSource)), RequestedScopes: b.RequestedScopes, ClaimID: b.ClaimID, ClaimedAt: b.ClaimedAt})
	if err != nil {
		return result, oops.E(oops.CodeUnexpected, err, "persist preparation binding")
	}
	return result, nil
}

// PrepareIdentityChaining is the only registration entrypoint. Discovery never
// invokes it. Its durable DCR claim commits before HTTP and cannot be replayed.
func (s *Service) PrepareIdentityChaining(ctx context.Context, in PreparationInput) (*PreparationResult, error) {
	return s.prepareIdentityChaining(ctx, in, false)
}
func (s *Service) UnlinkIdentityChaining(ctx context.Context, in PreparationInput) (*PreparationResult, error) {
	return s.prepareIdentityChaining(ctx, in, true)
}
func (s *Service) ReadIdentityChaining(ctx context.Context, in PreparationInput) (*PreparationResult, error) {
	return s.readIdentityChaining(ctx, in)
}

func (s *Service) prepareIdentityChaining(ctx context.Context, in PreparationInput, unlink bool) (*PreparationResult, error) {
	var emptyClient repo.RemoteSessionClient
	project, org, err := s.preparationTenant(ctx, true)
	if err != nil {
		return nil, err
	}
	in, err = normalizePreparationInput(in)
	if err != nil {
		return nil, err
	}
	// A connection-scoped lock serializes this binding across the claim commit,
	// HTTP submission and outcome commit without an open transaction during HTTP.
	// Process/connection loss releases the lock; the durable claim still prevents
	// replay of an uncertain registration.
	releaseAdmission, err := admitRegistration(ctx, s.db)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "admit preparation binding")
	}
	defer releaseAdmission()

	conn, err := s.db.Acquire(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock preparation binding")
	}
	defer conn.Release()
	lockKey := strings.Join([]string{"ema-preparation", org, project.String(), in.UserSessionIssuerID.String(), in.RemoteSessionIssuerID.String(), in.Resource}, "\n")
	lockRepo := repo.New(conn)
	if err := lockRepo.LockPreparationSubmission(ctx, lockKey); err != nil {
		// A canceled lock request can have acquired the lock at the server.
		_ = conn.Conn().Close(context.Background())
		return nil, oops.E(oops.CodeUnexpected, err, "lock preparation binding")
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := lockRepo.UnlockPreparationSubmission(unlockCtx, lockKey); err != nil {
			_ = conn.Conn().Close(context.Background())
		}
	}()
	// The shared registration lock closes the preflight-to-HTTP race with
	// interactive rotation. Reject foreign IDs before taking its global key.
	if _, err := lockRepo.GetRemoteSessionIssuerByID(ctx, repo.GetRemoteSessionIssuerByIDParams{ID: in.RemoteSessionIssuerID, ProjectID: conv.ToNullUUID(project), OrganizationID: conv.ToPGText(org), IncludeOrganizational: true, IncludeGlobal: true}); err != nil {
		return nil, preparationLookupError(err, "remote issuer not found")
	}
	releaseRegistration, err := lockRegistrationIssuer(ctx, conn, in.RemoteSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock issuer registration")
	}
	defer releaseRegistration()
	if err := s.recheckPreparationTenant(ctx, project, org, true); err != nil {
		return nil, err
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := authclientbackfill.LockEMAPreparationScope(ctx, tx, org, project, in.RemoteSessionIssuerID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock identity chaining sources")
	}
	q := repo.New(tx)
	// Lock the project before parents, including wholly inherited configurations.
	if _, err = q.LockEMAProject(ctx, repo.LockEMAProjectParams{ProjectID: project, OrganizationID: org}); err != nil {
		return nil, preparationLookupError(err, "project not found")
	}
	if err = lockUserSessionIssuersForClientBinding(ctx, s.logger, tx, q, project, org, []uuid.UUID{in.UserSessionIssuerID}); err != nil {
		return nil, err
	}
	if _, err = q.LockEMAUserIssuer(ctx, repo.LockEMAUserIssuerParams{ID: in.UserSessionIssuerID, ProjectID: conv.ToNullUUID(project), OrganizationID: conv.ToPGText(org)}); err != nil {
		return nil, preparationLookupError(err, "user session issuer not found")
	}
	issuer, err := q.LockEMAIssuer(ctx, repo.LockEMAIssuerParams{ID: in.RemoteSessionIssuerID, ProjectID: conv.ToNullUUID(project), OrganizationID: conv.ToPGText(org)})
	if err != nil {
		return nil, preparationLookupError(err, "remote issuer not found")
	}
	if m := in.ResourceMetadata; m != nil && (m.Resource != in.Resource || !slices.Contains(m.AuthorizationServers, issuer.Issuer)) {
		return nil, oops.E(oops.CodeBadRequest, nil, "resource authorization server association mismatch")
	}
	// Parent locks can also wait. Recheck before even creating a binding.
	if err := s.recheckPreparationTenant(ctx, project, org, true); err != nil {
		return nil, err
	}
	// Read the client pointer before the binding lock; issuer lock serializes all
	// preparation changes for this issuer and establishes lifecycle lock ordering.
	key := repo.GetEMABindingParams{ProjectID: project, OrganizationID: org, UserSessionIssuerID: in.UserSessionIssuerID, RemoteSessionIssuerID: in.RemoteSessionIssuerID, Resource: in.Resource}
	b, err := q.GetEMABinding(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		if unlink {
			r := preparationDiagnostic(PreparationStateConfigurationRequired)
			r.Resource = in.Resource
			r.Issuer = issuer.Issuer
			return r, nil
		}
		err = q.EnsureEMABinding(ctx, repo.EnsureEMABindingParams(key))
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
		}
		b, err = q.GetEMABinding(ctx, key)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
	}
	selected := in.ClientID
	if selected == uuid.Nil && b.RemoteSessionClientID.Valid {
		selected = b.RemoteSessionClientID.UUID
	}
	var client repo.RemoteSessionClient
	if selected != uuid.Nil {
		client, err = q.LockEMAClient(ctx, repo.LockEMAClientParams{ID: selected, ProjectID: conv.ToNullUUID(project), OrganizationID: conv.ToPGText(org)})
		if err != nil {
			return nil, preparationLookupError(err, "selected client not found")
		}
		if client.RemoteSessionIssuerID != issuer.ID {
			return nil, oops.E(oops.CodeBadRequest, nil, "selected client belongs to another issuer")
		}
	}
	b, err = q.LockEMABinding(ctx, repo.LockEMABindingParams(key))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
	}
	if err := s.recheckPreparationTenant(ctx, project, org, true); err != nil {
		return nil, err
	}
	oldRegistration := b.RemoteSessionClientID.UUID
	previous := b.Generation
	if unlink {
		if in.ExpectedGeneration != b.Generation {
			return preparationResult(b, issuer, client, PreparationStateConfigurationRequired), oops.E(oops.CodeConflict, nil, "binding generation changed; read current preparation before unlinking")
		}
		before := b
		b.Generation++
		b.State = conv.ToPGText(PreparationStateUnlinked)
		b.RemoteSessionClientID = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
		b.ClaimID = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
		b.ClaimedAt = pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
		b.GrantSource = conv.ToPGText(PreparationGrantSourceUnknown)
		b.RequestedScopes = []string{}
		b, err = setPreparationBinding(ctx, q, b, previous)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
		}
		if err := s.auditIdentityChainingTransition(ctx, tx, before, b, client); err != nil {
			return nil, err
		}
		if err := authclientbackfill.SyncEMARegistrations(ctx, tx, org, []uuid.UUID{oldRegistration}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "reconcile unlinked identity chaining auth client")
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
		}
		return preparationResult(b, issuer, emptyClient, preparationBindingState(b.State)), nil
	}
	if b.ClaimID.Valid && in.ClientID == uuid.Nil && (preparationBindingState(b.State) != PreparationStateProviderRejection || in.ExpectedGeneration != b.Generation) {
		state := preparationBindingState(b.State)
		if state == PreparationStateInProgress && (!b.ClaimedAt.Valid || time.Since(b.ClaimedAt.Time) > time.Minute) {
			state = PreparationStateIndeterminate
			b.Generation++
			b.State = conv.ToPGText(state)
			b, err = setPreparationBinding(ctx, q, b, previous)
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
			}
			if err = tx.Commit(ctx); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
			}
		}
		return preparationResult(b, issuer, client, state), nil
	}
	// Registration evidence is durable; readiness is not. Recompute completed
	// provider registrations without replaying DCR or replacing effective scopes.
	if (in.Mechanism == PreparationMechanismDCR && in.ClientID == uuid.Nil) && b.RemoteSessionClientID.Valid && preparationBindingGrantSource(b.GrantSource) == PreparationGrantSourceProviderReturned {
		state := preparationRegistrationReadiness(ctx, q, client, issuer, org)
		if state != preparationBindingState(b.State) {
			before := b
			b.State = conv.ToPGText(state)
			b, err = setPreparationBinding(ctx, q, b, b.Generation)
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "persist identity chaining readiness")
			}
			if err := s.auditIdentityChainingTransition(ctx, tx, before, b, client); err != nil {
				return nil, err
			}
		}
		if err := authclientbackfill.SyncEMARegistrations(ctx, tx, org, []uuid.UUID{oldRegistration, b.RemoteSessionClientID.UUID}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "synchronize identity chaining auth client")
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
		}
		return preparationResult(b, issuer, client, state), nil
	}
	// Record explicit manual setup independently of automatic readiness.
	manual := selected != uuid.Nil && (in.Mechanism == PreparationMechanismManual || in.Mechanism == "")
	eligibility := IssuerPreparationEligibility(issuer)
	if !manual && eligibility != preparationEligible {
		return preparationResult(b, issuer, client, eligibility), nil
	}
	if !manual && preparationMetadataTransient(issuer) {
		return preparationResult(b, issuer, client, PreparationStateTransientFailure), nil
	}
	if in.Mechanism == PreparationMechanismDCR && (preparationBindingState(b.State) == PreparationStateReady || preparationBindingState(b.State) == PreparationStatePublishedAcceptanceUnverified) {
		if !preparationClientConfigurationValid(ctx, q, client, issuer, org) {
			return preparationResult(b, issuer, client, PreparationStateManualSetupRequired), nil
		}
		if !slices.Contains(client.GrantTypes, oauthwire.GrantTypeJWTBearer) {
			return preparationResult(b, issuer, client, preparationMissingGrantsState(client.GrantTypes)), nil
		}
	}
	// Turn on snapshots scopes onto the binding, and chaining requests only
	// that snapshot, OIDC-reserved scopes dropped. With a connection to follow,
	// its set wins when it names a grantable scope, or when it is non-empty and
	// the request names no grantable scope; otherwise, including an empty
	// connection set, the request's scopes are recorded. DCR registrations
	// always ask for the request's scopes.
	var connection *ScopeResolution
	if selected != uuid.Nil {
		connection, err = ConnectionScopes(ctx, tx, BindingConnection{ProjectID: project, OrganizationID: org, UserSessionIssuerID: in.UserSessionIssuerID, ClientID: selected, Resource: in.Resource, Discover: s.resources != nil})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "resolve identity chaining scopes").LogError(ctx, s.logger)
		}
	}
	scopes := BindingScopes(connection, in.Scopes)
	changed := preparationBindingState(b.State) == PreparationStateProviderRejection || selected != b.RemoteSessionClientID.UUID || preparationBindingState(b.State) == PreparationStateUnlinked || !SameBindingScopes(b.RequestedScopes, scopes.Scopes) || (in.ConfirmGrants != nil && (!samePreparationGrants(in.ConfirmGrants, client.GrantTypes) || preparationBindingGrantSource(b.GrantSource) != PreparationGrantSourceAdministratorDeclared)) || (in.Mechanism == PreparationMechanismCIMD && (!slices.Contains(client.GrantTypes, oauthwire.GrantTypeJWTBearer) || preparationBindingGrantSource(b.GrantSource) != PreparationGrantSourceCIMDPublished))
	if changed && (b.RemoteSessionClientID.Valid || b.ClaimID.Valid || preparationBindingState(b.State) == PreparationStateUnlinked) && in.ExpectedGeneration != b.Generation {
		return preparationResult(b, issuer, client, PreparationStateConfigurationRequired), nil
	}
	if selected == uuid.Nil {
		if in.Mechanism != PreparationMechanismDCR {
			return preparationResult(b, issuer, client, PreparationStateConfigurationRequired), nil
		}
		method := in.TokenEndpointAuthMethod
		if !issuer.RegistrationEndpoint.Valid || (method != oauthwire.AuthMethodClientSecretBasic && method != oauthwire.AuthMethodClientSecretPost) || !slices.Contains(issuer.TokenEndpointAuthMethodsSupported, method) {
			return preparationResult(b, issuer, client, PreparationStateManualSetupRequired), nil
		}
		if !urls.IsAbsoluteHTTPSOrLoopback(issuer.RegistrationEndpoint.String) {
			return preparationResult(b, issuer, client, PreparationStateManualSetupRequired), nil
		}
		// Only mutate the returned binding once the request can be persisted.
		before := b
		if changed {
			b.Generation++
		}
		b.RequestedScopes = scopes.Scopes
		b.State = conv.ToPGText(PreparationStateInProgress)
		b.ClaimID = conv.ToNullUUID(uuid.New())
		b.ClaimedAt = conv.ToPGTimestamptz(time.Now())
		b.GrantSource = conv.ToPGText(PreparationGrantSourceUnknown)
		b, err = setPreparationBinding(ctx, q, b, previous)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
		}
		if err := s.auditIdentityChainingTransition(ctx, tx, before, b, client); err != nil {
			return nil, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
		}
		// The authorized durable claim owns completion. Do not reauthorize after
		// submission: losing permission then must not discard returned credentials
		// or make an externally completed registration replayable.
		return s.finishPreparationDCR(ctx, conn, in, b, issuer, method)
	}
	// Explicit selection is never inferred from interactive attachments or grants.
	if in.Mechanism == PreparationMechanismDCR {
		return preparationResult(b, issuer, client, PreparationStateConfigurationRequired), nil
	}
	grants := client.GrantTypes
	source := preparationBindingGrantSource(b.GrantSource)
	if selected != b.RemoteSessionClientID.UUID {
		// A different client's evidence cannot inherit this binding's source.
		// Explicit manual selection records an administrator declaration, but
		// selecting CIMD alone does not confirm the client's existing grants.
		source = PreparationGrantSourceUnknown
		if manual {
			source = PreparationGrantSourceAdministratorDeclared
		}
	}
	if in.ConfirmGrants != nil {
		grants = slices.Clone(in.ConfirmGrants)
		source = PreparationGrantSourceAdministratorDeclared
	}
	// Legacy grant arrays alone are not recorded registration evidence.
	// Require an explicit confirmation before reusing an unknown source.
	if !preparationGrantSourceRecorded(source) {
		return preparationResult(b, issuer, client, PreparationStateUnknownGrants), nil
	}
	state := PreparationStateReady
	if source == PreparationGrantSourceCIMDPublished {
		state = PreparationStatePublishedAcceptanceUnverified
	}
	if in.Mechanism == PreparationMechanismCIMD {
		if !client.ClientIDMetadataUri.Valid || !issuer.ClientIDMetadataDocumentSupported {
			return preparationResult(b, issuer, client, PreparationStateManualSetupRequired), nil
		}
		// Unknown legacy grants require administrator confirmation, not guesses.
		if grants == nil {
			return preparationResult(b, issuer, client, PreparationStateUnknownGrants), nil
		}
		// Selecting CIMD is not a grant declaration. Reuse recorded evidence or
		// publish exactly ConfirmGrants; never add JWT-bearer implicitly or turn
		// an explicit empty declaration into grants.
		source = PreparationGrantSourceCIMDPublished
		state = PreparationStatePublishedAcceptanceUnverified
	}
	if !slices.Contains(grants, oauthwire.GrantTypeJWTBearer) {
		state = PreparationStateUnknownGrants
		if grants != nil {
			state = PreparationStateManualSetupRequired
		}
	}
	if (state == PreparationStateReady || state == PreparationStatePublishedAcceptanceUnverified) && !preparationClientConfigurationValid(ctx, q, client, issuer, org) {
		return preparationResult(b, issuer, client, PreparationStateManualSetupRequired), nil
	}
	// Only the request's scopes are checked: the connection's are what a
	// sign-in through this client already requests.
	if scopes.Source == remotesessionmetrics.ScopeSourcePreparationRequest && client.Scope != nil {
		for _, scope := range in.Scopes {
			if !slices.Contains(client.Scope, scope) {
				return preparationResult(b, issuer, client, PreparationStateManualSetupRequired), nil
			}
		}
	}
	if !samePreparationGrants(grants, client.GrantTypes) {
		// Publishing/modifying inherited registrations affects other projects and
		// therefore requires organization-level authority in addition to project write.
		if !client.ProjectID.Valid {
			if err = s.authz.Require(ctx, authz.Check{ResourceKind: "", Dimensions: nil, Scope: authz.ScopeOrgAdmin, ResourceID: org}); err != nil {
				return nil, err
			}
		}
		// CIMD grants are also the live interactive registration. Preparation
		// must not disable login or refresh, even when selected as manual.
		// Reject removal rather than silently broadening explicit confirmation.
		// NULL grants publish the legacy interactive compatibility document,
		// but remain unknown registration evidence until explicitly confirmed.
		if client.ClientIDMetadataUri.Valid {
			for _, grant := range []string{oauthwire.GrantTypeAuthorizationCode, oauthwire.GrantTypeRefreshToken} {
				if (client.GrantTypes == nil || slices.Contains(client.GrantTypes, grant)) && !slices.Contains(grants, grant) {
					return nil, oops.E(oops.CodeBadRequest, nil, "preparation cannot remove existing interactive CIMD grants")
				}
			}
		}
		// A grant publication is client-wide. Do not silently reconfigure another
		// resource binding without invalidating its generation as well.
		count, countErr := q.CountActiveEMABindingsForClient(ctx, repo.CountActiveEMABindingsForClientParams{ClientID: conv.ToNullUUID(client.ID), OrganizationID: org, ProjectID: uuid.Nil})
		if countErr != nil {
			return nil, oops.E(oops.CodeUnexpected, countErr, "count client bindings")
		}
		allowed := int64(0)
		if b.RemoteSessionClientID.Valid && b.RemoteSessionClientID.UUID == client.ID && preparationBindingState(b.State) != PreparationStateUnlinked {
			allowed = 1
		}
		if count > allowed {
			return preparationResult(b, issuer, client, PreparationStateConfigurationRequired), nil
		}
		client, err = q.SetEMAClientGrants(ctx, repo.SetEMAClientGrantsParams{ID: client.ID, ProjectID: conv.ToNullUUID(project), OrganizationID: conv.ToPGText(org), GrantTypes: grants})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
		}
	}
	if grants == nil {
		source = PreparationGrantSourceUnknown
	}
	// All configuration-required exits above return the persisted generation
	// and scopes, not a prospective transition that will be rolled back.
	before := b
	if changed {
		b.Generation++
	}
	b.RequestedScopes = scopes.Scopes
	b.RemoteSessionClientID = conv.ToNullUUID(client.ID)
	b.State = conv.ToPGText(state)
	b.GrantSource = conv.ToPGText(source)
	b.ClaimID = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	b.ClaimedAt = pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	b, err = setPreparationBinding(ctx, q, b, previous)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
	}
	if err := s.auditIdentityChainingTransition(ctx, tx, before, b, client); err != nil {
		return nil, err
	}
	if err := authclientbackfill.SyncEMARegistrations(ctx, tx, org, []uuid.UUID{oldRegistration, b.RemoteSessionClientID.UUID}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "synchronize identity chaining auth client")
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "prepare identity chaining")
	}
	if !SameBindingScopes(before.RequestedScopes, b.RequestedScopes) {
		s.logger.InfoContext(ctx, "recorded identity chaining scopes",
			attr.SlogProjectID(project.String()),
			attr.SlogOrganizationID(org),
			attr.SlogRemoteSessionClientID(client.ID.String()),
			attr.SlogOAuthResource(b.Resource),
			attr.SlogOAuthScope(strings.Join(b.RequestedScopes, " ")),
			attr.SlogOAuthScopeSource(scopes.Source),
		)
	}
	// The binding records registration evidence, not discovery eligibility.
	// Reads apply these same gates and recover when discovery becomes eligible.
	if manual && eligibility != preparationEligible {
		state = eligibility
	} else if manual && preparationMetadataTransient(issuer) {
		state = PreparationStateTransientFailure
	}
	return preparationResult(b, issuer, client, state), nil
}
