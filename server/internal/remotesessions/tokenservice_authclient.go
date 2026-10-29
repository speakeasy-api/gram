package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// tokenLookup selects how a user's stored credential is tied to the server's
// sign-in.
type tokenLookup int

const (
	// lookupSignIn requires the client to be linked to the server's user
	// session issuer.
	lookupSignIn tokenLookup = iota
	// lookupAuthClient is for the auth client resolver, which has already
	// checked the client against the server: no link is required, but the
	// consent's sign-in must be in the server's project or organization.
	lookupAuthClient
)

// AuthClientTokenRequest names one OAuth registration a typed auth client
// resolved to, for one caller.
type AuthClientTokenRequest struct {
	ProjectID      uuid.UUID
	OrganizationID string
	// UserSessionIssuerID is consulted only for agent attachments, which are
	// still keyed by the sign-in.
	UserSessionIssuerID uuid.UUID
	Subject             urn.SessionSubject
	RegistrationID      uuid.UUID
	SkipUnusable        bool
}

// ResolveAuthClientAccessTokens resolves the caller's credential for the
// registration an auth client resolved to, with the per-client behavior of
// ResolveClientAccessTokens but without requiring the registration to be
// linked to the endpoint's user session issuer. The registration and its
// issuer must be live and within the project's or organization's reach;
// otherwise it returns ErrChosenClientUnbound. A consent serves only when it
// came through a sign-in of the server's organization: a project registration
// stays in its project, and an organization or global one in the organization.
//
// Only the auth client resolver calls it, after checking the client against
// the server. Agent attachments keep the sign-in-keyed lookup.
func (m *ChallengeManager) ResolveAuthClientAccessTokens(ctx context.Context, req AuthClientTokenRequest) (map[uuid.UUID]UpstreamToken, error) {
	if req.ProjectID == uuid.Nil || req.OrganizationID == "" || req.RegistrationID == uuid.Nil || req.Subject.IsZero() {
		return nil, ErrInvalidAuthorizationRequest
	}
	if attached, err := attachedCaller(ctx, req.Subject); err != nil {
		return nil, err
	} else if attached {
		return m.ResolveClientAccessTokens(ctx, req.ProjectID, req.OrganizationID, req.UserSessionIssuerID, req.Subject, req.RegistrationID, req.SkipUnusable)
	}
	client, err := m.reachableClient(ctx, req.ProjectID, req.OrganizationID, req.RegistrationID)
	if err != nil {
		return nil, err
	}
	return m.resolveClientTokens(ctx, lookupAuthClient, req.ProjectID, req.OrganizationID, req.UserSessionIssuerID, req.Subject, []remotesessions_repo.ListRemoteSessionClientsForUserSessionIssuerRow{client}, req.SkipUnusable)
}

// AuthClientNeedsReconnect is RemoteSessionsNeedReconnect for the one
// registration an auth client resolved to, judged from stored state alone and
// without the user session issuer link. A registration that is gone or out of
// reach needs reconnecting.
func (m *ChallengeManager) AuthClientNeedsReconnect(ctx context.Context, req AuthClientTokenRequest) (bool, error) {
	if req.ProjectID == uuid.Nil || req.OrganizationID == "" || req.RegistrationID == uuid.Nil || req.Subject.IsZero() {
		return false, ErrInvalidAuthorizationRequest
	}
	if attached, err := attachedCaller(ctx, req.Subject); err != nil {
		return false, err
	} else if attached {
		clients, err := m.listRemoteSessionClientRowsForUserSessionIssuer(ctx, req.ProjectID, req.OrganizationID, req.UserSessionIssuerID, false)
		if err != nil {
			return false, fmt.Errorf("list remote_session_clients: %w", err)
		}
		chosen := slices.DeleteFunc(clients, func(c remotesessions_repo.ListRemoteSessionClientsForUserSessionIssuerRow) bool {
			return c.ClientID != req.RegistrationID
		})
		if len(chosen) == 0 {
			return true, nil
		}
		return m.storedGrantsNeedReconnect(ctx, lookupSignIn, req.ProjectID, req.OrganizationID, req.UserSessionIssuerID, chosen, req.Subject)
	}
	client, err := m.reachableClient(ctx, req.ProjectID, req.OrganizationID, req.RegistrationID)
	switch {
	case errors.Is(err, ErrChosenClientUnbound):
		return true, nil
	case err != nil:
		return false, err
	}
	return m.storedGrantsNeedReconnect(ctx, lookupAuthClient, req.ProjectID, req.OrganizationID, req.UserSessionIssuerID, []remotesessions_repo.ListRemoteSessionClientsForUserSessionIssuerRow{client}, req.Subject)
}

// ListAuthClientRegistrations returns the registrations among ids that the
// auth client resolver can serve in the project: live, with a live issuer, and
// within the project's or organization's reach, whether or not a user session
// issuer links them. Registrations out of reach are left out.
func (m *ChallengeManager) ListAuthClientRegistrations(ctx context.Context, projectID uuid.UUID, organizationID string, ids []uuid.UUID) ([]Client, error) {
	if len(ids) == 0 {
		return []Client{}, nil
	}
	rows, err := remotesessions_repo.New(m.db).ListReachableRemoteSessionClients(ctx, remotesessions_repo.ListReachableRemoteSessionClientsParams{
		ProjectID:              projectID,
		OrganizationID:         organizationID,
		RemoteSessionClientIds: ids,
	})
	if err != nil {
		return nil, fmt.Errorf("list reachable remote_session_clients: %w", err)
	}
	out := make([]Client, 0, len(rows))
	for _, row := range rows {
		out = append(out, clientFromRow(remotesessions_repo.ListRemoteSessionClientsForUserSessionIssuerRow(row)))
	}
	return out, nil
}

// AuthClientRemoteSessionStatuses is RemoteSessionStatuses for registrations
// the auth client resolver serves: it reports a user's session for each of
// clientIDs only where the resolver would serve it, whichever sign-in it was
// consented through.
func (m *ChallengeManager) AuthClientRemoteSessionStatuses(ctx context.Context, subject urn.SessionSubject, projectID uuid.UUID, organizationID string, clientIDs []uuid.UUID) (map[uuid.UUID]RemoteSessionState, error) {
	if subject.IsZero() || len(clientIDs) == 0 {
		return map[uuid.UUID]RemoteSessionState{}, nil
	}
	rows, err := remotesessions_repo.New(m.db).ListRemoteSessionStatusesInTenant(ctx, remotesessions_repo.ListRemoteSessionStatusesInTenantParams{
		ProjectID:              projectID,
		OrganizationID:         organizationID,
		SubjectUrn:             subject,
		RemoteSessionClientIds: clientIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("list remote session statuses in tenant: %w", err)
	}
	states := make([]remotesessions_repo.ListRemoteSessionStatusesForSubjectRow, 0, len(rows))
	for _, row := range rows {
		states = append(states, remotesessions_repo.ListRemoteSessionStatusesForSubjectRow(row))
	}
	return remoteSessionStates(states), nil
}

// attachedCaller reports whether the caller resolves through an agent
// attachment, deciding the credential source in resolveCallerUpstreamToken's
// order: workloads and principal credentials use the user's own session.
func attachedCaller(ctx context.Context, subject urn.SessionSubject) (bool, error) {
	if actor, ok := contextvalues.AuthenticatedActor(ctx); ok && actor.Type == urn.PrincipalTypeWorkload {
		return false, nil
	}
	if subject.Kind == urn.SessionSubjectKindWorkload {
		return false, ErrInvalidAuthorizationRequest
	}
	if _, ok := principalcredential.FromContext(ctx); ok {
		return false, nil
	}
	_, attached, err := remoteSessionCallerPrincipal(ctx, subject)
	return attached, err
}

// AttachedConnection is the stored connection an agent attachment serves for
// one registration: the exact remote session row and the user who owns it.
type AttachedConnection struct {
	RemoteSessionID uuid.UUID
	OwnerUserID     string
}

// AuthClientAttachedConnection reports the connection an attached caller's
// binding serves for req.RegistrationID, through the same exact, tenant- and
// sign-in-scoped lookup token resolution uses: a revoked, deleted, replaced or
// out-of-reach binding serves nothing. It is metadata only and never a grant:
// it returns no token, and the caller still decides permission for the owner
// it names. attached is false for a caller that is not served by attachments
// (a user, a principal credential, a workload) and for an attached caller with
// no usable binding; the caller's own subject still governs those.
func (m *ChallengeManager) AuthClientAttachedConnection(ctx context.Context, req AuthClientTokenRequest) (conn AttachedConnection, attached bool, err error) {
	var zero AttachedConnection
	if req.Subject.IsZero() {
		return zero, false, ErrInvalidAuthorizationRequest
	}
	if attached, err := attachedCaller(ctx, req.Subject); err != nil || !attached {
		return zero, false, err
	}
	principalID, _, err := remoteSessionCallerPrincipal(ctx, req.Subject)
	if err != nil {
		return zero, false, err
	}
	if req.ProjectID == uuid.Nil || req.OrganizationID == "" || req.UserSessionIssuerID == uuid.Nil || req.RegistrationID == uuid.Nil {
		return zero, false, ErrInvalidAuthorizationRequest
	}
	source, err := remotesessions_repo.New(m.db).GetPrincipalRemoteSessionBinding(ctx, remotesessions_repo.GetPrincipalRemoteSessionBindingParams{
		ProjectID:             req.ProjectID,
		OrganizationID:        req.OrganizationID,
		PrincipalID:           principalID,
		UserSessionIssuerID:   req.UserSessionIssuerID,
		RemoteSessionClientID: req.RegistrationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return zero, false, nil
	case err != nil:
		return zero, false, fmt.Errorf("get principal remote session binding: %w", err)
	}
	if source.SubjectUrn.Kind != urn.SessionSubjectKindUser || source.SubjectUrn.ID == "" {
		return zero, false, nil
	}
	return AttachedConnection{RemoteSessionID: source.ID, OwnerUserID: source.SubjectUrn.ID}, true, nil
}

func (m *ChallengeManager) reachableClient(ctx context.Context, projectID uuid.UUID, organizationID string, registrationID uuid.UUID) (remotesessions_repo.ListRemoteSessionClientsForUserSessionIssuerRow, error) {
	row, err := remotesessions_repo.New(m.db).GetReachableRemoteSessionClient(ctx, remotesessions_repo.GetReachableRemoteSessionClientParams{
		ProjectID:             projectID,
		OrganizationID:        organizationID,
		RemoteSessionClientID: registrationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return remotesessions_repo.ListRemoteSessionClientsForUserSessionIssuerRow{}, ErrChosenClientUnbound
	case err != nil:
		return remotesessions_repo.ListRemoteSessionClientsForUserSessionIssuerRow{}, fmt.Errorf("get reachable remote_session_client: %w", err)
	}
	return remotesessions_repo.ListRemoteSessionClientsForUserSessionIssuerRow(row), nil
}

// tenantUserRemoteSession selects a user's own session for clientID in the
// tenant: through the user session issuer link, or, for the auth client
// resolver, through any sign-in of the server's project or organization.
func tenantUserRemoteSession(ctx context.Context, q *remotesessions_repo.Queries, lookup tokenLookup, subject urn.SessionSubject, clientID, projectID uuid.UUID, organizationID string, userSessionIssuerID uuid.UUID) (remotesessions_repo.RemoteSession, error) {
	if lookup == lookupAuthClient {
		sess, err := q.GetTenantUserRemoteSessionForClient(ctx, remotesessions_repo.GetTenantUserRemoteSessionForClientParams{
			ProjectID:             projectID,
			OrganizationID:        organizationID,
			SubjectUrn:            subject,
			RemoteSessionClientID: clientID,
		})
		if err != nil {
			return sess, fmt.Errorf("get tenant user remote session for client: %w", err)
		}
		return sess, nil
	}
	sess, err := q.GetTenantUserRemoteSession(ctx, remotesessions_repo.GetTenantUserRemoteSessionParams{
		SubjectUrn:            subject,
		RemoteSessionClientID: clientID,
		ProjectID:             projectID,
		OrganizationID:        organizationID,
		UserSessionIssuerID:   userSessionIssuerID,
	})
	if err != nil {
		return sess, fmt.Errorf("get tenant user remote session: %w", err)
	}
	return sess, nil
}

// activeUserRemoteSession selects a plain caller's own session for clientID:
// by subject and client alone, which the sign-in link scopes, or, for the auth
// client resolver, only when the consent came through the server's
// organization.
func activeUserRemoteSession(ctx context.Context, q *remotesessions_repo.Queries, lookup tokenLookup, subject urn.SessionSubject, clientID, projectID uuid.UUID, organizationID string) (remotesessions_repo.RemoteSession, error) {
	if lookup == lookupAuthClient {
		sess, err := q.GetActiveRemoteSessionInTenant(ctx, remotesessions_repo.GetActiveRemoteSessionInTenantParams{
			ProjectID:             projectID,
			OrganizationID:        organizationID,
			SubjectUrn:            subject,
			RemoteSessionClientID: clientID,
		})
		if err != nil {
			return sess, fmt.Errorf("get active remote_session in tenant: %w", err)
		}
		return sess, nil
	}
	sess, err := q.GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{
		SubjectUrn:            subject,
		RemoteSessionClientID: clientID,
	})
	if err != nil {
		return sess, fmt.Errorf("get active remote_session: %w", err)
	}
	return sess, nil
}
