// Upstream credential choices for sessions issued without a browser: an
// ID-JAG exchange or a workload grant. Where typed auth client resolution is
// enforced, each MCP server the session targets gets a choice before the
// session is written: the one the token request names, otherwise the one the
// caller's issuance profile names, otherwise the server's only ready
// credential the subject may use. Several ready credentials with no choice
// named is an error, never the consent screen's preselection order, and so is
// a named choice that does not validate. Nothing is recorded unless every
// server's choice validates.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// tokenParamUpstreamAuthClient names one MCP server's upstream credential
	// on a token request, as "<mcp_server_id>:<auth_client_id>", or
	// "<mcp_server_id>:none" for a server that needs no upstream
	// authentication. It may repeat, once per server.
	tokenParamUpstreamAuthClient = "upstream_auth_client" //nolint:gosec // a form parameter name, not a credential

	// tokenParamNoAuth is the value naming no upstream authentication.
	tokenParamNoAuth = "none"

	// tokenErrorSelectionRequired answers a token request for a server that
	// offers several credentials the subject may use when neither the request
	// nor the caller's profile names one. Not an IETF-registered code.
	tokenErrorSelectionRequired = "selection_required" //nolint:gosec // an OAuth error code, not a credential

	// tokenErrorSetupRequired answers a token request whose credential for a
	// server cannot be used until it is set up or connected. Not an
	// IETF-registered code.
	tokenErrorSetupRequired = "setup_required"

	// maxRequestedAuthClients bounds the choices one token request names; a
	// gateway's members fit well within it.
	maxRequestedAuthClients = 100
)

// headlessCaller is who asks for a session without a browser: a client, or a
// workload subject. It selects the caller's issuance profile.
type headlessCaller struct {
	// clientID is the authenticated user session client, when one is.
	clientID uuid.NullUUID

	// workloadIssuerID and workloadSubject name a workload caller.
	workloadIssuerID uuid.NullUUID
	workloadSubject  string
}

// headlessChoice is one server's requested or configured choice: a client, or
// none for no upstream authentication.
type headlessChoice struct {
	clientID uuid.NullUUID
}

// parseRequestedAuthClients reads the token request's per-server choices. A
// malformed value is an invalid_request error.
func parseRequestedAuthClients(values []string) (map[uuid.UUID]headlessChoice, error) {
	if len(values) > maxRequestedAuthClients {
		return nil, oops.NewOAuthError(http.StatusBadRequest, "invalid_request", fmt.Sprintf("at most %d %s values are accepted", maxRequestedAuthClients, tokenParamUpstreamAuthClient), nil)
	}
	requested := make(map[uuid.UUID]headlessChoice, len(values))
	for _, value := range values {
		rawServer, rawClient, ok := strings.Cut(value, ":")
		serverID, err := uuid.Parse(rawServer)
		if !ok || err != nil {
			return nil, oops.NewOAuthError(http.StatusBadRequest, "invalid_request", tokenParamUpstreamAuthClient+" must be <mcp_server_id>:<auth_client_id> or <mcp_server_id>:none", nil)
		}
		if _, dup := requested[serverID]; dup {
			return nil, oops.NewOAuthError(http.StatusBadRequest, "invalid_request", tokenParamUpstreamAuthClient+" names a server twice", nil)
		}
		choice := headlessChoice{clientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}
		if rawClient != tokenParamNoAuth {
			clientID, err := uuid.Parse(rawClient)
			if err != nil {
				return nil, oops.NewOAuthError(http.StatusBadRequest, "invalid_request", tokenParamUpstreamAuthClient+" must name an auth client id or none", nil)
			}
			choice.clientID = uuid.NullUUID{UUID: clientID, Valid: true}
		}
		requested[serverID] = choice
	}
	return requested, nil
}

// headlessAuthClientPicks decides the choice of every server the session
// targets and returns them as the issuance's picks: the registration a
// choice authenticates through, keyed by server, and the typed client. It
// returns nil maps where typed auth client resolution is not enforced, where
// the server's choice keeps being decided at recording, and for a session
// that targets no one server. enforced is the organization's enforcement
// state as the caller read it, which it records the session's choices under
// too, so the picks and the recording never disagree.
func (s *Service) headlessAuthClientPicks(ctx context.Context, endpoint *ResolvedMcpEndpoint, subject urn.SessionSubject, caller headlessCaller, requested map[uuid.UUID]headlessChoice, enforced bool) (map[uuid.UUID]uuid.UUID, map[uuid.UUID]uuid.UUID, error) {
	if !enforced {
		if len(requested) > 0 {
			return nil, nil, oops.NewOAuthError(http.StatusBadRequest, "invalid_request", tokenParamUpstreamAuthClient+" is not accepted for this organization yet", errChoiceNotAccepted)
		}
		return nil, nil, nil
	}

	servers, err := s.headlessTargetServers(ctx, endpoint, subject)
	if err != nil {
		return nil, nil, err
	}
	inScope := make(map[uuid.UUID]bool, len(servers))
	ids := make([]uuid.UUID, 0, len(servers))
	for _, server := range servers {
		inScope[server.id] = true
		ids = append(ids, server.id)
	}
	for serverID := range requested {
		if !inScope[serverID] {
			return nil, nil, oops.NewOAuthError(http.StatusBadRequest, "invalid_request", tokenParamUpstreamAuthClient+" names a server this token does not reach", errChoiceServerNotReached)
		}
	}
	if len(servers) == 0 {
		return nil, nil, nil
	}

	configured, err := s.issuanceProfileChoices(ctx, endpoint, caller, ids)
	if err != nil {
		return nil, nil, err
	}

	// Every target server's options are read once, then decided per server.
	options, err := s.consentAuthChoices(ctx, endpoint.OrganizationID, subject, servers, nil, nil, true)
	if err != nil {
		return nil, nil, fmt.Errorf("decide server credentials: %w", err)
	}
	upstreamPicks := map[uuid.UUID]uuid.UUID{}
	authClientPicks := map[uuid.UUID]uuid.UUID{}
	for _, server := range servers {
		auth := options[server.id]
		if auth == nil {
			return nil, nil, setupRequired("a server's upstream sign-in is not set up", errChoiceNotSetUp)
		}
		if err := s.headlessReadiness(ctx, endpoint, subject, server, auth); err != nil {
			return nil, nil, err
		}
		choice, named := requested[server.id]
		var selected *consentAuthClientOption
		if profileChoice, ok := configured[server.id]; ok && !named {
			selected, err = decideHeadlessChoice(server, auth, profileChoice, true)
			// A profile's choice that no longer validates, because the
			// credential was withdrawn or the subject lost access, gives way
			// to the choice the caller would get without a profile.
			if _, stale := errors.AsType[*oops.OAuthError](err); stale {
				selected, err = decideHeadlessChoice(server, auth, choice, false)
			}
		} else {
			selected, err = decideHeadlessChoice(server, auth, choice, named)
		}
		if err != nil {
			return nil, nil, err
		}
		if selected == nil {
			continue
		}
		authClientPicks[server.id] = selected.ID
		if selected.registrationID.Valid {
			upstreamPicks[server.id] = selected.registrationID.UUID
		}
	}
	return upstreamPicks, authClientPicks, nil
}

// headlessTargetServers is the servers a session for endpoint reaches: its
// own server, or the gateway members the subject reaches.
func (s *Service) headlessTargetServers(ctx context.Context, endpoint *ResolvedMcpEndpoint, subject urn.SessionSubject) ([]consentAuthServer, error) {
	switch {
	case endpoint.McpServerID.Valid && !endpoint.ToolsetID.Valid:
		return []consentAuthServer{{id: endpoint.McpServerID.UUID, projectID: endpoint.ProjectID}}, nil
	case endpoint.MetaMcpServerID.Valid:
		members, err := s.consentSubjectMembers(ctx, endpoint, AuthnChallengeState{Subject: &subject, ID: "headless:" + uuid.NewString()}) //nolint:exhaustruct // only the subject and a session-like id are read
		if err != nil {
			return nil, fmt.Errorf("list gateway members: %w", err)
		}
		servers := make([]consentAuthServer, 0, len(members))
		for _, member := range members {
			if member.backend == metaMemberBackendProxied {
				servers = append(servers, consentAuthServer{id: member.serverID, projectID: member.projectID})
			}
		}
		return servers, nil
	}
	return nil, nil
}

// issuanceProfileChoices is the caller's configured choice for each of the
// servers, if an issuance profile exists for it.
func (s *Service) issuanceProfileChoices(ctx context.Context, endpoint *ResolvedMcpEndpoint, caller headlessCaller, serverIDs []uuid.UUID) (map[uuid.UUID]headlessChoice, error) {
	queries := upstreamclientsrepo.New(s.db)
	profileID, err := queries.GetIssuanceProfileForCaller(ctx, upstreamclientsrepo.GetIssuanceProfileForCallerParams{
		UserSessionIssuerID: endpoint.UserSessionIssuerID,
		OrganizationID:      endpoint.OrganizationID,
		UserSessionClientID: caller.clientID,
		WorkloadIssuerID:    caller.workloadIssuerID,
		WorkloadSubject:     conv.ToPGTextEmpty(caller.workloadSubject),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load issuance profile: %w", err)
	}
	rows, err := queries.ListCredentialChoices(ctx, upstreamclientsrepo.ListCredentialChoicesParams{
		CredentialKind: upstreamclients.CredentialKindIssuanceProfile,
		CredentialID:   profileID,
		OrganizationID: endpoint.OrganizationID,
		McpServerIds:   serverIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("load issuance profile choices: %w", err)
	}
	choices := make(map[uuid.UUID]headlessChoice, len(rows))
	for _, row := range rows {
		choices[row.McpServerID] = headlessChoice{clientID: row.AuthClientID}
	}
	return choices, nil
}

// decideHeadlessChoice validates a named choice for one server, or, with none
// named, takes the server's only ready credential the subject may use. auth
// is the server's options with their headless readiness. It returns nil for
// no upstream authentication.
func decideHeadlessChoice(server consentAuthServer, auth *consentServerAuth, choice headlessChoice, named bool) (*consentAuthClientOption, error) {
	switch auth.Status {
	case consentAuthNoAuth:
		if named && choice.clientID.Valid {
			return nil, setupRequired("the server needs no upstream authentication, so it takes no credential", errChoiceTakesNoCredential)
		}
		return nil, nil
	case consentAuthSetupIncomplete:
		return nil, setupRequired("a server's upstream sign-in is not set up", errChoiceNotSetUp)
	case consentAuthNoAccess:
		return nil, accessDenied("you may not use any of a server's upstream credentials", errChoiceNoneAllowed)
	case consentAuthChoose:
	}

	if named {
		if !choice.clientID.Valid {
			return nil, setupRequired("a server needs upstream authentication, so it needs a credential", errChoiceNeedsCredential)
		}
		for i := range auth.Options {
			option := &auth.Options[i]
			if option.ID != choice.clientID.UUID {
				continue
			}
			if !option.Usable() {
				return nil, setupRequired("the credential chosen for a server is not ready: set it up or connect it first", errChoiceNotReady)
			}
			return option, nil
		}
		// Denied, from another tenant, or not offered: the subject may not use it.
		return nil, accessDenied("you may not use the credential chosen for a server", errChoiceDenied)
	}

	var ready []*consentAuthClientOption
	for i := range auth.Options {
		if auth.Options[i].Usable() {
			ready = append(ready, &auth.Options[i])
		}
	}
	switch len(ready) {
	case 0:
		return nil, setupRequired("no upstream credential for a server is ready: set one up or connect it first", errChoiceNoneReady)
	case 1:
		return ready[0], nil
	}
	return nil, oops.NewOAuthError(http.StatusBadRequest, tokenErrorSelectionRequired, fmt.Sprintf("server %s offers several upstream credentials; name one with %s=%s:<auth_client_id> or configure an issuance profile", server.id, tokenParamUpstreamAuthClient, server.id), errChoiceNotNamed)
}

// headlessReadiness marks the personal options of a server ready or not from
// the subject's stored connection, which a browserless caller cannot make.
// Identity-provider clients are judged when used.
func (s *Service) headlessReadiness(ctx context.Context, endpoint *ResolvedMcpEndpoint, subject urn.SessionSubject, server consentAuthServer, auth *consentServerAuth) error {
	for i := range auth.Options {
		option := &auth.Options[i]
		switch option.Type {
		case authz.AuthClientTypeEMA:
			if option.State == consentClientConnect {
				option.State = consentClientReady
			}
		case authz.AuthClientTypeRemoteSession:
			if option.State != consentClientConnect || !option.registrationID.Valid {
				continue
			}
			reconnect, err := s.remoteChallengeMgr.AuthClientNeedsReconnect(ctx, remotesessions.AuthClientTokenRequest{
				ProjectID: server.projectID, OrganizationID: endpoint.OrganizationID, UserSessionIssuerID: endpoint.UserSessionIssuerID,
				Subject: subject, RegistrationID: option.registrationID.UUID, SkipUnusable: false, Attribution: remotesessions.RefreshAttribution{},
			})
			if err != nil {
				return fmt.Errorf("check upstream connection: %w", err)
			}
			option.State = consentClientReady
			if reconnect {
				option.State = consentClientConnect
			}
		}
	}
	// The consent preselection does not apply without a browser.
	for i := range auth.Options {
		auth.Options[i].Selected = false
	}
	return nil
}

func setupRequired(description string, cause headlessChoiceError) error {
	return oops.NewOAuthError(http.StatusBadRequest, tokenErrorSetupRequired, description, cause)
}

func accessDenied(description string, cause headlessChoiceError) error {
	return oops.NewOAuthError(http.StatusBadRequest, "access_denied", description, cause)
}

// headlessChoiceError is why a headless upstream choice was refused. It is
// the refusal's internal cause, never part of the response.
type headlessChoiceError string

func (p headlessChoiceError) Error() string { return string(p) }

const (
	// errChoiceNotAccepted names a choice in an organization that does not take them yet.
	errChoiceNotAccepted headlessChoiceError = "choice_not_accepted"

	// errChoiceServerNotReached names a choice for a server the token does not reach.
	errChoiceServerNotReached headlessChoiceError = "choice_server_not_reached"

	// errChoiceTakesNoCredential names a credential for a server that needs none.
	errChoiceTakesNoCredential headlessChoiceError = "credential_not_needed" //nolint:gosec // a reason code, not a credential

	// errChoiceNeedsCredential names no credential for a server that needs one.
	errChoiceNeedsCredential headlessChoiceError = "credential_needed" //nolint:gosec // a reason code, not a credential

	// errChoiceNotSetUp is a server whose upstream sign-in is not set up.
	errChoiceNotSetUp headlessChoiceError = "sign_in_not_set_up"

	// errChoiceNotReady names a credential that is not set up or connected yet.
	errChoiceNotReady headlessChoiceError = "credential_not_ready"

	// errChoiceNoneReady is a server with no ready credential and none named.
	errChoiceNoneReady headlessChoiceError = "no_credential_ready"

	// errChoiceDenied names a credential the subject may not use.
	errChoiceDenied headlessChoiceError = "credential_denied"

	// errChoiceNoneAllowed is a server whose credentials the subject may not use.
	errChoiceNoneAllowed headlessChoiceError = "no_credential_allowed"

	// errChoiceNotNamed is a server with several ready credentials and none named.
	errChoiceNotNamed headlessChoiceError = "credential_not_chosen"
)
