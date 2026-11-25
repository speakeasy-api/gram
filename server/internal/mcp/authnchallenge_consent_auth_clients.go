// Typed auth client choices on the consent screen. Where typed auth client
// resolution is enforced, each MCP server, and each gateway member, offers the
// clients it is associated with that the person may use, and the person picks
// one. The pick becomes the credential's choice for the server when it is
// issued; nothing here changes an existing credential's choice.

package mcp

import (
	"cmp"
	"context"
	"errors"
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
	// unavailable marks an outage; the option can still be picked explicitly.
	unavailable bool
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

type typedConsentAuthKey struct{}

func withTypedConsentAuth(ctx context.Context) context.Context {
	return context.WithValue(ctx, typedConsentAuthKey{}, true)
}

func (s *Service) typedConsentAuth(ctx context.Context, organizationID string) (bool, error) {
	if typed, _ := ctx.Value(typedConsentAuthKey{}).(bool); typed {
		return true, nil
	}
	typed, err := s.authEnforcement.Enforced(ctx, organizationID)
	if err != nil {
		return false, fmt.Errorf("read upstream auth enforcement: %w", err)
	}
	return typed, nil
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
	offers, err := s.configuredAuthOffers(ctx, organizationID, ids)
	if err != nil {
		return nil, fmt.Errorf("list consent auth clients: %w", err)
	}
	unavailable := make(map[string]bool)
	for _, card := range cards {
		unavailable[card.ClientID] = cardStatus(card) == gatewayServerUnavailable
	}
	remembered := map[uuid.UUID]uuid.UUID{}
	rememberedRows, err := queries.ListRememberedAuthClients(ctx, upstreamclientsrepo.ListRememberedAuthClientsParams{SubjectUrn: subject.String(), OrganizationID: organizationID, McpServerIds: ids})
	if err != nil {
		return nil, fmt.Errorf("list remembered auth clients: %w", err)
	}
	for _, row := range rememberedRows {
		if row.AuthClientID.Valid {
			remembered[row.McpServerID] = row.AuthClientID.UUID
		}
	}

	// A personal connection or identity-provider sign-in belongs to a person;
	// a caller representing nobody cannot use one.
	represented := subject.Kind == urn.SessionSubjectKindUser || subject.Kind == urn.SessionSubjectKindAgent

	choices := make(map[uuid.UUID]*consentServerAuth, len(servers))
	for _, server := range servers {
		offer, known := offers[server.id]
		auth := &consentServerAuth{ServerID: server.id, Status: consentAuthChoose, Options: nil}
		choices[server.id] = auth

		offered := offer.clients
		switch {
		case len(offered) > 0:
		case !known || offer.pending:
			// Upstream credentials no auth client records yet are incomplete
			// setup, never a server needing none.
			auth.Status = consentAuthSetupIncomplete
			continue
		default:
			// A server offering no enabled client needs no upstream
			// authentication.
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
			// Browser consent can only preview choices that exact serving accepts.
			// Browserless callers apply their own serving policy.
			if !deferPersonal {
				res = enforcedCompatibility(res)
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
				unavailable:     row.RemoteSessionClientID.Valid && unavailable[row.RemoteSessionClientID.UUID.String()],
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
		if status == gatewayServerUnavailable && card.reconnectsDespiteOutage() {
			status = gatewayServerReconnect
		}
		// A broken provider address needs administrator repair, not approval
		// or another attempt by the person. Ordinary outages remain eligible
		// while the connection itself is valid.
		if card.SignInAddress {
			return consentClientSetup, ""
		}
		if clientType == authz.AuthClientTypeEMA {
			switch status {
			case gatewayServerChained, gatewayServerChainPending, gatewayServerUnavailable:
				return consentClientReady, ""
			case gatewayServerReconnect:
				return consentClientReconnect, card.ClientID
			case gatewayServerNotConnected:
				return consentClientConnect, card.ClientID
			case gatewayServerConnected, gatewayServerNoSignIn, gatewayServerNotSetUp, gatewayServerNoAccess:
			}
			return consentClientSetup, ""
		}
		switch status {
		case gatewayServerConnected, gatewayServerChained, gatewayServerChainPending, gatewayServerUnavailable:
			// An outage leaves the connection in place: it can still be chosen.
			return consentClientReady, ""
		case gatewayServerReconnect:
			return consentClientReconnect, card.ClientID
		case gatewayServerNotConnected, gatewayServerNoSignIn, gatewayServerNotSetUp, gatewayServerNoAccess:
		}
		return consentClientConnect, card.ClientID
	}
	return consentClientSetup, ""
}

// selectConsentAuthClient marks the option the credential will record. A pick
// or a remembered choice applies only while it is still usable; otherwise
// healthy options come before outages. Within each group, the fixed order
// is identity provider, then shared clients by name,
// then the person's own account, then the app's own credential.
func selectConsentAuthClient(auth *consentServerAuth, picked, remembered uuid.UUID) {
	slices.SortStableFunc(auth.Options, func(a, b consentAuthClientOption) int {
		return cmp.Or(cmp.Compare(preselectionRank(a.Type), preselectionRank(b.Type)), cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID.String(), b.ID.String()))
	})
	selectOption := func(option *consentAuthClientOption) {
		// Sharing a registration does not make two typed offers equivalent.
		// A remembered choice may prefill other reviews, but it is not an
		// explicit pick for this review when both offers are usable.
		if option.ID != picked && option.registrationID.Valid && slices.ContainsFunc(auth.Options, func(other consentAuthClientOption) bool {
			return other.ID != option.ID && other.Usable() && other.registrationID == option.registrationID
		}) {
			return
		}
		option.Selected = true
	}

	for _, want := range []uuid.UUID{picked, remembered} {
		if want == uuid.Nil {
			continue
		}
		for i := range auth.Options {
			if auth.Options[i].ID == want && auth.Options[i].Usable() {
				selectOption(&auth.Options[i])
				return
			}
		}
	}
	for _, unavailable := range []bool{false, true} {
		for i := range auth.Options {
			if auth.Options[i].Usable() && auth.Options[i].unavailable == unavailable {
				selectOption(&auth.Options[i])
				return
			}
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
// is nil for gateways, hosted servers, first-party pages, and challenges
// without a subject.
func (s *Service) directConsentAuthChoice(ctx context.Context, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState, cards []remoteSessionCard, picked uuid.UUID) (*consentServerAuth, error) {
	if challengeState.FirstParty || challengeState.Subject == nil || !endpoint.McpServerID.Valid || endpoint.ToolsetID.Valid || endpoint.MetaMcpServerID.Valid {
		return nil, nil
	}
	// Permission is judged for the consenting person, as their requests will
	// be.
	subjectCtx, err := s.contextForSessionSubject(ctx, endpoint, *challengeState.Subject, "consent:"+challengeState.ID, challengeState.ClientID)
	if err != nil {
		return nil, fmt.Errorf("stamp consent subject context: %w", err)
	}
	serverID := endpoint.McpServerID.UUID
	choices, err := s.consentAuthChoices(withTypedConsentAuth(withMandatoryAdmission(subjectCtx)), endpoint.OrganizationID, *challengeState.Subject, []consentAuthServer{{id: serverID, projectID: endpoint.ProjectID}}, cards, map[uuid.UUID]uuid.UUID{serverID: picked}, false)
	if err != nil {
		return nil, err
	}
	return choices[serverID], nil
}

// errConsentNoUsableConnection refuses an approval no connection could serve.
var errConsentNoUsableConnection = errors.New("no connection can serve this approval")

// consentCardsApprovable is the rule for approving through service cards.
func consentCardsApprovable(cards []remoteSessionCard) bool {
	return len(cards) == 0 || slices.ContainsFunc(cards, func(card remoteSessionCard) bool {
		return (card.Connected && !card.connectionDown()) || card.Chained
	})
}

// consentDirectAuthClientPick validates the credential posted with a direct
// server's approval against the choices computed again now, and returns it as
// the grant's typed pick and its registration. Hosted endpoints retain the
// card-based rule; proxied servers never infer a credential from their cards.
func (s *Service) consentDirectAuthClientPick(ctx context.Context, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState, posted string, upstreamPicks map[uuid.UUID]uuid.UUID, cards []remoteSessionCard) (map[uuid.UUID]uuid.UUID, error) {
	picked := uuid.Nil
	if posted != "" {
		parsed, err := uuid.Parse(posted)
		if err != nil || parsed == uuid.Nil {
			return nil, errConsentConfigurationChanged
		}
		picked = parsed
	}
	auth, err := s.directConsentAuthChoice(ctx, endpoint, challengeState, cards, picked)
	if err != nil {
		return nil, err
	}
	if auth == nil {
		// An agent's attached connection is stored under its owner, so its own cards can't decide.
		agent := challengeState.Subject != nil && challengeState.Subject.Kind == urn.SessionSubjectKindAgent
		if !endpoint.MetaMcpServerID.Valid && !agent && !consentCardsApprovable(cards) {
			return nil, errConsentNoUsableConnection
		}
		return nil, nil
	}
	if !auth.Approvable() {
		if picked == uuid.Nil && auth.Status == consentAuthChoose {
			return nil, errConsentNoUsableConnection
		}
		return nil, errConsentConfigurationChanged
	}
	selected := auth.Selected()
	if selected == nil {
		if picked != uuid.Nil {
			return nil, errConsentConfigurationChanged
		}
		resolved, err := s.resolveNoAuthChoice(withTypedConsentAuth(ctx), endpoint.OrganizationID, endpoint.McpServerID.UUID)
		if err != nil {
			return nil, err
		}
		if enforcedCompatibility(resolved).outcome != authClientNoAuth {
			return nil, errConsentNoUsableConnection
		}
		// No upstream authentication: no registration is recorded.
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
func (s *Service) gatewayConsentAuthChoices(ctx context.Context, r *http.Request, endpoint *ResolvedMcpEndpoint, subject urn.SessionSubject, members []metaMember, cards []remoteSessionCard) (map[uuid.UUID]*consentServerAuth, error) {
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
	return s.consentAuthChoices(withTypedConsentAuth(ctx), endpoint.OrganizationID, subject, servers, cards, picked, false)
}

func (s *Service) resolveMemberAuthChoice(ctx context.Context, endpoint *ResolvedMcpEndpoint, member metaMember, auth *consentServerAuth, subject urn.SessionSubject) (authClientResolution, error) {
	switch {
	case auth == nil:
		return gapResolution(authClientReasonMissingChoice), nil
	case auth.Status == consentAuthNoAuth:
		resolved, err := s.resolveNoAuthChoice(withTypedConsentAuth(ctx), endpoint.OrganizationID, member.serverID)
		return enforcedCompatibility(resolved), err
	case auth.Status == consentAuthNoAccess:
		res := gapResolution(authClientReasonNotPermitted)
		res.outcome = authClientNoAccess
		res.serverName = member.name
		return res, nil
	}
	if selected := auth.Selected(); selected != nil {
		resolved, err := s.resolveAuthClientChoiceForConsent(withTypedConsentAuth(ctx), endpoint.OrganizationID, member.serverID, member.projectID, selected.ID, subject)
		return enforcedCompatibility(resolved), err
	}
	return gapResolution(authClientReasonSetup), nil
}
