// Typed auth client resolution for issuer-gated direct proxied servers.
//
// A credential's choice for a server names a typed auth client
// (mcp_auth_clients). When the auth-client-resolver flag is on for the
// organization, that client decides how the request authenticates upstream:
// a personal connection or a service account through its OAuth registration,
// a static credential or a forwarded header through the client's own headers,
// or no upstream authentication at all. A client that is gone, disabled,
// detached from the server or outside its tenant is a gap the caller fixes by
// reconnecting, never an implied no-auth and never another client.
//
// Some choices are handed back to the upstream client choice, which keeps
// serving them as before: EMA clients, choices not yet mapped onto auth
// clients, header copies that no longer match the server's headers, a
// registration that may still be served by identity chaining, and
// headers-only clients on tunneled servers. With the flag off, the shadow
// flag runs the resolver beside the choice and reports disagreement, without
// acquiring credentials.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Auth client types, as stored in mcp_auth_clients.client_type. They are the
// values mcp:auth checks carry.
const (
	authClientTypeRemoteSession    = authz.AuthClientTypeRemoteSession
	authClientTypeServiceAccount   = authz.AuthClientTypeServiceAccount
	authClientTypeStaticCredential = authz.AuthClientTypeStaticCredential
	authClientTypePassThrough      = authz.AuthClientTypePassThrough
	authClientTypeEMA              = authz.AuthClientTypeEMA
)

// authClientOutcome is how a credential's typed auth client resolves for a
// server.
type authClientOutcome string

const (
	// authClientResolved: the client supplies the upstream credential.
	authClientResolved authClientOutcome = "resolved"
	// authClientNoAuth: the server needs no upstream authentication.
	authClientNoAuth authClientOutcome = "no_auth"
	// authClientGap: the choice cannot be used; reconnecting records a new one.
	authClientGap authClientOutcome = "reconnect"
	// authClientNoAccess: the caller may not use the chosen client.
	// Reconnecting would choose it again, so this is a refusal with a link to
	// request access, never a reconnect.
	authClientNoAccess authClientOutcome = "no_access"
	// authClientLegacy: the upstream client choice serves the request.
	authClientLegacy authClientOutcome = "legacy"
)

// authClientParityError is the parity outcome of a resolver that failed.
const authClientParityError = "error"

// Reasons for an outcome. They are a closed set: the parity metric carries
// them as a label.
const (
	authClientReasonMissingChoice   = "missing_choice"
	authClientReasonUnmapped        = "unmapped_choice"
	authClientReasonClientGone      = "client_gone"
	authClientReasonClientDeleted   = "client_deleted"
	authClientReasonClientDisabled  = "client_disabled"
	authClientReasonUnassociated    = "client_unassociated"
	authClientReasonRegistration    = "registration_gone"
	authClientReasonDetached        = "registration_detached"
	authClientReasonUnknownType     = "unknown_type"
	authClientReasonNoAuthForbidden = "no_auth_forbidden"
	authClientReasonNoAuthStale     = "no_auth_server_has_clients"
	authClientReasonNotPermitted    = "not_permitted"
	authClientReasonSetup           = "setup"
	authClientReasonHeadersStale    = "client_headers_stale"
	authClientReasonLegacyMayChain  = "legacy_may_chain"
	authClientReasonTunneledHeaders = "tunneled_headers_only"
	authClientReasonEMA             = "ema"
	authClientReasonNone            = "none"
)

// authClientResolution is one credential's typed auth client decision for one
// server.
type authClientResolution struct {
	outcome    authClientOutcome
	reason     string
	clientID   uuid.UUID
	clientType string

	// serverName names the server in a request-access link.
	serverName string
	// tunneled is a server reached through a tunnel, which is sent no client
	// headers.
	tunneled bool

	// choice routes OAuth-backed clients: the registration and its issuer.
	choice upstreamChoice

	// headers are the client's own headers, loaded only when it serves.
	headers []proxy.ConfiguredHeader

	// noAuthAllowed is a server explicitly marked as needing no upstream
	// authentication.
	noAuthAllowed bool
}

// headersOnly reports a resolved client whose material is its headers alone,
// with no bearer to acquire.
func (r *authClientResolution) headersOnly() bool {
	return r.outcome == authClientResolved && isHeadersOnlyType(r.clientType)
}

func isHeadersOnlyType(clientType string) bool {
	return clientType == authClientTypeStaticCredential || clientType == authClientTypePassThrough
}

// authClientPermitRequest is one use of a resolved client to check.
type authClientPermitRequest struct {
	OrganizationID string
	ServerID       uuid.UUID
	ClientID       uuid.UUID
	ClientType     string
	// CredentialKind and CredentialID identify the credential whose choice
	// named the client.
	CredentialKind string
	CredentialID   uuid.UUID
	// Subject is the user the credential represents.
	Subject urn.SessionSubject
}

// authClientPermitter decides whether the caller may use a resolved client.
type authClientPermitter interface {
	Permitted(ctx context.Context, req authClientPermitRequest) (bool, error)
}

// connectPermitter keeps today's rule until per-client grants exist
// (AIM-589): every client associated with the server is usable by its
// callers. Server access itself (mcp:connect) is checked later, when the
// backend is prepared, and still applies.
type connectPermitter struct{}

func (connectPermitter) Permitted(context.Context, authClientPermitRequest) (bool, error) {
	return true, nil
}

// gapResolution is a choice that cannot be used, for reason.
func gapResolution(reason string) authClientResolution {
	none := upstreamChoice{clientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, remoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}
	return authClientResolution{outcome: authClientGap, reason: reason, clientID: uuid.Nil, clientType: "", serverName: "", tunneled: false, choice: none, headers: nil, noAuthAllowed: false}
}

// decideAuthClient resolves a recorded choice without side effects.
func decideAuthClient(row upstreamclientsrepo.GetAuthClientSelectionRow) authClientResolution {
	res := gapResolution("")
	res.serverName = row.ServerName.String
	res.tunneled = row.Tunneled
	res.noAuthAllowed = row.RequiresUpstreamAuth.Valid && !row.RequiresUpstreamAuth.Bool
	if !row.AuthClientID.Valid {
		if !row.NoAuth {
			// An OAuth choice recorded before choices name auth clients.
			res.outcome = authClientLegacy
			res.reason = authClientReasonUnmapped
			return res
		}
		switch {
		case row.ServerHasHeaderClients && !row.ServerHasOauthClients:
			// A no-auth choice from before the backfill gave the server its
			// header clients; the old path still serves it with the server's
			// headers.
			res.outcome = authClientLegacy
			res.reason = authClientReasonUnmapped
		case row.RequiresUpstreamAuth.Valid && row.RequiresUpstreamAuth.Bool:
			res.reason = authClientReasonNoAuthForbidden
		case !row.RequiresUpstreamAuth.Valid && row.ServerHasClients:
			res.reason = authClientReasonNoAuthStale
		case row.CredentialHeadersUncovered:
			res.reason = authClientReasonSetup
		default:
			res.outcome = authClientNoAuth
		}
		return res
	}
	res.clientID = row.AuthClientID.UUID
	switch {
	case !row.ClientID.Valid:
		res.reason = authClientReasonClientGone
		return res
	case row.ClientDeleted.Bool:
		res.reason = authClientReasonClientDeleted
		return res
	case !row.ClientEnabled.Bool:
		res.reason = authClientReasonClientDisabled
		return res
	case !row.Associated:
		res.reason = authClientReasonUnassociated
		return res
	}
	res.clientType = row.ClientType.String
	switch res.clientType {
	case authClientTypeRemoteSession, authClientTypeServiceAccount:
		switch {
		case !row.RegistrationID.Valid || !row.RegistrationLive:
			res.reason = authClientReasonRegistration
		case !row.RegistrationAttached:
			res.reason = authClientReasonDetached
		case row.HeadersStale && !row.Tunneled:
			// The client's copies of the server's auxiliary credential
			// headers no longer match it; the old path sends the current ones.
			// A tunneled server is sent no client headers, so nothing stale
			// reaches it.
			res.outcome = authClientLegacy
			res.reason = authClientReasonHeadersStale
		case res.clientType == authClientTypeRemoteSession && row.RegistrationHasEma:
			// The upstream client choice may chain an identity for this
			// registration when its token is unusable; the resolver does not.
			res.outcome = authClientLegacy
			res.reason = authClientReasonLegacyMayChain
		case row.CredentialHeadersUncovered, row.RequiresDeniedSource:
			res.reason = authClientReasonSetup
		default:
			res.outcome = authClientResolved
			res.choice = upstreamChoice{clientID: row.RegistrationID, remoteSessionIssuerID: row.RegistrationIssuerID}
		}
	case authClientTypeStaticCredential, authClientTypePassThrough:
		switch {
		case row.Tunneled:
			// Tunneled servers are sent no configured headers yet; serving a
			// client's headers through a tunnel is not designed.
			res.outcome = authClientLegacy
			res.reason = authClientReasonTunneledHeaders
		case row.HeadersStale:
			res.outcome = authClientLegacy
			res.reason = authClientReasonHeadersStale
		case row.CredentialHeadersUncovered, row.RequiresDeniedSource:
			res.reason = authClientReasonSetup
		default:
			res.outcome = authClientResolved
		}
	case authClientTypeEMA:
		res.outcome = authClientLegacy
		res.reason = authClientReasonEMA
	default:
		res.reason = authClientReasonUnknownType
	}
	return res
}

// resolveAuthClient reads the credential's choice for serverID and decides it,
// without checking permission, acquiring any credential or loading client
// headers.
func (u *upstreamSelector) resolveAuthClient(ctx context.Context, serverID uuid.UUID) (authClientResolution, error) {
	row, err := upstreamclientsrepo.New(u.service.db).GetAuthClientSelection(ctx, upstreamclientsrepo.GetAuthClientSelectionParams{
		CredentialKind: upstreamclients.CredentialKindUserSession,
		CredentialID:   u.credentialID,
		McpServerID:    serverID,
		OrganizationID: u.organizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return gapResolution(authClientReasonMissingChoice), nil
	case err != nil:
		return gapResolution(""), fmt.Errorf("load auth client choice: %w", err)
	}
	return decideAuthClient(row), nil
}

// resolveAuthClientForServing resolves the credential's client for serverID
// as a request will use it: checked against the permitter, with the client's
// headers loaded. The caller turns the outcome into a response, so direct
// endpoints and gateway members can answer it their own way.
func (s *Service) resolveAuthClientForServing(ctx context.Context, selector *upstreamSelector, serverID, projectID uuid.UUID, subject urn.SessionSubject) (authClientResolution, error) {
	res, err := selector.resolveAuthClient(ctx, serverID)
	if err != nil {
		return res, err
	}
	if res.outcome == authClientResolved {
		permitted, err := s.authClientPermit().Permitted(ctx, authClientPermitRequest{
			OrganizationID: selector.organizationID,
			ServerID:       serverID,
			ClientID:       res.clientID,
			ClientType:     res.clientType,
			CredentialKind: upstreamclients.CredentialKindUserSession,
			CredentialID:   selector.credentialID,
			Subject:        subject,
		})
		if err != nil {
			return res, fmt.Errorf("check auth client permission: %w", err)
		}
		s.shadowAuthClientUse(ctx, selector.organizationID, permitted, projectID, res.clientID, res.clientType, subject)
		if !permitted {
			res.outcome = authClientNoAccess
			res.reason = authClientReasonNotPermitted
		}
	}
	if res.outcome == authClientResolved && !res.tunneled {
		headers, err := s.loadAuthClientHeaders(ctx, selector.organizationID, res.clientID)
		if err != nil {
			return res, err
		}
		var blocked bool
		res.headers, blocked = withoutDeniedSources(headers)
		// A headers-only client with nothing left to send would call the
		// upstream anonymously, which only a server needing no auth allows.
		if blocked || (res.headersOnly() && len(res.headers) == 0 && !res.noAuthAllowed) {
			res.outcome = authClientGap
			res.reason = authClientReasonSetup
			res.headers = nil
		}
	}
	annotateAuthClient(ctx, s.logger, res)
	return res, nil
}

func (s *Service) authClientPermit() authClientPermitter {
	if s.authClientPermitter != nil {
		return s.authClientPermitter
	}
	return connectPermitter{}
}

// withoutDeniedSources drops client headers that read a denied inbound header,
// such as Authorization, which on an issuer-gated endpoint carries the
// caller's Speakeasy session token. It reports whether a dropped header was
// required, which the client cannot serve without.
func withoutDeniedSources(headers []proxy.ConfiguredHeader) ([]proxy.ConfiguredHeader, bool) {
	kept := make([]proxy.ConfiguredHeader, 0, len(headers))
	blocked := false
	for _, h := range headers {
		if h.ValueFromRequestHeader != "" && proxy.IsDeniedPassThroughSource(h.ValueFromRequestHeader) {
			blocked = blocked || h.IsRequired
			continue
		}
		kept = append(kept, h)
	}
	return kept, blocked
}

// loadAuthClientHeaders reads and reveals a resolved client's own headers.
func (s *Service) loadAuthClientHeaders(ctx context.Context, organizationID string, clientID uuid.UUID) ([]proxy.ConfiguredHeader, error) {
	rows, err := upstreamclientsrepo.New(s.db).ListAuthClientHeaders(ctx, upstreamclientsrepo.ListAuthClientHeadersParams{
		AuthClientID:   clientID,
		OrganizationID: organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("list auth client headers: %w", err)
	}
	headers := make([]proxy.ConfiguredHeader, 0, len(rows))
	for _, row := range rows {
		value := row.Value.String
		if row.IsSecret && value != "" {
			if value, err = s.enc.Decrypt(value); err != nil {
				return nil, fmt.Errorf("decrypt auth client header %s: %w", row.Name, err)
			}
		}
		headers = append(headers, proxy.ConfiguredHeader{
			IsRequired:             row.IsRequired,
			Name:                   row.Name,
			StaticValue:            value,
			ValueFromRequestHeader: row.ValueFromRequestHeader.String,
		})
	}
	return headers, nil
}

// annotateAuthClient records the resolution on the request span and, at debug
// level, the log. Ids and types only: header values never leave the resolver.
func annotateAuthClient(ctx context.Context, logger *slog.Logger, res authClientResolution) {
	attrs := []attribute.KeyValue{attr.AuthClientOutcome(string(res.outcome))}
	logAttrs := []any{attr.SlogAuthClientOutcome(string(res.outcome))}
	if res.clientID != uuid.Nil {
		attrs = append(attrs, attr.AuthClientID(res.clientID.String()))
		logAttrs = append(logAttrs, attr.SlogAuthClientID(res.clientID.String()))
	}
	if res.clientType != "" {
		attrs = append(attrs, attr.AuthClientType(res.clientType))
		logAttrs = append(logAttrs, attr.SlogAuthClientType(res.clientType))
	}
	if res.reason != "" {
		attrs = append(attrs, attr.AuthClientReason(res.reason))
		logAttrs = append(logAttrs, attr.SlogAuthClientReason(res.reason))
	}
	trace.SpanFromContext(ctx).SetAttributes(attrs...)
	logger.DebugContext(ctx, "auth client resolved", logAttrs...)
}

// enforceAuthClient applies the resolver to a direct proxied endpoint. It
// reports false when the upstream client choice serves the request. A gap
// writes the reconnect challenge and a refused client a request-access error.
func (s *Service) enforceAuthClient(ctx context.Context, w http.ResponseWriter, authentication *issuerGateAuthentication, selector *upstreamSelector) (upstreamChoice, bool, error) {
	var none upstreamChoice
	endpoint := authentication.endpoint
	res, err := s.resolveAuthClientForServing(ctx, selector, endpoint.McpServerID.UUID, endpoint.ProjectID, authentication.subject)
	if err != nil {
		return none, true, oops.E(oops.CodeUnexpected, err, "resolve auth client").LogError(ctx, s.logger)
	}
	switch res.outcome {
	case authClientLegacy:
		return none, false, nil
	case authClientGap:
		s.recordRemoteSessionRejection(ctx, authentication, "mcp issuer gate rejected: auth client unusable: "+res.reason, issuerGateReasonUpstreamSelectionUnavailable)
		return none, true, writeRemoteSessionReconnectChallenge(w, authentication)
	case authClientNoAccess:
		return none, true, s.authClientNoAccessError(ctx, endpoint.McpServerID.UUID, res)
	case authClientNoAuth:
		authentication.chosenClient = none.clientID
		authentication.chosenNoAuth = true
		authentication.authClient = &res
		return none, true, nil
	case authClientResolved:
	}
	authentication.authClient = &res
	if res.headersOnly() {
		authentication.chosenClient = none.clientID
		authentication.chosenNoAuth = true
		return none, true, nil
	}
	authentication.chosenClient = res.choice.clientID
	authentication.chosenNoAuth = false
	return res.choice, true, nil
}

// authClientNoAccessError refuses a caller the chosen client, with a link to
// request access to the server.
func (s *Service) authClientNoAccessError(ctx context.Context, serverID uuid.UUID, res authClientResolution) error {
	message := "you don't have access to the connection this MCP server uses. Ask an organization administrator to grant access."
	if link := strings.TrimSpace(s.requestAccessURL(ctx, serverID.String(), res.serverName)); link != "" {
		message += "\n\nRequest access:\n" + link
	}
	return oops.E(oops.CodeForbidden, nil, "%s", message).LogWarn(ctx, s.logger, attr.SlogAuthClientID(res.clientID.String()), attr.SlogAuthClientReason(res.reason))
}

// refreshNeedsAuthClientReconnect is the resolver's view of a refresh for an
// organization it serves: whether the refreshed session must reconnect, and
// whether the resolver decided at all. Undecided, the upstream client choice
// decides as before.
func (s *Service) refreshNeedsAuthClientReconnect(ctx context.Context, endpoint *ResolvedMcpEndpoint, subject urn.SessionSubject, jti string) (reconnect bool, decided bool, err error) {
	if !endpoint.McpServerID.Valid || s.authClientMode(ctx, endpoint.OrganizationID) != feature.AuthClientResolverEnforced {
		return false, false, nil
	}
	selector, err := s.refreshSelector(ctx, endpoint, jti)
	if err != nil {
		return false, false, err
	}
	res, err := selector.resolveAuthClient(ctx, endpoint.McpServerID.UUID)
	if err != nil {
		return false, false, err
	}
	if res.outcome == authClientResolved && res.choice.clientID.Valid {
		// The resolved registration's stored grant decides, whichever sign-in
		// it was consented through.
		reconnect, err := s.remoteChallengeMgr.AuthClientNeedsReconnect(ctx, remotesessions.AuthClientTokenRequest{
			ProjectID:           endpoint.ProjectID,
			OrganizationID:      endpoint.OrganizationID,
			UserSessionIssuerID: endpoint.UserSessionIssuerID,
			Subject:             subject,
			RegistrationID:      res.choice.clientID.UUID,
			SkipUnusable:        false,
		})
		if err != nil {
			return false, false, fmt.Errorf("check auth client reconnect: %w", err)
		}
		return reconnect, true, nil
	}
	reconnect, decided = refreshDecision(res)
	return reconnect, decided, nil
}

// refreshDecision is what a resolution means for a refresh: a gap reconnects,
// and a choice that needs no upstream session refreshes. OAuth clients and
// choices served the old way are left to the upstream client choice.
func refreshDecision(res authClientResolution) (reconnect bool, decided bool) {
	switch {
	case res.outcome == authClientGap:
		return true, true
	case res.outcome == authClientNoAuth, res.headersOnly():
		return false, true
	}
	return false, false
}

// legacyChoiceOutcome names the upstream client choice's outcome in the
// resolver's terms, for shadow comparison.
func legacyChoiceOutcome(choice upstreamChoice, err error) authClientOutcome {
	switch {
	case err != nil:
		return authClientGap
	case choice.noAuth():
		return authClientNoAuth
	default:
		return authClientResolved
	}
}

// shadowAuthClient compares the resolver with the upstream client choice for
// the same request and reports disagreement. It never changes the request and
// never asks the permitter.
func (s *Service) shadowAuthClient(ctx context.Context, selector *upstreamSelector, serverID, projectID uuid.UUID, subject urn.SessionSubject, choice upstreamChoice, choiceErr error) {
	res, err := selector.resolveAuthClient(ctx, serverID)
	if err != nil {
		s.metrics.RecordAuthClientParity(ctx, false, authClientParityError, string(legacyChoiceOutcome(choice, choiceErr)), authClientReasonNone)
		s.logger.WarnContext(ctx, "auth client resolver shadow failed", attr.SlogMcpServerID(serverID.String()), attr.SlogError(err))
		return
	}
	// Compare only a legacy decision: a missing or unusable choice refuses
	// the request, any other error is not a decision.
	legacyDenied := errors.Is(choiceErr, errUpstreamSelectionMissing) || errors.Is(choiceErr, errUpstreamSelectionUnusable)
	if res.outcome == authClientResolved && (choiceErr == nil || legacyDenied) {
		s.shadowAuthClientUse(ctx, selector.organizationID, choiceErr == nil, projectID, res.clientID, res.clientType, subject)
	}
	annotateAuthClient(ctx, s.logger, res)
	s.reportAuthClientParity(ctx, selector.organizationID, serverID, res, choice, choiceErr)
}

// reportAuthClientParity counts one shadow comparison and logs a
// disagreement, ids and outcomes only. A choice the resolver hands back to the
// upstream client choice is compared only when that hand-back hides a
// difference; an unmapped choice is counted without a warning.
func (s *Service) reportAuthClientParity(ctx context.Context, organizationID string, serverID uuid.UUID, res authClientResolution, choice upstreamChoice, choiceErr error) {
	if choiceErr != nil && !errors.Is(choiceErr, errUpstreamSelectionMissing) && !errors.Is(choiceErr, errUpstreamSelectionUnusable) {
		return
	}
	legacy := legacyChoiceOutcome(choice, choiceErr)
	var agreed bool
	warn := true
	switch {
	case res.outcome == authClientLegacy && res.reason == authClientReasonUnmapped:
		agreed, warn = false, false
	case res.outcome == authClientLegacy && (res.reason == authClientReasonHeadersStale || res.reason == authClientReasonLegacyMayChain):
		agreed = false
	case res.outcome == authClientLegacy:
		return
	default:
		// The upstream client choice serves a headers-only client as no-auth.
		expected := res.outcome
		if res.headersOnly() {
			expected = authClientNoAuth
		}
		agreed = legacy == expected
		if agreed && expected == authClientResolved {
			agreed = res.choice.clientID == choice.clientID
		}
	}
	trace.SpanFromContext(ctx).SetAttributes(attr.AuthClientLegacyOutcome(string(legacy)), attr.AuthClientParityAgreed(agreed))
	reason := res.reason
	if reason == "" {
		reason = authClientReasonNone
	}
	s.metrics.RecordAuthClientParity(ctx, agreed, string(res.outcome), string(legacy), reason)
	if agreed || !warn {
		return
	}
	s.logger.WarnContext(ctx, "auth client resolver parity mismatch",
		attr.SlogMcpServerID(serverID.String()),
		attr.SlogOrganizationID(organizationID),
		attr.SlogAuthClientID(res.clientID.String()),
		attr.SlogAuthClientType(res.clientType),
		attr.SlogAuthClientOutcome(string(res.outcome)),
		attr.SlogAuthClientReason(res.reason),
		attr.SlogAuthClientLegacyOutcome(string(legacy)),
		attr.SlogRemoteSessionClientID(choice.clientID.UUID.String()),
	)
}

// authClientMode is the organization's resolver mode.
func (s *Service) authClientMode(ctx context.Context, organizationID string) feature.AuthClientResolverMode {
	return feature.AuthClientResolver(ctx, s.features, organizationID)
}
