package demoseed

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
)

// seedDeployment: Deployment stack, toolsets, MCP connections, issuers, agents, assistants.
func (s *seeder) seedDeployment(ctx context.Context) error {
	if err := s.seedDeploymentStack(ctx); err != nil {
		return err
	}
	if err := s.seedDeploymentToolsets(ctx); err != nil {
		return err
	}
	if err := s.seedDeploymentConnections(ctx); err != nil {
		return err
	}
	if err := s.seedDeploymentWorkloads(ctx); err != nil {
		return err
	}
	return s.seedDeploymentAssistants(ctx)
}

// seedDeploymentStack writes one completed deployment with 8 HTTP tools whose
// URNs (tools:http:acme:*) match the ClickHouse telemetry, feeding the
// Deployments, Sources, MCP, Playground, and Catalog surfaces.
func (s *seeder) seedDeploymentStack(ctx context.Context) error {
	proj := s.projectID()

	if err := s.q.DeployInsertAsset(ctx, repo.DeployInsertAssetParams{
		ID:             s.assetID(),
		ProjectID:      proj,
		OrganizationID: s.orgID(),
		// The tenant's uuid prefix padded to a 64-char placeholder digest; the
		// runner points the row at the uploaded spec afterwards.
		Sha256: s.spec.UUIDPrefix + "00000000000000000000000000000000000000000000000000000001",
	}); err != nil {
		return fmt.Errorf("insert demo openapi asset: %w", err)
	}

	if err := s.q.DeployInsertDeployment(ctx, repo.DeployInsertDeploymentParams{
		ID:             s.deployID(),
		UserID:         s.userID("lucas"),
		ProjectID:      proj,
		OrganizationID: s.orgID(),
		IdempotencyKey: s.spec.Marker + "-deployment-1",
	}); err != nil {
		return fmt.Errorf("insert demo deployment: %w", err)
	}

	if err := s.q.DeployInsertDeploymentStatus(ctx, s.deployID()); err != nil {
		return fmt.Errorf("insert demo deployment status: %w", err)
	}

	if err := s.q.DeployInsertDeploymentLogs(ctx, repo.DeployInsertDeploymentLogsParams{
		DeploymentID: s.deployID(),
		ProjectID:    proj,
	}); err != nil {
		return fmt.Errorf("insert demo deployment logs: %w", err)
	}

	if err := s.q.DeployInsertOpenAPIAsset(ctx, repo.DeployInsertOpenAPIAssetParams{
		ID:           s.doaID(),
		DeploymentID: s.deployID(),
		AssetID:      s.assetID(),
	}); err != nil {
		return fmt.Errorf("insert demo deployment openapi asset: %w", err)
	}

	for _, name := range toolNames {
		method := "GET"
		if name == "process_refund" || name == "set_env" {
			method = "POST"
		}
		if err := s.q.DeployInsertHTTPTool(ctx, repo.DeployInsertHTTPToolParams{
			ToolUrn:             toolURN(name),
			ProjectID:           proj,
			DeploymentID:        s.deployID(),
			Openapiv3DocumentID: s.doaID(),
			Name:                name,
			Summary:             "Acme " + strings.ReplaceAll(name, "_", " "),
			Description:         "Calls the Acme internal API operation " + name + ".",
			HttpMethod:          method,
			Path:                "/" + strings.ReplaceAll(name, "_", "/"),
			ReadOnlyHint:        name != "process_refund",
			DestructiveHint:     name == "process_refund",
		}); err != nil {
			return fmt.Errorf("insert demo http tool %s: %w", name, err)
		}
	}
	return nil
}

func (s *seeder) seedDeploymentToolsets(ctx context.Context) error {
	proj := s.projectID()
	externalOAuthID := s.det("external-oauth-discovery")

	// Keep a metadata-based External OAuth row so the authentication page can
	// demonstrate the proactive recommendation without contacting a provider.
	if err := s.q.DeployInsertExternalOAuthMetadata(ctx, repo.DeployInsertExternalOAuthMetadataParams{
		ID:        externalOAuthID,
		ProjectID: proj,
	}); err != nil {
		return fmt.Errorf("insert demo external oauth metadata: %w", err)
	}

	if err := s.q.DeployInsertToolsets(ctx, repo.DeployInsertToolsetsParams{
		Toolset1:              s.toolset1(),
		OrganizationID:        s.orgID(),
		ProjectID:             proj,
		SupportMcpSlug:        s.spec.OrgSlug + "-support",
		Toolset2:              s.toolset2(),
		Toolset3:              s.toolset3(),
		PartnerMcpSlug:        s.spec.OrgSlug + "-partner",
		Toolset4:              s.toolset4(),
		DiscoveryMcpSlug:      s.spec.OrgSlug + "-oauth-discovery",
		ExternalOauthServerID: externalOAuthID,
	}); err != nil {
		return fmt.Errorf("insert demo toolsets: %w", err)
	}

	urns := func(idx ...int) []string {
		out := make([]string, 0, len(idx))
		for _, i := range idx {
			out = append(out, toolURN(toolNames[i]))
		}
		return out
	}
	if err := s.q.DeployInsertToolsetVersions(ctx, repo.DeployInsertToolsetVersionsParams{
		Toolset1:     s.toolset1(),
		Toolset1Urns: urns(0, 1, 2, 3, 4, 5, 6, 7),
		Toolset2:     s.toolset2(),
		Toolset2Urns: urns(0, 1, 4, 6, 7),
		Toolset3:     s.toolset3(),
		Toolset3Urns: urns(0, 2, 3),
		Toolset4:     s.toolset4(),
		Toolset4Urns: urns(0),
	}); err != nil {
		return fmt.Errorf("insert demo toolset versions: %w", err)
	}
	return nil
}

// deployUserSessionClient is one RFC 7591 registration against the partner
// gateway's issuer.
type deployUserSessionClient struct {
	id         uuid.UUID
	clientID   string
	secretHash pgtype.Text
	name       string
	redirect   string
	issuedAgo  string
	authMethod pgtype.Text
}

// deployUserSession is one person's connection through the partner gateway.
type deployUserSession struct {
	n              int
	client         uuid.UUID
	user           demoUser
	refreshExpires string
	expiresIn      string
	lastUsedAgo    string
	createdAgo     string
}

// seedDeploymentConnections writes the MCP connections: the issuer that gates
// the partner gateway, the agents registered against it, and the sessions they
// hold. Without these the Connections surfaces (the server's Clients and
// Sessions tab, the organization MCP Sessions page, and a person's connections
// on their employee page) render an empty state.
//
// The registrations deliberately span every credential kind the list can
// report, including one that cannot authenticate at all, so the badge and the
// detail sheet have something to show without a real agent connecting.
//
// No explicit deletes: user_session_issuers, user_session_clients, and
// user_sessions all cascade from projects, which is deleted and recreated
// earlier in the seed.
func (s *seeder) seedDeploymentConnections(ctx context.Context) error {
	proj := s.projectID()
	org := s.orgID()

	if err := s.q.DeployInsertUserSessionIssuer(ctx, repo.DeployInsertUserSessionIssuerParams{
		ID:             s.usIssuer(),
		ProjectID:      proj,
		OrganizationID: org,
	}); err != nil {
		return fmt.Errorf("insert demo user session issuer: %w", err)
	}

	if err := s.q.DeployAttachUserSessionIssuer(ctx, repo.DeployAttachUserSessionIssuerParams{
		UserSessionIssuerID: s.usIssuer(),
		ToolsetID:           s.toolset3(),
	}); err != nil {
		return fmt.Errorf("attach demo user session issuer: %w", err)
	}

	// Advertised capability and an administrator-declared registration are
	// separate evidence. No binding or session is created: neither proves
	// that a provisioned human can use this resource. Reserved example URLs
	// and a public client keep this fixture free of operational credentials.
	// This projection is administrator-declared, not a completed discovery
	// visit.
	if err := s.q.DeployInsertChainingIssuer(ctx, repo.DeployInsertChainingIssuerParams{
		ID:             s.chainingIssuer(),
		ProjectID:      proj,
		OrganizationID: org,
	}); err != nil {
		return fmt.Errorf("insert demo identity chaining issuer: %w", err)
	}
	if err := s.q.DeployInsertChainingClient(ctx, repo.DeployInsertChainingClientParams{
		ID:                    s.chainingClient(),
		ProjectID:             proj,
		OrganizationID:        org,
		RemoteSessionIssuerID: s.chainingIssuer(),
	}); err != nil {
		return fmt.Errorf("insert demo identity chaining client: %w", err)
	}

	// A client that holds its upstream credential for itself (credential_owner
	// self): Speakeasy obtains one access token with the client_credentials
	// grant and it is shared by every caller, so nobody connects an account.
	// Shaped like a Jamf Pro API client, the same tenant as the MDM fixture:
	// Jamf Pro publishes no authorization server metadata, so the issuer is
	// entered by hand with only its token endpoint, and its grant type and auth
	// method are administrator-declared. The client sends its secret in the
	// form body, as Jamf documents. Access comes from the API Roles assigned in
	// Jamf Pro, not from a requested scope. No MCP server is bound to it yet:
	// the dashboard does not tell self clients apart, and a bound server would
	// read as per-user identity. Reserved example host and an invalid
	// ciphertext keep it from obtaining any credential.
	jamfIssuer := s.det("remote-identity-provider-jamf-pro")
	jamfHost := "https://" + s.spec.OrgSlug + ".jamfcloud.example"
	if err := s.q.DeployInsertJamfIssuer(ctx, repo.DeployInsertJamfIssuerParams{
		ID:             jamfIssuer,
		ProjectID:      proj,
		OrganizationID: org,
		Issuer:         jamfHost,
		TokenEndpoint:  jamfHost + "/api/oauth/token",
	}); err != nil {
		return fmt.Errorf("insert demo jamf pro issuer: %w", err)
	}
	if err := s.q.DeployInsertJamfClient(ctx, repo.DeployInsertJamfClientParams{
		ID:                    s.det("remote-identity-client-jamf-pro"),
		ProjectID:             proj,
		OrganizationID:        org,
		RemoteSessionIssuerID: jamfIssuer,
	}); err != nil {
		return fmt.Errorf("insert demo jamf pro client: %w", err)
	}

	// Resolved from a Client ID Metadata Document, and the strongest posture
	// available: it signs an assertion with a key it publishes, so Speakeasy
	// holds no secret for it. This is the row the "Key-authenticated" badge
	// appears on.
	if err := s.q.DeployInsertCIMDClient(ctx, repo.DeployInsertCIMDClientParams{
		ID:                  s.uscKey(),
		ProjectID:           proj,
		OrganizationID:      org,
		UserSessionIssuerID: s.usIssuer(),
		CimdUrl:             demoCIMDURL,
	}); err != nil {
		return fmt.Errorf("insert demo cimd client: %w", err)
	}

	secret := pgtype.Text{String: demoSecretHash, Valid: true}
	clients := []deployUserSessionClient{
		// A public client: it presents nothing, and PKCE is the whole proof.
		// The ordinary case, and deliberately unbadged in the list.
		{s.uscPublic(), "gram_demo_client_public", pgtype.Text{String: "", Valid: false}, "Claude Code", "http://127.0.0.1:41293/callback", "11 days", pgtype.Text{String: "none", Valid: true}},
		// A confidential client presenting a secret Speakeasy issued it.
		{s.uscSecret(), "gram_demo_client_secret", secret, "Acme Nightly Batch", "https://batch.example.com/callback", "12 days", pgtype.Text{String: "client_secret_basic", Valid: true}},
		// Registered before the method was recorded. The kind still resolves
		// -- off the stored secret -- rather than reading as unknown, which is
		// the whole reason it is derived on the server.
		{s.uscLegacy(), "gram_demo_client_legacy", secret, "Acme Legacy Connector", "https://legacy.example.com/callback", "40 days", pgtype.Text{String: "", Valid: false}},
		// Contradicts itself: it committed to signed assertions and yet
		// carries a secret, so the token endpoint refuses it. It holds a
		// session it obtained before it was broken, and cannot refresh that
		// session.
		{s.uscBroken(), "gram_demo_client_broken", secret, "Vendor Sync (misconfigured)", "https://vendor.example.com/callback", "6 days", pgtype.Text{String: "private_key_jwt", Valid: true}},
	}
	for _, c := range clients {
		if err := s.q.DeployInsertUserSessionClient(ctx, repo.DeployInsertUserSessionClientParams{
			ID:                      c.id,
			ProjectID:               proj,
			OrganizationID:          org,
			UserSessionIssuerID:     s.usIssuer(),
			ClientID:                c.clientID,
			ClientSecretHash:        c.secretHash,
			ClientName:              c.name,
			RedirectUri:             c.redirect,
			IssuedAgo:               c.issuedAgo,
			TokenEndpointAuthMethod: c.authMethod,
		}); err != nil {
			return fmt.Errorf("insert demo user session client %s: %w", c.clientID, err)
		}
	}

	// refresh_token_hash is globally unique, so it is derived rather than
	// literal: two tenants seeded into one database would otherwise collide.
	// Nothing ever presents these; no demo session can be refreshed.
	sessions := []deployUserSession{
		{1, s.uscKey(), demoUsers[0], "21 days", "40 minutes", "25 minutes", "9 days"},
		{2, s.uscKey(), demoUsers[2], "19 days", "-5 minutes", "3 hours", "7 days"},
		{3, s.uscPublic(), demoUsers[1], "27 days", "35 minutes", "2 hours", "11 days"},
		{4, s.uscSecret(), demoUsers[3], "3 days", "-20 minutes", "4 days", "12 days"},
		// Expiring, and its registration can no longer authenticate, so this
		// one is the connection an operator is meant to notice.
		{5, s.uscBroken(), demoUsers[4], "16 hours", "-50 minutes", "30 hours", "6 days"},
	}
	for _, us := range sessions {
		n := fmt.Sprint(us.n)
		if err := s.q.DeployInsertUserSession(ctx, repo.DeployInsertUserSessionParams{
			ID:                  s.det("user-session-" + n),
			ProjectID:           proj,
			OrganizationID:      org,
			UserSessionIssuerID: s.usIssuer(),
			UserSessionClientID: us.client,
			SubjectUrn:          "user:" + s.userID(us.user.handle),
			Jti:                 "demo-jti-" + n,
			RefreshTokenHash:    s.det("user-session-refresh-" + n).String(),
			RefreshExpiresIn:    us.refreshExpires,
			ExpiresIn:           us.expiresIn,
			LastUsedAgo:         us.lastUsedAgo,
			CreatedAgo:          us.createdAgo,
		}); err != nil {
			return fmt.Errorf("insert demo user session %s: %w", n, err)
		}
	}

	if err := s.q.DeployInsertManagedAgentSession(ctx, repo.DeployInsertManagedAgentSessionParams{
		ID:                  s.det("managed-agent-session-1"),
		ProjectID:           proj,
		OrganizationID:      org,
		UserSessionIssuerID: s.usIssuer(),
		UserSessionClientID: s.uscPublic(),
		SubjectUrn:          "agent:" + s.det("managed-agent-1").String(),
		AuthorizerUserID:    s.userID(demoUsers[0].handle),
		Jti:                 s.det("managed-agent-jti-1").String(),
		RefreshTokenHash:    "DEMO-NOT-A-VALID-HASH-" + s.det("managed-agent-refresh-1").String(),
	}); err != nil {
		return fmt.Errorf("insert demo managed agent session: %w", err)
	}
	return nil
}

// seedDeploymentWorkloads writes the workload sessions: machines that
// authenticate with a platform-issued identity token rather than as a person.
// The MCP Sessions page labels them by issuer, subject and assigned agent, and
// explains that revoking one does not keep the workload out.
//
// The issuer URL is on a reserved domain, so no real platform can mint a token
// this row would accept. The sessions are display-only the same way the
// managed-agent session is: an invalid refresh hash, an empty delegation, and
// a jti no token carries. Assignments point at the managed agents seeded with
// the roles, one active and one suspended, so both agent states render. The
// payments workload is admitted at both tiers, so its row shows that
// withdrawing one admission still leaves it able to reconnect.
func (s *seeder) seedDeploymentWorkloads(ctx context.Context) error {
	proj := s.projectID()
	org := s.orgID()
	ciIssuer := s.det("workload-issuer-1")
	agentIssuer := s.det("workload-issuer-2")

	const (
		paymentsSubject = "repo:acme/payments-api:ref:refs/heads/main"
		docsSubject     = "repo:acme/docs-site:environment:production"
		fleetSubject    = "wimse://agents.example.com/org/acme/agent/*"
	)

	// Two issuers, deliberately of different shapes, because wildcard
	// admission is only sound on one of them. 'Acme CI' mints a subject that
	// encodes a branch ref, where a wildcard would admit anyone able to push a
	// branch, so it does NOT permit one. The agent platform mints an opaque
	// identity per resource that the caller cannot influence, which is the
	// case the feature exists for.
	//
	// Organization tier. Two of the admissions below are organization-tier and
	// name this issuer, and an organization-tier admission may only bind an
	// organization-tier issuer, so a project row here would be a policy shape
	// the management API refuses to write.
	//
	// Tags overlap deliberately, so a tag search in the Access Hub matches more
	// than one platform.
	issuers := []repo.DeployInsertOrgWorkloadIssuerParams{
		{
			ID:                     ciIssuer,
			OrganizationID:         org,
			Name:                   "Acme CI",
			Description:            "Build and deploy pipelines for Acme services, signing in as the repository and branch they run for.",
			Tags:                   []string{"ci", "build"},
			Issuer:                 "https://ci-identity.example.com",
			JwksUri:                "https://ci-identity.example.com/.well-known/jwks.json",
			AllowWildcardAdmission: false,
		},
		{
			ID:                     agentIssuer,
			OrganizationID:         org,
			Name:                   "Acme Agent Platform",
			Description:            "Autonomous support and operations agents, one identity per agent deployment.",
			Tags:                   []string{"agents", "build"},
			Issuer:                 "https://agents.example.com",
			JwksUri:                "https://agents.example.com/.well-known/jwks.json",
			AllowWildcardAdmission: true,
		},
	}
	for _, p := range issuers {
		if err := s.q.DeployInsertOrgWorkloadIssuer(ctx, p); err != nil {
			return fmt.Errorf("insert demo workload issuer %s: %w", p.Name, err)
		}
	}

	admissions := []repo.DeployInsertWorkloadAdmissionParams{
		{ID: s.det("workload-admission-1"), OrganizationID: org, ProjectID: uuid.NullUUID{UUID: proj, Valid: true}, WorkloadIssuerID: ciIssuer, Subject: paymentsSubject, MatchKind: "exact", Name: "Payments deploy", Tags: []string{"payments", "deploy"}},
		{ID: s.det("workload-admission-2"), OrganizationID: org, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, WorkloadIssuerID: ciIssuer, Subject: docsSubject, MatchKind: "exact", Name: "Docs publish", Tags: []string{"docs"}},
		{ID: s.det("workload-admission-3"), OrganizationID: org, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, WorkloadIssuerID: ciIssuer, Subject: paymentsSubject, MatchKind: "exact", Name: "Payments deploy (all projects)", Tags: []string{"payments", "deploy"}},
		// One rule standing for a whole fleet, which is what the trailing '*'
		// is for: this platform's agent id is minted per resource and is not
		// known in advance, so admitting each one exactly is not an onboarding
		// flow.
		{ID: s.det("workload-admission-4"), OrganizationID: org, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, WorkloadIssuerID: agentIssuer, Subject: fleetSubject, MatchKind: "wildcard", Name: "Agent fleet", Tags: []string{"support", "slack"}},
	}
	for _, p := range admissions {
		if err := s.q.DeployInsertWorkloadAdmission(ctx, p); err != nil {
			return fmt.Errorf("insert demo workload admission %s: %w", p.Name, err)
		}
	}

	// Keyed on (issuer, match_kind, subject) exactly as the admission is,
	// because that is the tuple the lookup resolves: an assignment whose
	// match_kind disagreed with its admission would list as having no agent.
	assignments := []repo.DeployInsertWorkloadAssignmentParams{
		{ID: s.det("workload-assignment-1"), OrganizationID: org, WorkloadIssuerID: ciIssuer, Subject: paymentsSubject, MatchKind: "exact", AgentID: s.det("managed-agent-1")},
		{ID: s.det("workload-assignment-2"), OrganizationID: org, WorkloadIssuerID: ciIssuer, Subject: docsSubject, MatchKind: "exact", AgentID: s.det("managed-agent-2")},
		// An active agent on purpose: this is the one rule standing for a whole
		// fleet, so pointing it at a suspended or revoked agent would show a
		// workload that authenticates and then reaches nothing.
		{ID: s.det("workload-assignment-3"), OrganizationID: org, WorkloadIssuerID: agentIssuer, Subject: fleetSubject, MatchKind: "wildcard", AgentID: s.det("managed-agent-4")},
	}
	for _, p := range assignments {
		if err := s.q.DeployInsertWorkloadAssignment(ctx, p); err != nil {
			return fmt.Errorf("insert demo workload assignment %s: %w", p.Subject, err)
		}
	}

	workloadSessions := []struct {
		n                                                     string
		subject                                               string
		refreshExpiresIn, expiredAgo, lastUsedAgo, createdAgo string
	}{
		{"1", paymentsSubject, "7 days", "10 minutes", "4 minutes", "2 days"},
		{"2", docsSubject, "6 days", "30 minutes", "3 hours", "4 days"},
	}
	for _, ws := range workloadSessions {
		if err := s.q.DeployInsertWorkloadSession(ctx, repo.DeployInsertWorkloadSessionParams{
			ID:                  s.det("workload-session-" + ws.n),
			ProjectID:           proj,
			OrganizationID:      org,
			UserSessionIssuerID: s.usIssuer(),
			SubjectUrn:          "workload:" + ciIssuer.String() + ":" + ws.subject,
			Jti:                 s.det("workload-jti-" + ws.n).String(),
			RefreshTokenHash:    "DEMO-NOT-A-VALID-HASH-" + s.det("workload-refresh-"+ws.n).String(),
			RefreshExpiresIn:    ws.refreshExpiresIn,
			ExpiredAgo:          ws.expiredAgo,
			LastUsedAgo:         ws.lastUsedAgo,
			CreatedAgo:          ws.createdAgo,
		}); err != nil {
			return fmt.Errorf("insert demo workload session %s: %w", ws.n, err)
		}
	}
	return nil
}

// seedDeploymentAssistants writes one assistant with a dedicated agent and one
// legacy assistant without, plus the dashboard triggers and the workload
// identity that lets the bound assistant's root trigger act as its agent.
func (s *seeder) seedDeploymentAssistants(ctx context.Context) error {
	proj := s.projectID()
	org := s.orgID()
	owner := s.userID(demoUsers[0].handle)
	bound := s.det("assistant-bound")
	legacy := s.det("assistant-legacy")
	agent := s.det("assistant-agent")
	binding := s.det("assistant-binding")
	trust := s.det("assistant-platform-trust")

	assistants := []struct {
		fixture string
		id      uuid.UUID
		name    string
	}{
		{"bound", bound, "Identity-bound assistant"},
		{"legacy", legacy, "Legacy assistant"},
	}
	for _, a := range assistants {
		if err := s.q.DeployInsertAssistant(ctx, repo.DeployInsertAssistantParams{
			ID:              a.id,
			OrganizationID:  org,
			ProjectID:       proj,
			CreatedByUserID: owner,
			Name:            a.name,
		}); err != nil {
			return fmt.Errorf("insert demo assistant %s: %w", a.fixture, err)
		}
	}

	// The dedicated agent starts with the grants provisioning gives it: every
	// MCP server and skill in the assistant's project, and its own assistant.
	if err := s.q.DeployInsertAgent(ctx, repo.DeployInsertAgentParams{
		ID:             agent,
		OrganizationID: org,
		ProjectID:      proj,
		OwnerUserID:    owner,
	}); err != nil {
		return fmt.Errorf("insert demo assistant agent: %w", err)
	}

	principal := "agent:" + agent.String()
	mcpSelectors := map[string]string{"resource_kind": "mcp", "resource_id": "*", "project_id": proj.String()}
	grants := []struct {
		name      string
		scope     string
		selectors map[string]string
	}{
		{"mcp-connect", "mcp:connect", mcpSelectors},
		{"mcp-read", "mcp:read", mcpSelectors},
		{"skill-read", "skill:read", map[string]string{"resource_kind": "skill", "resource_id": proj.String()}},
		{"assistant-write", "assistant:write", map[string]string{"resource_kind": "assistant", "resource_id": bound.String()}},
	}
	for _, g := range grants {
		selectors, err := json.Marshal(g.selectors)
		if err != nil {
			return fmt.Errorf("marshal demo assistant agent grant selectors: %w", err)
		}
		if err := s.q.DeployInsertPrincipalGrant(ctx, repo.DeployInsertPrincipalGrantParams{
			ID:             s.det("assistant-agent-grant-" + g.name),
			OrganizationID: org,
			PrincipalUrn:   principal,
			Scope:          g.scope,
			Selectors:      selectors,
		}); err != nil {
			return fmt.Errorf("insert demo assistant agent grant %s: %w", g.scope, err)
		}
	}

	for _, a := range assistants {
		if err := s.q.DeployInsertDashboardTrigger(ctx, repo.DeployInsertDashboardTriggerParams{
			ID:             s.det("assistant-dashboard-" + a.fixture),
			OrganizationID: org,
			ProjectID:      proj,
			Name:           a.name,
			TargetRef:      a.id.String(),
		}); err != nil {
			return fmt.Errorf("insert demo assistant dashboard trigger %s: %w", a.fixture, err)
		}
	}

	// The shared-space example remains paused with synthetic identifiers and no credentials.
	chatConfig, err := json.Marshal(map[string]any{
		"space_names": []string{"spaces/" + s.det("googlechat-space").String()},
		"workspace_events": map[string]any{
			"subscription":           "projects/example-project/subscriptions/demo-" + s.det("googlechat-push").String(),
			"workspace_subscription": "subscriptions/" + s.det("googlechat-subscription").String(),
			"audience":               "https://example.invalid/" + s.det("googlechat-audience").String(),
			"service_account_email":  s.det("googlechat-push-account").String() + "@example-project.iam.gserviceaccount.com",
			"user_names":             []string{"users/" + s.det("googlechat-participant").String()},
			"text_prefix":            "assistant:",
		},
	})
	if err != nil {
		return fmt.Errorf("marshal demo Google Chat configuration: %w", err)
	}
	if err := s.q.DeployInsertGoogleChatTrigger(ctx, repo.DeployInsertGoogleChatTriggerParams{
		ID:             s.det("assistant-googlechat-bound"),
		ConfigJson:     chatConfig,
		OrganizationID: org,
		ProjectID:      proj,
		TargetRef:      bound.String(),
		TargetDisplay:  "Identity-bound assistant",
	}); err != nil {
		return fmt.Errorf("insert demo Google Chat trigger: %w", err)
	}

	if err := s.q.DeployInsertAssistantWorkloadIssuer(ctx, repo.DeployInsertAssistantWorkloadIssuerParams{
		ID:             trust,
		OrganizationID: org,
		ProjectID:      proj,
		Name:           "Assistant triggers " + proj.String(),
	}); err != nil {
		return fmt.Errorf("insert demo assistant workload issuer: %w", err)
	}

	if err := s.q.DeployInsertAssistantAgentBinding(ctx, repo.DeployInsertAssistantAgentBindingParams{
		ID:             binding,
		OrganizationID: org,
		ProjectID:      proj,
		AssistantID:    bound,
		AgentID:        agent,
	}); err != nil {
		return fmt.Errorf("insert demo assistant agent binding: %w", err)
	}

	if err := s.q.DeployInsertTriggerWorkloadBinding(ctx, repo.DeployInsertTriggerWorkloadBindingParams{
		ID:                 s.det("assistant-root-binding"),
		OrganizationID:     org,
		ProjectID:          proj,
		AssistantBindingID: binding,
		WorkloadIssuerID:   trust,
		AssistantID:        bound.String(),
	}); err != nil {
		return fmt.Errorf("insert demo trigger workload binding: %w", err)
	}

	if err := s.q.DeployInsertAssistantAdmission(ctx, repo.DeployInsertAssistantAdmissionParams{
		ID:             s.det("assistant-admission"),
		OrganizationID: org,
		ProjectID:      proj,
	}); err != nil {
		return fmt.Errorf("insert demo assistant admission: %w", err)
	}

	if err := s.q.DeployInsertAssistantAssignment(ctx, repo.DeployInsertAssistantAssignmentParams{
		ID:             s.det("assistant-assignment"),
		OrganizationID: org,
		AgentID:        agent,
		ProjectID:      proj,
	}); err != nil {
		return fmt.Errorf("insert demo assistant assignment: %w", err)
	}
	return nil
}
