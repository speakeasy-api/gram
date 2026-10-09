package remotesessions

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type identityChainingAuditAction int

const (
	identityChainingAuditEnable identityChainingAuditAction = iota
	identityChainingAuditUpdateScopes
	identityChainingAuditDisable
)

// auditIdentityChaining records a binding change on clientID in the binding's transaction.
func (s *Service) auditIdentityChaining(ctx context.Context, tx pgx.Tx, action identityChainingAuditAction, b repo.RemoteSessionEmaBinding, clientID uuid.UUID, externalClientID string, scopesBefore []string) error {
	actor, displayName, err := preparationAuditActor(ctx)
	if err != nil {
		return err
	}
	event := audit.LogRemoteSessionClientIdentityChainingEvent{
		OrganizationID:         b.OrganizationID,
		ProjectID:              b.ProjectID,
		Actor:                  actor,
		ActorDisplayName:       displayName,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(clientID),
		ClientID:               externalClientID,
		BindingID:              b.ID,
		UserSessionIssuerURN:   urn.NewUserSessionIssuer(b.UserSessionIssuerID),
		RemoteSessionIssuerURN: urn.NewRemoteSessionIssuer(b.RemoteSessionIssuerID),
		Resource:               b.Resource,
		Generation:             b.Generation,
		State:                  preparationBindingState(b.State),
		ScopesBefore:           scopesBefore,
		ScopesAfter:            b.RequestedScopes,
	}
	switch action {
	case identityChainingAuditEnable:
		err = s.auditLogger.LogRemoteSessionClientEnableIdentityChaining(ctx, tx, event)
	case identityChainingAuditUpdateScopes:
		err = s.auditLogger.LogRemoteSessionClientUpdateIdentityChainingScopes(ctx, tx, event)
	case identityChainingAuditDisable:
		err = s.auditLogger.LogRemoteSessionClientDisableIdentityChaining(ctx, tx, event)
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "audit identity chaining change")
	}
	return nil
}

// preparationAuditActor is the request's authenticated actor; only a user actor carries the session email as its display name.
func preparationAuditActor(ctx context.Context) (urn.Principal, *string, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return urn.Principal{}, nil, oops.C(oops.CodeUnauthorized)
	}
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	if !ok {
		if authCtx.UserID == "" {
			return urn.Principal{}, nil, oops.C(oops.CodeUnauthorized)
		}
		actor = urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	}
	if actor.Type != urn.PrincipalTypeUser {
		return actor, nil, nil
	}
	return actor, authCtx.Email, nil
}

// identityChainingActive reports whether b routes users through a client, which only a ready binding does.
func identityChainingActive(b repo.RemoteSessionEmaBinding) bool {
	return b.RemoteSessionClientID.Valid && preparationBindingState(b.State) == PreparationStateReady
}

// auditIdentityChainingTransition records what a binding write changed: a client losing or gaining a ready binding, or a scope change while it stays ready on the same client.
func (s *Service) auditIdentityChainingTransition(ctx context.Context, tx pgx.Tx, before, after repo.RemoteSessionEmaBinding, client repo.RemoteSessionClient) error {
	wasActive, isActive := identityChainingActive(before), identityChainingActive(after)
	sameClient := wasActive && isActive && before.RemoteSessionClientID.UUID == after.RemoteSessionClientID.UUID
	if wasActive && !sameClient {
		if err := s.auditIdentityChainingDisable(ctx, tx, before, after, client); err != nil {
			return err
		}
	}
	switch {
	case isActive && !sameClient:
		external, err := identityChainingExternalClientID(ctx, tx, after, client)
		if err != nil {
			return err
		}
		return s.auditIdentityChaining(ctx, tx, identityChainingAuditEnable, after, after.RemoteSessionClientID.UUID, external, nil)
	case sameClient && !slices.Equal(before.RequestedScopes, after.RequestedScopes):
		external, err := identityChainingExternalClientID(ctx, tx, after, client)
		if err != nil {
			return err
		}
		return s.auditIdentityChaining(ctx, tx, identityChainingAuditUpdateScopes, after, after.RemoteSessionClientID.UUID, external, before.RequestedScopes)
	default:
		return nil
	}
}

// auditIdentityChainingDisable records before's client losing the binding at after's generation.
func (s *Service) auditIdentityChainingDisable(ctx context.Context, tx pgx.Tx, before, after repo.RemoteSessionEmaBinding, client repo.RemoteSessionClient) error {
	external, err := identityChainingExternalClientID(ctx, tx, before, client)
	if err != nil {
		return err
	}
	disabled := before
	disabled.Generation = after.Generation
	return s.auditIdentityChaining(ctx, tx, identityChainingAuditDisable, disabled, before.RemoteSessionClientID.UUID, external, nil)
}

// identityChainingExternalClientID returns the RFC 7591 client_id of b's client, reusing known when it is that client.
func identityChainingExternalClientID(ctx context.Context, tx pgx.Tx, b repo.RemoteSessionEmaBinding, known repo.RemoteSessionClient) (string, error) {
	if known.ID != uuid.Nil && known.ID == b.RemoteSessionClientID.UUID {
		return known.ClientID, nil
	}
	row, err := repo.New(tx).GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{ID: b.RemoteSessionClientID.UUID, ProjectID: b.ProjectID, OrganizationID: b.OrganizationID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", nil
	case err != nil:
		return "", oops.E(oops.CodeUnexpected, err, "audit identity chaining change")
	}
	return row.RemoteSessionClient.ClientID, nil
}
