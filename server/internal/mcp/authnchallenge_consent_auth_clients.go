// Typed auth client choices on the consent screen. Where typed auth client
// resolution is enforced, each MCP server, and each gateway member, offers the
// clients it is associated with that the person may use, and the person picks
// one. The pick becomes the credential's choice for the server when it is
// issued; nothing here changes an existing credential's choice.

package mcp

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/feature"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// consentAuthClientState is how one offered client stands for this person.
type consentAuthClientState string

const (
	// consentClientReady can be chosen now.
	consentClientReady consentAuthClientState = "ready"
	// consentClientConnect needs the person to connect their own account
	// first.
	consentClientConnect consentAuthClientState = "connect"
	// consentClientReconnect needs the person to redo their connection.
	consentClientReconnect consentAuthClientState = "reconnect"
	// consentClientSetup cannot be used until an administrator finishes
	// setting it up.
	consentClientSetup consentAuthClientState = "setup"
)

// consentServerAuthStatus is what a server's upstream authentication means
// for this person.
type consentServerAuthStatus string

const (
	// consentAuthChoose offers one or more clients.
	consentAuthChoose consentServerAuthStatus = "choose"
	// consentAuthNoAuth needs no upstream authentication.
	consentAuthNoAuth consentServerAuthStatus = "no_auth"
	// consentAuthSetupIncomplete needs authentication but offers no client
	// anyone can use yet.
	consentAuthSetupIncomplete consentServerAuthStatus = "setup_incomplete"
	// consentAuthNoAccess offers clients, none of which this person may use.
	consentAuthNoAccess consentServerAuthStatus = "no_access"
)

// consentAuthClientOption is one client a server offers this person.
type consentAuthClientOption struct {
	ID    uuid.UUID
	Name  string
	Type  string
	State consentAuthClientState

	// ConnectClientID is the consent card that connects or reconnects a
	// personal client, when State asks for it.
	ConnectClientID string

	// Selected marks the option the page preselects or the person picked.
	Selected bool

	// registrationID is the OAuth registration the client authenticates
	// through, recorded with the pick.
	registrationID uuid.NullUUID
}

// TypeLabel names how the client authenticates, in the person's words.
func (o consentAuthClientOption) TypeLabel() string {
	switch o.Type {
	case authz.AuthClientTypeRemoteSession:
		return "Your own account"
	case authz.AuthClientTypeServiceAccount:
		return "Shared service account"
	case authz.AuthClientTypeStaticCredential:
		return "Shared credential"
	case authz.AuthClientTypePassThrough:
		return "Your app's own credential"
	case authz.AuthClientTypeEMA:
		return "Your identity provider"
	}
	return "Credential"
}

// Shared reports a client whose upstream identity everyone who uses it
// shares, so the upstream records their actions as that identity.
func (o consentAuthClientOption) Shared() bool {
	return o.Type == authz.AuthClientTypeServiceAccount || o.Type == authz.AuthClientTypeStaticCredential
}

// Usable reports an option that can be approved now.
func (o consentAuthClientOption) Usable() bool { return o.State == consentClientReady }

// consentServerAuth is one server's upstream authentication on the consent
// screen.
type consentServerAuth struct {
	ServerID uuid.UUID
	Status   consentServerAuthStatus
	Options  []consentAuthClientOption
}

// Selected is the option the credential will record, if any is usable.
func (a *consentServerAuth) Selected() *consentAuthClientOption {
	if a == nil {
		return nil
	}
	for i := range a.Options {
		if a.Options[i].Selected {
			return &a.Options[i]
		}
	}
	return nil
}

// ShowPicker reports whether the person has a choice to make.
func (a *consentServerAuth) ShowPicker() bool {
	return a != nil && a.Status == consentAuthChoose && len(a.Options) > 1
}

// Approvable reports whether the server can be approved as it stands.
func (a *consentServerAuth) Approvable() bool {
	if a == nil {
		return true
	}
	switch a.Status {
	case consentAuthNoAuth:
		return true
	case consentAuthChoose:
		return a.Selected() != nil
	case consentAuthSetupIncomplete, consentAuthNoAccess:
	}
	return false
}

// Problem explains why the server cannot be approved yet, or is empty.
func (a *consentServerAuth) Problem() string {
	if a == nil {
		return ""
	}
	switch a.Status {
	case consentAuthSetupIncomplete:
		return "This server's sign-in isn't set up yet. Ask an administrator to finish setting it up."
	case consentAuthNoAccess:
		return "You don't have access to any of the ways this server signs in. Ask an administrator for access."
	case consentAuthChoose:
		if a.Selected() == nil {
			return "Connect one of the options above to continue."
		}
	case consentAuthNoAuth:
	}
	return ""
}

// consentAuthServer is one server whose choice the consent screen offers.
type consentAuthServer struct {
	id        uuid.UUID
	projectID uuid.UUID
}

// consentAuthChoicesEnforced reports whether the organization chooses
// upstream authentication through typed auth clients. Elsewhere the consent
// screen keeps choosing through its service cards.
func (s *Service) consentAuthChoicesEnforced(ctx context.Context, organizationID string) (bool, error) {
	mode, err := s.authClientMode(ctx, organizationID)
	if err != nil {
		return false, err
	}
	return mode == feature.AuthClientResolverEnforced, nil
}

// consentAuthChoices decides, for each server, the clients this person may
// choose from, their state, and which one is selected: the person's pick when
// it is still usable, otherwise the client they last approved for the server,
// otherwise the first usable one in the fixed preselection order. Nothing is
// selected that cannot be approved now. With deferPersonal, personal and
// identity-provider clients the resolver accepts are left in
// consentClientConnect for the caller to judge, as a browserless caller has
// no consent cards; cards is then unused.
func (s *Service) consentAuthChoices(ctx context.Context, organizationID string, subject urn.SessionSubject, servers []consentAuthServer, cards []remoteSessionCard, picked map[uuid.UUID]uuid.UUID, deferPersonal bool) (map[uuid.UUID]*consentServerAuth, error) {
	if len(servers) == 0 {
		return map[uuid.UUID]*consentServerAuth{}, nil
	}
	ids := make([]uuid.UUID, 0, len(servers))
	for _, server := range servers {
		ids = append(ids, server.id)
	}
	queries := upstreamclientsrepo.New(s.db)
	rows, err := queries.ListServerConsentAuthClients(ctx, upstreamclientsrepo.ListServerConsentAuthClientsParams{OrganizationID: organizationID, McpServerIds: ids})
	if err != nil {
		return nil, fmt.Errorf("list consent auth clients: %w", err)
	}
	remembered := map[uuid.UUID]uuid.UUID{}
	rememberedRows, err := queries.ListRememberedAuthClients(ctx, upstreamclientsrepo.ListRememberedAuthClientsParams{SubjectUrn: subject.String(), OrganizationID: organizationID, McpServerIds: ids})
	if err != nil {
		return nil, fmt.Errorf("list remembered auth clients: %w", err)
	}
	for _, row := range rememberedRows {
		remembered[row.McpServerID] = row.AuthClientID
	}

	byServer := map[uuid.UUID][]upstreamclientsrepo.ListServerConsentAuthClientsRow{}
	for _, row := range rows {
		byServer[row.McpServerID] = append(byServer[row.McpServerID], row)
	}

	// A personal connection or identity-provider sign-in belongs to a person;
	// a caller representing nobody cannot use one.
	represented := subject.Kind == urn.SessionSubjectKindUser || subject.Kind == urn.SessionSubjectKindAgent

	choices := make(map[uuid.UUID]*consentServerAuth, len(servers))
	for _, server := range servers {
		serverRows := byServer[server.id]
		auth := &consentServerAuth{ServerID: server.id, Status: consentAuthChoose, Options: nil}
		choices[server.id] = auth

		var requires *bool
		if len(serverRows) > 0 && serverRows[0].RequiresUpstreamAuth.Valid {
			requires = &serverRows[0].RequiresUpstreamAuth.Bool
		}
		offered := slices.DeleteFunc(slices.Clone(serverRows), func(row upstreamclientsrepo.ListServerConsentAuthClientsRow) bool {
			return !row.AuthClientID.Valid
		})
		switch {
		case requires != nil && !*requires:
			auth.Status = consentAuthNoAuth
			continue
		case len(offered) == 0 && requires != nil:
			auth.Status = consentAuthSetupIncomplete
			continue
		case len(offered) == 0:
			// Undecided with nothing offered serves without upstream
			// authentication, as the resolver does.
			auth.Status = consentAuthNoAuth
			continue
		}

		denied := 0
		for _, row := range offered {
			clientType := row.ClientType.String
			if !represented && (clientType == authz.AuthClientTypeRemoteSession || clientType == authz.AuthClientTypeEMA) {
				continue
			}
			res, err := s.resolveAuthClientChoiceForConsent(ctx, organizationID, server.id, server.projectID, row.AuthClientID.UUID, subject)
			if err != nil {
				return nil, fmt.Errorf("resolve consent auth client: %w", err)
			}
			if res.outcome == authClientNoAccess {
				denied++
				continue
			}
			option := consentAuthClientOption{
				ID:              row.AuthClientID.UUID,
				Name:            row.AuthClientName.String,
				Type:            clientType,
				State:           consentClientReady,
				ConnectClientID: "",
				Selected:        false,
				registrationID:  row.RemoteSessionClientID,
			}
			switch {
			case res.outcome == authClientGap:
				option.State = consentClientSetup
			case (clientType == authz.AuthClientTypeRemoteSession || clientType == authz.AuthClientTypeEMA) && deferPersonal:
				option.State = consentClientConnect
			case clientType == authz.AuthClientTypeRemoteSession || clientType == authz.AuthClientTypeEMA:
				option.State, option.ConnectClientID = personalReadiness(clientType, row.RemoteSessionClientID, cards)
			}
			auth.Options = append(auth.Options, option)
		}
		if len(auth.Options) == 0 {
			auth.Status = consentAuthSetupIncomplete
			if denied > 0 {
				auth.Status = consentAuthNoAccess
			}
			continue
		}
		selectConsentAuthClient(auth, picked[server.id], remembered[server.id])
	}
	return choices, nil
}

// personalReadiness is the state of a personal or identity-provider client
// from the consent card that connects its registration. A registration this
// sign-in has no card for cannot be connected here.
func personalReadiness(clientType string, registrationID uuid.NullUUID, cards []remoteSessionCard) (consentAuthClientState, string) {
	if !registrationID.Valid {
		return consentClientSetup, ""
	}
	for _, card := range cards {
		if card.ClientID != registrationID.UUID.String() {
			continue
		}
		status := cardStatus(card)
		if clientType == authz.AuthClientTypeEMA {
			switch status {
			case gatewayServerChained, gatewayServerChainPending:
				return consentClientReady, ""
			case gatewayServerReconnect:
				return consentClientReconnect, card.ClientID
			case gatewayServerNotConnected:
				return consentClientConnect, card.ClientID
			case gatewayServerConnected, gatewayServerNoSignIn, gatewayServerNotSetUp, gatewayServerUnavailable, gatewayServerNoAccess:
			}
			return consentClientSetup, ""
		}
		switch status {
		case gatewayServerConnected, gatewayServerChained, gatewayServerChainPending:
			return consentClientReady, ""
		case gatewayServerReconnect:
			return consentClientReconnect, card.ClientID
		case gatewayServerNotConnected, gatewayServerNoSignIn, gatewayServerNotSetUp, gatewayServerUnavailable, gatewayServerNoAccess:
		}
		return consentClientConnect, card.ClientID
	}
	return consentClientSetup, ""
}

// selectConsentAuthClient marks the option the credential will record. A pick
// or a remembered choice applies only while it is still usable; otherwise
// the fixed order decides: identity provider, then shared clients by name,
// then the person's own account, then the app's own credential.
func selectConsentAuthClient(auth *consentServerAuth, picked, remembered uuid.UUID) {
	slices.SortStableFunc(auth.Options, func(a, b consentAuthClientOption) int {
		return cmp.Or(cmp.Compare(preselectionRank(a.Type), preselectionRank(b.Type)), cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID.String(), b.ID.String()))
	})
	for _, want := range []uuid.UUID{picked, remembered} {
		if want == uuid.Nil {
			continue
		}
		for i := range auth.Options {
			if auth.Options[i].ID == want && auth.Options[i].Usable() {
				auth.Options[i].Selected = true
				return
			}
		}
	}
	for i := range auth.Options {
		if auth.Options[i].Usable() {
			auth.Options[i].Selected = true
			return
		}
	}
}

// preselectionRank orders client types for preselection.
func preselectionRank(clientType string) int {
	switch clientType {
	case authz.AuthClientTypeEMA:
		return 0
	case authz.AuthClientTypeServiceAccount, authz.AuthClientTypeStaticCredential:
		return 1
	case authz.AuthClientTypeRemoteSession:
		return 2
	case authz.AuthClientTypePassThrough:
		return 3
	}
	return 4
}

// directConsentAuthChoice is the upstream credential choice of a direct
// proxied server's consent page, with picked selected when it is usable. It
// is nil for gateways, hosted servers, first-party pages and organizations
// where typed auth client resolution is not enforced.
func (s *Service) directConsentAuthChoice(ctx context.Context, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState, cards []remoteSessionCard, picked uuid.UUID) (*consentServerAuth, error) {
	if challengeState.FirstParty || challengeState.Subject == nil || !endpoint.McpServerID.Valid || endpoint.ToolsetID.Valid || endpoint.MetaMcpServerID.Valid {
		return nil, nil
	}
	enforced, err := s.consentAuthChoicesEnforced(ctx, endpoint.OrganizationID)
	if err != nil || !enforced {
		return nil, err
	}
	serverID := endpoint.McpServerID.UUID
	choices, err := s.consentAuthChoices(ctx, endpoint.OrganizationID, *challengeState.Subject, []consentAuthServer{{id: serverID, projectID: endpoint.ProjectID}}, cards, map[uuid.UUID]uuid.UUID{serverID: picked}, false)
	if err != nil {
		return nil, err
	}
	return choices[serverID], nil
}

// consentDirectAuthClientPick validates the credential posted with a direct
// server's approval against the choices computed again now, and returns it as
// the grant's typed pick. The registration it names replaces the card-derived
// pick in upstreamPicks; a headers-only client removes it. A pick that is no
// longer usable, or a missing one where a choice is required, is
// errConsentConfigurationChanged. It returns nil where the page offers no
// typed choice.
func (s *Service) consentDirectAuthClientPick(ctx context.Context, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState, posted string, upstreamPicks map[uuid.UUID]uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	picked := uuid.Nil
	if posted != "" {
		parsed, err := uuid.Parse(posted)
		if err != nil {
			return nil, errConsentConfigurationChanged
		}
		picked = parsed
	}
	cards, err := s.buildRemoteSessionCards(ctx, endpoint, challengeState, s.resolveAutoRefreshPolicy(ctx, endpoint.OrganizationID))
	if err != nil {
		return nil, fmt.Errorf("build remote session cards: %w", err)
	}
	auth, err := s.directConsentAuthChoice(ctx, endpoint, challengeState, cards, picked)
	if err != nil || auth == nil {
		return nil, err
	}
	if !auth.Approvable() {
		return nil, errConsentConfigurationChanged
	}
	selected := auth.Selected()
	if selected == nil {
		// No upstream authentication: nothing to pick, and no card's client
		// is recorded for the server.
		delete(upstreamPicks, endpoint.McpServerID.UUID)
		return nil, nil
	}
	if picked != uuid.Nil && selected.ID != picked {
		return nil, errConsentConfigurationChanged
	}
	serverID := endpoint.McpServerID.UUID
	if selected.registrationID.Valid && upstreamPicks != nil {
		upstreamPicks[serverID] = selected.registrationID.UUID
	} else {
		delete(upstreamPicks, serverID)
	}
	return map[uuid.UUID]uuid.UUID{serverID: selected.ID}, nil
}

// gatewayAuthClientParam prefixes the query parameter naming a gateway
// member's picked credential by the member's slug. Changing a pick reloads
// the page, so the tools captured for approval are the ones that credential
// reaches.
const gatewayAuthClientParam = "auth_client."

// gatewayConsentAuthChoices is each proxied member's upstream credential
// choice on a gateway's consent page, with the picks the page's query names.
// It is nil where typed auth client resolution is not enforced.
func (s *Service) gatewayConsentAuthChoices(ctx context.Context, r *http.Request, endpoint *ResolvedMcpEndpoint, subject urn.SessionSubject, members []metaMember, cards []remoteSessionCard) (map[uuid.UUID]*consentServerAuth, error) {
	enforced, err := s.consentAuthChoicesEnforced(ctx, endpoint.OrganizationID)
	if err != nil || !enforced {
		return nil, err
	}
	servers := make([]consentAuthServer, 0, len(members))
	picked := map[uuid.UUID]uuid.UUID{}
	query := r.URL.Query()
	for _, member := range members {
		if member.backend != metaMemberBackendProxied {
			continue
		}
		servers = append(servers, consentAuthServer{id: member.serverID, projectID: member.projectID})
		if raw := query.Get(gatewayAuthClientParam + member.slug); raw != "" {
			if id, err := uuid.Parse(raw); err == nil {
				picked[member.serverID] = id
			}
		}
	}
	return s.consentAuthChoices(ctx, endpoint.OrganizationID, subject, servers, cards, picked, false)
}

// resolveMemberAuthChoice is the resolution a gateway member's credential
// choice gives its discovery: the selected client, no upstream
// authentication, or a refusal that leaves the member unavailable.
func (s *Service) resolveMemberAuthChoice(ctx context.Context, endpoint *ResolvedMcpEndpoint, member metaMember, auth *consentServerAuth, subject urn.SessionSubject) (authClientResolution, error) {
	switch {
	case auth == nil:
		return gapResolution(authClientReasonMissingChoice), nil
	case auth.Status == consentAuthNoAuth:
		return s.resolveAuthClientForConsent(ctx, endpoint.OrganizationID, member.serverID, member.projectID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, subject)
	case auth.Status == consentAuthNoAccess:
		res := gapResolution(authClientReasonNotPermitted)
		res.outcome = authClientNoAccess
		res.serverName = member.name
		return res, nil
	}
	if selected := auth.Selected(); selected != nil {
		return s.resolveAuthClientChoiceForConsent(ctx, endpoint.OrganizationID, member.serverID, member.projectID, selected.ID, subject)
	}
	return gapResolution(authClientReasonSetup), nil
}
