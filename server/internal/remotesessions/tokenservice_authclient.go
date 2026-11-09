package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
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
	// Resource selects the upstream token grant for the server's audience.
	// The separate mcp:auth permission remains registration-scoped.
	Resource     string
	SkipUnusable bool

	// Attribution says where a refresh's connection row goes.
	Attribution RefreshAttribution
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
	attached, err := attachedCaller(ctx, req.Subject)
	if err != nil {
		return nil, err
	}
	client, err := m.reachableClient(ctx, req.ProjectID, req.OrganizationID, req.RegistrationID)
	switch {
	case attached && (errors.Is(err, ErrChosenClientUnbound) || (err == nil && client.CredentialOwner != string(CredentialOwnerSelf))):
		// An attached caller reaches a person's connection through its
		// attachment. A service account's token belongs to nobody, so no
		// attachment applies to it.
		return m.ResolveClientAccessTokens(ctx, req.Attribution, req.ProjectID, req.OrganizationID, req.UserSessionIssuerID, req.Subject, req.RegistrationID, req.SkipUnusable, req.Resource)
	case err != nil:
		return nil, err
	}
	return m.resolveClientTokens(ctx, req.Attribution, lookupAuthClient, req.ProjectID, req.OrganizationID, req.UserSessionIssuerID, req.Subject, []remotesessions_repo.ListRemoteSessionClientsForUserSessionIssuerRow{client}, req.SkipUnusable, req.Resource)
}

// AuthClientNeedsReconnect is RemoteSessionsNeedReconnect for the one
// registration an auth client resolved to, judged from stored state alone and
// without the user session issuer link. A registration that is gone or out of
// reach needs reconnecting.
func (m *ChallengeManager) AuthClientNeedsReconnect(ctx context.Context, req AuthClientTokenRequest) (bool, error) {
	if req.ProjectID == uuid.Nil || req.OrganizationID == "" || req.RegistrationID == uuid.Nil || req.Subject.IsZero() {
		return false, ErrInvalidAuthorizationRequest
	}
	attached, err := attachedCaller(ctx, req.Subject)
	if err != nil {
		return false, err
	}
	client, reachErr := m.reachableClient(ctx, req.ProjectID, req.OrganizationID, req.RegistrationID)
	// As ResolveAuthClientAccessTokens: a self-held credential belongs to
	// nobody, so no attachment applies to it.
	if attached && (errors.Is(reachErr, ErrChosenClientUnbound) || (reachErr == nil && client.CredentialOwner != string(CredentialOwnerSelf))) {
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
		return m.storedGrantsNeedReconnect(ctx, lookupSignIn, req.ProjectID, req.OrganizationID, req.UserSessionIssuerID, chosen, req.Subject, req.Resource)
	}
	switch {
	case errors.Is(reachErr, ErrChosenClientUnbound):
		return true, nil
	case reachErr != nil:
		return false, reachErr
	}
	return m.storedGrantsNeedReconnect(ctx, lookupAuthClient, req.ProjectID, req.OrganizationID, req.UserSessionIssuerID, []remotesessions_repo.ListRemoteSessionClientsForUserSessionIssuerRow{client}, req.Subject, req.Resource)
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
	return m.AuthClientRemoteSessionStatusesForResource(ctx, subject, projectID, organizationID, clientIDs, "")
}

// AuthClientRemoteSessionStatusesForResource reports the selected upstream audience,
// preferring its exact grant over a legacy unbound grant. It does not confer access.
func (m *ChallengeManager) AuthClientRemoteSessionStatusesForResource(ctx context.Context, subject urn.SessionSubject, projectID uuid.UUID, organizationID string, clientIDs []uuid.UUID, resource string) (map[uuid.UUID]RemoteSessionState, error) {
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
	return remoteSessionStates(states, resource), nil
}

// DisconnectAuthClientRemoteSession disconnects a registration authorized by a
// typed server offer, without requiring an issuer link. An unqualified request
// may change a singleton only; multiple live grants require an exact resource.
func (m *ChallengeManager) DisconnectAuthClientRemoteSession(ctx context.Context, subject urn.SessionSubject, projectID uuid.UUID, organizationID string, registrationID uuid.UUID) (int64, error) {
	return m.disconnectAuthClientRemoteSession(ctx, subject, projectID, organizationID, registrationID, "")
}

// DisconnectAuthClientRemoteSessionForResource disconnects only a matching bound
// audience. The caller must first authorize the exact typed server offer and
// credential owner; registration reach alone is not permission to use an offer.
func (m *ChallengeManager) DisconnectAuthClientRemoteSessionForResource(ctx context.Context, subject urn.SessionSubject, projectID uuid.UUID, organizationID string, registrationID uuid.UUID, resource string) (int64, error) {
	if resource == "" {
		return 0, focusedResourceRequired()
	}
	return m.disconnectAuthClientRemoteSession(ctx, subject, projectID, organizationID, registrationID, resource)
}

func (m *ChallengeManager) disconnectAuthClientRemoteSession(ctx context.Context, subject urn.SessionSubject, projectID uuid.UUID, organizationID string, registrationID uuid.UUID, resource string) (int64, error) {
	var revoked []RevokedCredentials
	err := m.mutateAuthClientRemoteSession(ctx, subject, projectID, organizationID, registrationID, resource, func(q *remotesessions_repo.Queries, session remotesessions_repo.RemoteSession) error {
		rows, err := q.SoftDeleteRemoteSessionBySubjectAndClientInTenant(ctx, remotesessions_repo.SoftDeleteRemoteSessionBySubjectAndClientInTenantParams{
			SubjectUrn: subject, ProjectID: projectID, OrganizationID: organizationID, RemoteSessionClientID: registrationID,
			ID: session.ID, ExpectedUpdatedAt: session.UpdatedAt, Resource: resource,
		})
		if err != nil {
			return fmt.Errorf("disconnect remote session in tenant: %w", err)
		}
		if len(rows) == 0 {
			return remoteSessionReferenceChanged()
		}
		for _, row := range rows {
			revoked = append(revoked, RevokedCredentials{RemoteSessionClientID: row.RemoteSessionClientID, AccessTokenEncrypted: row.AccessTokenEncrypted, RefreshTokenEncrypted: row.RefreshTokenEncrypted})
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	// Never revoke an upstream credential before its tombstone commits.
	for _, credentials := range revoked {
		m.revoker.RevokeDetached(ctx, credentials)
	}
	return int64(len(revoked)), nil
}

// SetAuthClientRemoteSessionAutoRefresh changes only a singleton grant for a
// typed registration. It retains the preference's independent refresh clock.
func (m *ChallengeManager) SetAuthClientRemoteSessionAutoRefresh(ctx context.Context, subject urn.SessionSubject, projectID uuid.UUID, organizationID string, registrationID uuid.UUID, enabled bool) (int64, error) {
	return m.setAuthClientRemoteSessionAutoRefresh(ctx, subject, projectID, organizationID, registrationID, "", enabled)
}

// SetAuthClientRemoteSessionAutoRefreshForResource changes a matching bound
// audience, never a legacy NULL grant inferred from another server's resource.
// The caller must first authorize the exact typed offer and credential owner.
func (m *ChallengeManager) SetAuthClientRemoteSessionAutoRefreshForResource(ctx context.Context, subject urn.SessionSubject, projectID uuid.UUID, organizationID string, registrationID uuid.UUID, resource string, enabled bool) (int64, error) {
	if resource == "" {
		return 0, focusedResourceRequired()
	}
	return m.setAuthClientRemoteSessionAutoRefresh(ctx, subject, projectID, organizationID, registrationID, resource, enabled)
}

func (m *ChallengeManager) setAuthClientRemoteSessionAutoRefresh(ctx context.Context, subject urn.SessionSubject, projectID uuid.UUID, organizationID string, registrationID uuid.UUID, resource string, enabled bool) (int64, error) {
	var changed int64
	err := m.mutateAuthClientRemoteSession(ctx, subject, projectID, organizationID, registrationID, resource, func(q *remotesessions_repo.Queries, session remotesessions_repo.RemoteSession) error {
		var err error
		changed, err = q.SetRemoteSessionAutoRefreshInTenant(ctx, remotesessions_repo.SetRemoteSessionAutoRefreshInTenantParams{
			SubjectUrn: subject, ProjectID: projectID, OrganizationID: organizationID, RemoteSessionClientID: registrationID,
			ID: session.ID, ExpectedUpdatedAt: session.UpdatedAt, Resource: resource, AutoRefresh: enabled,
		})
		if err != nil {
			return fmt.Errorf("set remote session auto refresh in tenant: %w", err)
		}
		if changed == 0 {
			return remoteSessionReferenceChanged()
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return changed, nil
}

// RefreshAuthClientRemoteSession refreshes a singleton authorized by a typed
// offer, without an issuer link. Registration-only ambiguity fails closed.
func (m *ChallengeManager) RefreshAuthClientRemoteSession(ctx context.Context, attribution RefreshAttribution, subject urn.SessionSubject, projectID uuid.UUID, organizationID string, registrationID uuid.UUID) (RefreshResult, error) {
	return m.refreshAuthClientRemoteSession(ctx, attribution, subject, projectID, organizationID, registrationID, "")
}

// RefreshAuthClientRemoteSessionForResource refreshes the selected bound row
// through existing rotation, singleflight and CAS. It confers no offer access.
func (m *ChallengeManager) RefreshAuthClientRemoteSessionForResource(ctx context.Context, attribution RefreshAttribution, subject urn.SessionSubject, projectID uuid.UUID, organizationID string, registrationID uuid.UUID, resource string) (RefreshResult, error) {
	if resource == "" {
		return RefreshResult{}, focusedResourceRequired()
	}
	return m.refreshAuthClientRemoteSession(ctx, attribution, subject, projectID, organizationID, registrationID, resource)
}

func (m *ChallengeManager) refreshAuthClientRemoteSession(ctx context.Context, attribution RefreshAttribution, subject urn.SessionSubject, projectID uuid.UUID, organizationID string, registrationID uuid.UUID, resource string) (RefreshResult, error) {
	var zero RefreshResult
	q := remotesessions_repo.New(m.db)
	session, err := authClientMutationSource(ctx, q, subject, projectID, organizationID, registrationID, resource)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, ErrRemoteSessionNotRefreshable
	}
	if err != nil {
		return zero, err
	}
	if !session.RefreshTokenEncrypted.Valid || session.RefreshTokenEncrypted.String == "" {
		return zero, ErrRemoteSessionNotRefreshable
	}
	result, err := m.refresher.RefreshNow(ctx, session, resource, remotesessionmetrics.RefreshTriggerManual, attribution)
	if err != nil {
		return zero, err
	}
	if result.Session.ID == uuid.Nil {
		return result, nil
	}
	// A source snapshot is not renewed tenant authority. Recheck admission after
	// the network call, without returning an out-of-date or sibling result.
	current, err := authClientMutationSource(ctx, q, subject, projectID, organizationID, registrationID, resource)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, ErrRemoteSessionNotRefreshable
	}
	if err != nil {
		return zero, err
	}
	if current.ID != session.ID || current.GrantGeneration != session.GrantGeneration || current.ID != result.Session.ID || !current.UpdatedAt.Time.Equal(result.Session.UpdatedAt.Time) {
		return zero, remoteSessionReferenceChanged()
	}
	return result, nil
}

// mutateAuthClientRemoteSession holds the same client-row lock as consent while
// choosing and mutating a source. A new sibling cannot race a singleton action.
// The callback only writes locally; upstream revocation happens after commit.
func (m *ChallengeManager) mutateAuthClientRemoteSession(ctx context.Context, subject urn.SessionSubject, projectID uuid.UUID, organizationID string, registrationID uuid.UUID, resource string, mutate func(*remotesessions_repo.Queries, remotesessions_repo.RemoteSession) error) error {
	if subject.IsZero() || projectID == uuid.Nil || organizationID == "" || registrationID == uuid.Nil {
		return ErrInvalidAuthorizationRequest
	}
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin typed remote session mutation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := remotesessions_repo.New(tx)
	if _, err := q.LockRemoteSessionClientForSessionWrite(ctx, registrationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("lock typed remote session registration: %w", err)
	}
	session, err := authClientMutationSource(ctx, q, subject, projectID, organizationID, registrationID, resource)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := mutate(q, session); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit typed remote session mutation: %w", err)
	}
	return nil
}

// authClientMutationSource uses the existing typed tenant/consenting-org
// selector, not an issuer-link check or a second offer authorization resolver.
func authClientMutationSource(ctx context.Context, q *remotesessions_repo.Queries, subject urn.SessionSubject, projectID uuid.UUID, organizationID string, registrationID uuid.UUID, resource string) (remotesessions_repo.RemoteSession, error) {
	var zero remotesessions_repo.RemoteSession
	if subject.IsZero() || projectID == uuid.Nil || organizationID == "" || registrationID == uuid.Nil {
		return zero, ErrInvalidAuthorizationRequest
	}
	admitted, err := q.CheckAuthClientRemoteSessionSubjectAdmission(ctx, remotesessions_repo.CheckAuthClientRemoteSessionSubjectAdmissionParams{ProjectID: projectID, OrganizationID: organizationID, SubjectUrn: subject.String()})
	if err != nil {
		return zero, fmt.Errorf("check typed remote session live admission: %w", err)
	}
	if !admitted {
		return zero, pgx.ErrNoRows
	}
	if resource == "" {
		rows, err := q.ListRemoteSessionStatusesInTenant(ctx, remotesessions_repo.ListRemoteSessionStatusesInTenantParams{SubjectUrn: subject, ProjectID: projectID, OrganizationID: organizationID, RemoteSessionClientIds: []uuid.UUID{registrationID}})
		if err != nil {
			return zero, fmt.Errorf("check typed remote session ambiguity: %w", err)
		}
		if len(rows) > 1 {
			return zero, focusedResourceRequired()
		}
	}
	session, err := q.GetActiveRemoteSessionInTenant(ctx, remotesessions_repo.GetActiveRemoteSessionInTenantParams{SubjectUrn: subject, ProjectID: projectID, OrganizationID: organizationID, RemoteSessionClientID: registrationID, Resource: resource})
	if err != nil {
		return zero, fmt.Errorf("get typed remote session mutation source: %w", err)
	}
	if resource != "" && (!session.Resource.Valid || session.Resource.String == "" || strings.TrimRight(session.Resource.String, "/") != strings.TrimRight(resource, "/")) {
		return zero, remoteSessionReferenceChanged()
	}
	return session, nil
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
	// A principal-backed key asking for its authorizer's own connection reads
	// it directly; any other subject keeps the key's attachment.
	if _, ok := principalAPIKeyAuthorizer(ctx, subject); ok {
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
func tenantUserRemoteSession(ctx context.Context, q *remotesessions_repo.Queries, lookup tokenLookup, subject urn.SessionSubject, clientID, projectID uuid.UUID, organizationID string, userSessionIssuerID uuid.UUID, resource string) (remotesessions_repo.RemoteSession, error) {
	if lookup == lookupAuthClient {
		sess, err := q.GetTenantUserRemoteSessionForClient(ctx, remotesessions_repo.GetTenantUserRemoteSessionForClientParams{
			Resource:              resource,
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
		Resource:              resource,
		SubjectUrn:            subject,
		RemoteSessionClientID: clientID,
		ProjectID:             projectID,
		OrganizationID:        organizationID,
		UserSessionIssuerID:   userSessionIssuerID,
	})
	if errors.Is(err, pgx.ErrNoRows) && resource != "" {
		sess, err = q.GetTenantUserRemoteSession(ctx, remotesessions_repo.GetTenantUserRemoteSessionParams{SubjectUrn: subject, RemoteSessionClientID: clientID, ProjectID: projectID, OrganizationID: organizationID, UserSessionIssuerID: userSessionIssuerID, Resource: ""})
	}
	if err != nil {
		return sess, fmt.Errorf("get tenant user remote session: %w", err)
	}
	return sess, nil
}

// activeUserRemoteSession selects a plain caller's own session for clientID:
// by subject and client alone, which the sign-in link scopes, or, for the auth
// client resolver, only when the consent came through the server's
// organization.
func activeUserRemoteSession(ctx context.Context, q *remotesessions_repo.Queries, lookup tokenLookup, subject urn.SessionSubject, clientID, projectID uuid.UUID, organizationID string, resource string) (remotesessions_repo.RemoteSession, error) {
	if lookup == lookupAuthClient {
		sess, err := q.GetActiveRemoteSessionInTenant(ctx, remotesessions_repo.GetActiveRemoteSessionInTenantParams{
			Resource:              resource,
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
		Resource:              resource,
		SubjectUrn:            subject,
		RemoteSessionClientID: clientID,
	})
	if errors.Is(err, pgx.ErrNoRows) && resource != "" {
		sess, err = q.GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{SubjectUrn: subject, RemoteSessionClientID: clientID, Resource: ""})
	}
	if err != nil {
		return sess, fmt.Errorf("get active remote_session: %w", err)
	}
	return sess, nil
}
