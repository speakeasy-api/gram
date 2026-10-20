package demoseed

import (
	"context"
	"fmt"
	"strconv"

	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
)

// serversUserSession is one live user session row; offsets are Postgres
// interval strings applied to now().
type serversUserSession struct {
	n                int
	issuer, client   string
	user             int
	refreshExpiresIn string
	expiresIn        string
	lastUsedAgo      string
	createdAgo       string
}

// serversValidatedSession is one upstream session on Linear's CIMD client.
// status "" is the never-validated session (NULL validation_status).
type serversValidatedSession struct {
	n               int32
	status          string
	grantGeneration int64
}

// serversKillswitchOperation is one completed killswitch operation receipt.
type serversKillswitchOperation struct {
	op           string
	prescription string
	operation    string
	hashDigit    string
	version      int32
	state        string
	expiresIn    string
	createdAgo   string
}

// seedServers: MCP servers, gateway endpoints, plugins, meta servers, remote sessions, killswitch.
func (s *seeder) seedServers(ctx context.Context) error {
	org := s.orgID()
	proj := s.projectID()
	user := func(k int) string { return s.userID(demoUsers[k-1].handle) }

	if err := s.q.ServersInsertRemoteMCPServers(ctx, repo.ServersInsertRemoteMCPServersParams{
		LinearID:  s.det("remotemcp-linear"),
		ProjectID: proj,
		SlackID:   s.det("remotemcp-slack"),
		GithubID:  s.det("remotemcp-github"),
	}); err != nil {
		return fmt.Errorf("insert demo remote mcp servers: %w", err)
	}

	if err := s.q.ServersInsertUserSessionIssuers(ctx, repo.ServersInsertUserSessionIssuersParams{
		WorkforceID: s.det("issuer-workforce"),
		OrgID:       org,
		LinearID:    s.det("issuer-linear"),
		ProjectID:   proj,
		SlackID:     s.det("issuer-slack"),
		GatewayID:   s.det("issuer-gateway"),
	}); err != nil {
		return fmt.Errorf("insert demo user session issuers: %w", err)
	}

	if err := s.q.ServersInsertMCPServers(ctx, repo.ServersInsertMCPServersParams{
		SupportID:         s.det("mcpserver-support"),
		ProjectID:         proj,
		Toolset1:          s.toolset1(),
		OpsID:             s.det("mcpserver-ops"),
		Toolset2:          s.toolset2(),
		LinearID:          s.det("mcpserver-linear"),
		RemoteLinearID:    s.det("remotemcp-linear"),
		IssuerLinearID:    s.det("issuer-linear"),
		SlackID:           s.det("mcpserver-slack"),
		RemoteSlackID:     s.det("remotemcp-slack"),
		IssuerSlackID:     s.det("issuer-slack"),
		GithubID:          s.det("mcpserver-github"),
		RemoteGithubID:    s.det("remotemcp-github"),
		IssuerWorkforceID: s.det("issuer-workforce"),
	}); err != nil {
		return fmt.Errorf("insert demo mcp servers: %w", err)
	}

	if err := s.q.ServersInsertHostedToolsetMCPServers(ctx, proj); err != nil {
		return fmt.Errorf("insert demo hosted toolset mcp servers: %w", err)
	}
	if err := s.q.ServersInsertHostedToolsetEndpoints(ctx, repo.ServersInsertHostedToolsetEndpointsParams{
		EndpointNamePrefix: s.name("endpoint-hosted-"),
		ProjectID:          proj,
	}); err != nil {
		return fmt.Errorf("insert demo hosted toolset endpoints: %w", err)
	}

	if err := s.q.ServersInsertGitHubContractorsBlock(ctx, repo.ServersInsertGitHubContractorsBlockParams{
		ID:             s.det("github-contractors-block"),
		OrgID:          org,
		GithubServerID: s.det("mcpserver-github"),
	}); err != nil {
		return fmt.Errorf("insert demo github contractors block: %w", err)
	}
	if err := s.q.ServersInsertSupportDeskGrants(ctx, repo.ServersInsertSupportDeskGrantsParams{
		OrgID:         org,
		Toolset1:      s.toolset1(),
		SlackServerID: s.det("mcpserver-slack"),
	}); err != nil {
		return fmt.Errorf("insert demo support desk grants: %w", err)
	}
	if err := s.q.ServersInsertGitHubUserGrants(ctx, repo.ServersInsertGitHubUserGrantsParams{
		DirectID:       s.det("github-direct-grant"),
		OrgID:          org,
		DirectUserID:   user(3),
		GithubServerID: s.det("mcpserver-github"),
		WildcardID:     s.det("github-wildcard-grant"),
		WildcardUserID: user(4),
	}); err != nil {
		return fmt.Errorf("insert demo github user grants: %w", err)
	}

	n, err := s.q.ServersInsertRoleServerMemberships(ctx, repo.ServersInsertRoleServerMembershipsParams{
		OrgID:     org,
		ProjectID: proj,
	})
	if err != nil {
		return fmt.Errorf("insert demo role server memberships: %w", err)
	}
	if n != 25 {
		return fmt.Errorf("demo seed: expected 25 role server memberships, found %d", n)
	}

	pluginID, err := s.q.ServersInsertEngineeringEssentialsPlugin(ctx, repo.ServersInsertEngineeringEssentialsPluginParams{
		OrgID:     org,
		ProjectID: proj,
	})
	if err != nil {
		return fmt.Errorf("insert demo engineering essentials plugin: %w", err)
	}
	if err := s.q.ServersInsertEngineeringEssentialsAssignment(ctx, repo.ServersInsertEngineeringEssentialsAssignmentParams{
		PluginID: pluginID,
		OrgID:    org,
	}); err != nil {
		return fmt.Errorf("insert demo engineering essentials assignment: %w", err)
	}
	n, err = s.q.ServersInsertEngineeringEssentialsServers(ctx, repo.ServersInsertEngineeringEssentialsServersParams{
		PluginID:  pluginID,
		ProjectID: proj,
	})
	if err != nil {
		return fmt.Errorf("insert demo engineering essentials servers: %w", err)
	}
	if n != 1 {
		return fmt.Errorf("demo seed: expected 1 supplemental server membership, found %d", n)
	}

	if err := s.q.ServersInsertLinearRemoteSessionIssuer(ctx, repo.ServersInsertLinearRemoteSessionIssuerParams{
		ID:        s.det("remote-identity-provider-linear"),
		ProjectID: proj,
		OrgID:     org,
	}); err != nil {
		return fmt.Errorf("insert demo linear remote session issuer: %w", err)
	}
	if err := s.q.ServersInsertLinearRemoteSessionClient(ctx, repo.ServersInsertLinearRemoteSessionClientParams{
		ID:                    s.det("remote-identity-client-linear"),
		ProjectID:             proj,
		OrgID:                 org,
		RemoteSessionIssuerID: s.det("remote-identity-provider-linear"),
		ClientID:              "https://clients.example.com/" + s.name("linear") + ".json",
	}); err != nil {
		return fmt.Errorf("insert demo linear remote session client: %w", err)
	}
	if err := s.q.ServersInsertRemoteSessionClientIssuerBinding(ctx, repo.ServersInsertRemoteSessionClientIssuerBindingParams{
		RemoteSessionClientID: s.det("remote-identity-client-linear"),
		UserSessionIssuerID:   s.det("issuer-linear"),
	}); err != nil {
		return fmt.Errorf("bind demo linear remote session client: %w", err)
	}
	if err := s.q.ServersStampLinearRemoteSessionIssuer(ctx, repo.ServersStampLinearRemoteSessionIssuerParams{
		RemoteSessionIssuerID: s.det("remote-identity-provider-linear"),
		McpServerID:           s.det("mcpserver-linear"),
		ProjectID:             proj,
	}); err != nil {
		return fmt.Errorf("stamp demo linear remote session issuer: %w", err)
	}
	if err := s.q.ServersInsertServerClientAssociation(ctx, repo.ServersInsertServerClientAssociationParams{
		ID:                    s.det("server-client-association-linear"),
		ProjectID:             proj,
		OrgID:                 org,
		McpServerID:           s.det("mcpserver-linear"),
		RemoteSessionClientID: s.det("remote-identity-client-linear"),
	}); err != nil {
		return fmt.Errorf("associate demo linear server with its client: %w", err)
	}
	if err := s.q.ServersUpsertLinearProtectedResource(ctx, repo.ServersUpsertLinearProtectedResourceParams{
		ID:        s.det("protected-resource-linear"),
		ProjectID: proj,
		OrgID:     org,
	}); err != nil {
		return fmt.Errorf("upsert demo linear protected resource: %w", err)
	}
	if err := s.q.ServersInsertSlackIdentityHeader(ctx, repo.ServersInsertSlackIdentityHeaderParams{
		ID:                s.det("agent-identity-header-slack"),
		RemoteMcpServerID: s.det("remotemcp-slack"),
	}); err != nil {
		return fmt.Errorf("insert demo slack identity header: %w", err)
	}

	if err := s.q.ServersInsertMetaMCPServer(ctx, repo.ServersInsertMetaMCPServerParams{
		ID:                  s.det("metamcp-1"),
		OrgID:               org,
		ProjectID:           proj,
		UserSessionIssuerID: s.det("issuer-gateway"),
	}); err != nil {
		return fmt.Errorf("insert demo meta mcp server: %w", err)
	}
	if err := s.q.ServersInsertMetaMCPServerMembers(ctx, repo.ServersInsertMetaMCPServerMembersParams{
		SupportMemberID: s.det("metamember-support"),
		ProjectID:       proj,
		MetaID:          s.det("metamcp-1"),
		SupportServerID: s.det("mcpserver-support"),
		OpsMemberID:     s.det("metamember-ops"),
		OpsServerID:     s.det("mcpserver-ops"),
		LinearMemberID:  s.det("metamember-linear"),
		LinearServerID:  s.det("mcpserver-linear"),
		SlackMemberID:   s.det("metamember-slack"),
		SlackServerID:   s.det("mcpserver-slack"),
	}); err != nil {
		return fmt.Errorf("insert demo meta mcp server members: %w", err)
	}
	if err := s.q.ServersInsertRemovedGitHubMember(ctx, repo.ServersInsertRemovedGitHubMemberParams{
		ID:             s.det("metamember-github"),
		ProjectID:      proj,
		MetaID:         s.det("metamcp-1"),
		GithubServerID: s.det("mcpserver-github"),
	}); err != nil {
		return fmt.Errorf("insert demo removed github meta member: %w", err)
	}
	if err := s.q.ServersInsertGatewayEndpoints(ctx, repo.ServersInsertGatewayEndpointsParams{
		GatewayEndpointID: s.det("endpoint-gateway"),
		ProjectID:         proj,
		MetaID:            s.det("metamcp-1"),
		GatewaySlug:       s.spec.OrgSlug + "-gateway",
		LinearEndpointID:  s.det("endpoint-linear"),
		LinearServerID:    s.det("mcpserver-linear"),
		LinearSlug:        s.spec.OrgSlug + "-linear",
		SlackEndpointID:   s.det("endpoint-slack"),
		SlackServerID:     s.det("mcpserver-slack"),
		SlackSlug:         s.spec.OrgSlug + "-slack",
	}); err != nil {
		return fmt.Errorf("insert demo gateway endpoints: %w", err)
	}
	if err := s.seedServersEMA(ctx); err != nil {
		return err
	}

	if err := s.q.ServersInsertUserSessionClients(ctx, repo.ServersInsertUserSessionClientsParams{
		LinearClientID:  s.det("usc-linear"),
		ProjectID:       proj,
		OrgID:           org,
		LinearIssuerID:  s.det("issuer-linear"),
		SlackClientID:   s.det("usc-slack"),
		SlackIssuerID:   s.det("issuer-slack"),
		GatewayClientID: s.det("usc-gateway"),
		GatewayIssuerID: s.det("issuer-gateway"),
	}); err != nil {
		return fmt.Errorf("insert demo user session clients: %w", err)
	}

	userSessions := []serversUserSession{
		{n: 6, issuer: "linear", client: "linear", user: 1, refreshExpiresIn: "11 days", expiresIn: "6 hours", lastUsedAgo: "40 minutes", createdAgo: "10 days"},
		{n: 7, issuer: "linear", client: "linear", user: 3, refreshExpiresIn: "9 days", expiresIn: "4 hours", lastUsedAgo: "3 hours", createdAgo: "9 days"},
		{n: 8, issuer: "slack", client: "slack", user: 2, refreshExpiresIn: "12 days", expiresIn: "9 hours", lastUsedAgo: "1 hour", createdAgo: "8 days"},
		{n: 9, issuer: "slack", client: "slack", user: 5, refreshExpiresIn: "10 days", expiresIn: "2 hours", lastUsedAgo: "20 hours", createdAgo: "7 days"},
		// The engineering manager reaches everything through the gateway, which
		// is the whole point of a meta server; without this he was the one person
		// with no connection at all.
		{n: 10, issuer: "gateway", client: "gateway", user: 6, refreshExpiresIn: "13 days", expiresIn: "7 hours", lastUsedAgo: "2 hours", createdAgo: "13 days"},
		{n: 11, issuer: "gateway", client: "gateway", user: 4, refreshExpiresIn: "5 days", expiresIn: "1 hour", lastUsedAgo: "5 hours", createdAgo: "12 days"},
	}
	for _, us := range userSessions {
		num := strconv.Itoa(us.n)
		if err := s.q.ServersInsertUserSession(ctx, repo.ServersInsertUserSessionParams{
			ID:                  s.det("user-session-" + num),
			ProjectID:           proj,
			OrgID:               org,
			UserSessionIssuerID: s.det("issuer-" + us.issuer),
			UserSessionClientID: s.det("usc-" + us.client),
			UserID:              user(us.user),
			Jti:                 "demo-jti-" + num,
			RefreshTokenHash:    s.det("user-session-refresh-" + num).String(),
			RefreshExpiresIn:    us.refreshExpiresIn,
			ExpiresIn:           us.expiresIn,
			LastUsedAgo:         us.lastUsedAgo,
			CreatedAgo:          us.createdAgo,
		}); err != nil {
			return fmt.Errorf("insert demo user session %d: %w", us.n, err)
		}
	}

	if err := s.q.ServersInsertAttachmentClient(ctx, repo.ServersInsertAttachmentClientParams{
		ID:                    s.det("attachment-client"),
		ProjectID:             proj,
		OrgID:                 org,
		RemoteSessionIssuerID: s.det("remote-identity-provider-linear"),
	}); err != nil {
		return fmt.Errorf("insert demo attachment client: %w", err)
	}
	if err := s.q.ServersInsertRemoteSessionClientIssuerBinding(ctx, repo.ServersInsertRemoteSessionClientIssuerBindingParams{
		RemoteSessionClientID: s.det("attachment-client"),
		UserSessionIssuerID:   s.det("issuer-linear"),
	}); err != nil {
		return fmt.Errorf("bind demo attachment client: %w", err)
	}
	// The Linear server is its issuer's only consumer, so every client bound
	// to the issuer is associated with it, as the app's reconcile would do.
	if err := s.q.ServersInsertServerClientAssociation(ctx, repo.ServersInsertServerClientAssociationParams{
		ID:                    s.det("server-client-association-attachment"),
		ProjectID:             proj,
		OrgID:                 org,
		McpServerID:           s.det("mcpserver-linear"),
		RemoteSessionClientID: s.det("attachment-client"),
	}); err != nil {
		return fmt.Errorf("associate demo linear server with the attachment client: %w", err)
	}
	if err := s.q.ServersInsertAttachmentSession(ctx, repo.ServersInsertAttachmentSessionParams{
		ID:                    s.det("attachment-session"),
		UserID:                user(1),
		UserSessionIssuerID:   s.det("issuer-linear"),
		RemoteSessionClientID: s.det("attachment-client"),
	}); err != nil {
		return fmt.Errorf("insert demo attachment session: %w", err)
	}

	validated := []serversValidatedSession{
		{n: 1, status: "valid", grantGeneration: 1},
		{n: 2, status: "valid", grantGeneration: 3},
		{n: 3, status: "rejected_by_member", grantGeneration: 2},
		{n: 4, status: "inactive", grantGeneration: 1},
		{n: 5, status: "unknown", grantGeneration: 1},
		{n: 6, status: "", grantGeneration: 1},
	}
	for _, v := range validated {
		if err := s.q.ServersInsertValidatedSession(ctx, repo.ServersInsertValidatedSessionParams{
			ID:                    s.det("validated-session-" + strconv.Itoa(int(v.n))),
			UserID:                user(int(v.n)),
			UserSessionIssuerID:   s.det("issuer-linear"),
			RemoteSessionClientID: s.det("remote-identity-client-linear"),
			GrantGeneration:       v.grantGeneration,
			Status:                v.status,
			HoursAgo:              v.n,
			DaysAgo:               v.n + 2,
		}); err != nil {
			return fmt.Errorf("insert demo validated remote session %d: %w", v.n, err)
		}
	}

	// The two managed release agents (1 and 4) both reference the attachment
	// session, and each gets an exact Linear connect grant.
	for _, agent := range []int{1, 4} {
		num := strconv.Itoa(agent)
		if err := s.q.ServersInsertAttachmentBinding(ctx, repo.ServersInsertAttachmentBindingParams{
			ID:                    s.det("attachment-binding-" + num),
			ProjectID:             proj,
			OrgID:                 org,
			PrincipalID:           s.det("managed-agent-" + num),
			UserSessionIssuerID:   s.det("issuer-linear"),
			RemoteSessionClientID: s.det("attachment-client"),
			AttachedByUserID:      user(1),
			RemoteSessionID:       s.det("attachment-session"),
		}); err != nil {
			return fmt.Errorf("insert demo attachment binding %d: %w", agent, err)
		}
	}
	for _, agent := range []int{1, 4} {
		num := strconv.Itoa(agent)
		if err := s.q.ServersInsertAgentConnectGrant(ctx, repo.ServersInsertAgentConnectGrantParams{
			ID:          s.det("agent-connect-grant-" + num),
			OrgID:       org,
			AgentID:     s.det("managed-agent-" + num),
			McpServerID: s.det("mcpserver-linear"),
			ProjectID:   proj,
		}); err != nil {
			return fmt.Errorf("insert demo agent connect grant %d: %w", agent, err)
		}
	}

	if err := s.q.ServersInsertKillswitchPrescriptions(ctx, repo.ServersInsertKillswitchPrescriptionsParams{
		ActiveSelectedID: s.det("killswitch-active-selected"),
		OrgID:            org,
		User1:            user(1),
		ActiveAllID:      s.det("killswitch-active-all"),
		ScheduledAllID:   s.det("killswitch-scheduled-all"),
		User3:            user(3),
		ChangedID:        s.det("killswitch-changed"),
		User2:            user(2),
		LiftedID:         s.det("killswitch-lifted"),
		User4:            user(4),
		ExpiredID:        s.det("killswitch-expired"),
		User5:            user(5),
	}); err != nil {
		return fmt.Errorf("insert demo killswitch prescriptions: %w", err)
	}
	if err := s.q.ServersInsertKillswitchPrescriptionVersions(ctx, repo.ServersInsertKillswitchPrescriptionVersionsParams{
		OrgID:            org,
		ActiveSelectedID: s.det("killswitch-active-selected"),
		ActiveAllID:      s.det("killswitch-active-all"),
		ScheduledAllID:   s.det("killswitch-scheduled-all"),
		ChangedID:        s.det("killswitch-changed"),
		LiftedID:         s.det("killswitch-lifted"),
		ExpiredID:        s.det("killswitch-expired"),
	}); err != nil {
		return fmt.Errorf("insert demo killswitch prescription versions: %w", err)
	}
	if err := s.q.ServersInsertKillswitchVersionResources(ctx, repo.ServersInsertKillswitchVersionResourcesParams{
		OrgID:            org,
		ActiveSelectedID: s.det("killswitch-active-selected"),
		SupportServerID:  s.det("mcpserver-support"),
		ChangedID:        s.det("killswitch-changed"),
		OpsServerID:      s.det("mcpserver-ops"),
		LinearServerID:   s.det("mcpserver-linear"),
		LiftedID:         s.det("killswitch-lifted"),
		SlackServerID:    s.det("mcpserver-slack"),
		ExpiredID:        s.det("killswitch-expired"),
	}); err != nil {
		return fmt.Errorf("insert demo killswitch version resources: %w", err)
	}
	if err := s.q.ServersInsertKillswitchExpiryEvent(ctx, repo.ServersInsertKillswitchExpiryEventParams{
		OrgID:          org,
		PrescriptionID: s.det("killswitch-expired"),
	}); err != nil {
		return fmt.Errorf("insert demo killswitch expiry event: %w", err)
	}

	operations := []serversKillswitchOperation{
		{op: "active-selected-v1", prescription: "active-selected", operation: "activate", hashDigit: "1", version: 1, state: "active", expiresIn: "22 days", createdAgo: "8 days"},
		{op: "active-all-v1", prescription: "active-all", operation: "activate", hashDigit: "2", version: 1, state: "active", expiresIn: "23 days", createdAgo: "7 days"},
		{op: "scheduled-all-v1", prescription: "scheduled-all", operation: "activate", hashDigit: "3", version: 1, state: "active", expiresIn: "29 days 23 hours", createdAgo: "1 hour"},
		{op: "changed-v1", prescription: "changed", operation: "activate", hashDigit: "4", version: 1, state: "active", expiresIn: "24 days", createdAgo: "6 days"},
		{op: "changed-v2", prescription: "changed", operation: "change", hashDigit: "5", version: 2, state: "active", expiresIn: "28 days", createdAgo: "2 days"},
		{op: "lifted-v1", prescription: "lifted", operation: "activate", hashDigit: "6", version: 1, state: "active", expiresIn: "25 days", createdAgo: "5 days"},
		{op: "lifted-v2", prescription: "lifted", operation: "deactivate", hashDigit: "7", version: 2, state: "inactive", expiresIn: "29 days 12 hours", createdAgo: "12 hours"},
		{op: "expired-v1", prescription: "expired", operation: "activate", hashDigit: "8", version: 1, state: "active", expiresIn: "26 days", createdAgo: "4 days"},
	}
	for _, o := range operations {
		if err := s.q.ServersInsertKillswitchOperation(ctx, repo.ServersInsertKillswitchOperationParams{
			OrgID:               org,
			OperationID:         s.det("killswitch-operation-" + o.op),
			ActorUserID:         user(6),
			Operation:           o.operation,
			HashDigit:           o.hashDigit,
			PrescriptionID:      s.det("killswitch-" + o.prescription),
			PrescriptionVersion: o.version,
			State:               o.state,
			ExpiresIn:           o.expiresIn,
			CreatedAgo:          o.createdAgo,
		}); err != nil {
			return fmt.Errorf("insert demo killswitch operation %s: %w", o.op, err)
		}
	}

	return nil
}

// seedServersEMA: Enterprise Managed Authorization. Acme Identity is a
// fictional identity provider for two fictional services behind workforce
// servers; the workforce sign-in deliberately does not trust it, so demo
// sign-in is unaffected. Neither service has accepted a provider yet: a
// seeded acceptance needs a trusted issuer session, which the hourly
// delegation cleanup erases unless an organization sign-in trusts the
// issuer. Runs after role distribution, so plugins skip these servers.
func (s *seeder) seedServersEMA(ctx context.Context) error {
	org := s.orgID()
	proj := s.projectID()
	workforce := s.det("issuer-workforce")

	if err := s.q.ServersInsertEMAIdentityProvider(ctx, repo.ServersInsertEMAIdentityProviderParams{
		ID:    s.det("ema-idp"),
		OrgID: org,
	}); err != nil {
		return fmt.Errorf("insert demo ema identity provider: %w", err)
	}
	if err := s.q.ServersInsertEMAIdentityProviderClient(ctx, repo.ServersInsertEMAIdentityProviderClientParams{
		ID:                    s.det("ema-idp-client"),
		OrgID:                 org,
		RemoteSessionIssuerID: s.det("ema-idp"),
	}); err != nil {
		return fmt.Errorf("insert demo ema identity provider client: %w", err)
	}
	if err := s.q.ServersInsertEMARemoteMCPServers(ctx, repo.ServersInsertEMARemoteMCPServersParams{
		TrackerID: s.det("remotemcp-ema-tracker"),
		ProjectID: proj,
		WikiID:    s.det("remotemcp-ema-wiki"),
	}); err != nil {
		return fmt.Errorf("insert demo ema remote mcp servers: %w", err)
	}
	if err := s.q.ServersInsertEMAMCPServers(ctx, repo.ServersInsertEMAMCPServersParams{
		TrackerID:         s.det("mcpserver-ema-tracker"),
		ProjectID:         proj,
		RemoteTrackerID:   s.det("remotemcp-ema-tracker"),
		IssuerWorkforceID: workforce,
		WikiID:            s.det("mcpserver-ema-wiki"),
		RemoteWikiID:      s.det("remotemcp-ema-wiki"),
	}); err != nil {
		return fmt.Errorf("insert demo ema mcp servers: %w", err)
	}
	if err := s.q.ServersInsertEMAVendorIssuers(ctx, repo.ServersInsertEMAVendorIssuersParams{
		TrackerID: s.det("ema-vendor-tracker"),
		ProjectID: proj,
		OrgID:     org,
		WikiID:    s.det("ema-vendor-wiki"),
	}); err != nil {
		return fmt.Errorf("insert demo ema vendor issuers: %w", err)
	}
	if err := s.q.ServersInsertEMAVendorClients(ctx, repo.ServersInsertEMAVendorClientsParams{
		TrackerID:       s.det("ema-client-tracker"),
		ProjectID:       proj,
		OrgID:           org,
		TrackerIssuerID: s.det("ema-vendor-tracker"),
		WikiID:          s.det("ema-client-wiki"),
		WikiIssuerID:    s.det("ema-vendor-wiki"),
	}); err != nil {
		return fmt.Errorf("insert demo ema vendor clients: %w", err)
	}
	for _, client := range []string{"ema-client-tracker", "ema-client-wiki"} {
		if err := s.q.ServersInsertRemoteSessionClientIssuerBinding(ctx, repo.ServersInsertRemoteSessionClientIssuerBindingParams{
			RemoteSessionClientID: s.det(client),
			UserSessionIssuerID:   workforce,
		}); err != nil {
			return fmt.Errorf("bind demo %s: %w", client, err)
		}
	}
	// The workforce issuer is shared, so each service's server is associated
	// with the one client of the authorization server its protected resource
	// names. GitHub, on the same issuer, uses neither client.
	for _, service := range []string{"tracker", "wiki"} {
		if err := s.q.ServersInsertServerClientAssociation(ctx, repo.ServersInsertServerClientAssociationParams{
			ID:                    s.det("server-client-association-ema-" + service),
			ProjectID:             proj,
			OrgID:                 org,
			McpServerID:           s.det("mcpserver-ema-" + service),
			RemoteSessionClientID: s.det("ema-client-" + service),
		}); err != nil {
			return fmt.Errorf("associate demo ema %s server with its client: %w", service, err)
		}
	}
	if err := s.q.ServersInsertEMAProtectedResources(ctx, repo.ServersInsertEMAProtectedResourcesParams{
		TrackerID: s.det("protected-resource-ema-tracker"),
		ProjectID: proj,
		OrgID:     org,
		WikiID:    s.det("protected-resource-ema-wiki"),
	}); err != nil {
		return fmt.Errorf("insert demo ema protected resources: %w", err)
	}
	if err := s.q.ServersInsertEMABindings(ctx, repo.ServersInsertEMABindingsParams{
		TrackerID:         s.det("ema-binding-tracker"),
		ProjectID:         proj,
		OrgID:             org,
		IssuerWorkforceID: workforce,
		TrackerIssuerID:   s.det("ema-vendor-tracker"),
		TrackerClientID:   s.det("ema-client-tracker"),
		WikiID:            s.det("ema-binding-wiki"),
		WikiIssuerID:      s.det("ema-vendor-wiki"),
		WikiClientID:      s.det("ema-client-wiki"),
	}); err != nil {
		return fmt.Errorf("insert demo ema bindings: %w", err)
	}
	if err := s.q.ServersInsertEMAProviderOrder(ctx, repo.ServersInsertEMAProviderOrderParams{
		ID:                    s.det("ema-order-wiki"),
		OrgID:                 org,
		RemoteSessionIssuerID: s.det("ema-idp"),
	}); err != nil {
		return fmt.Errorf("insert demo ema provider order: %w", err)
	}
	return nil
}
