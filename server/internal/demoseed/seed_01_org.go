package demoseed

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
	"github.com/speakeasy-api/gram/server/internal/sigint/evaluation"
)

// orgStep is one ordered statement of the org phase.
type orgStep struct {
	what string
	run  func() error
}

func runOrgSteps(steps []orgStep) error {
	for _, st := range steps {
		if err := st.run(); err != nil {
			return fmt.Errorf("%s: %w", st.what, err)
		}
	}
	return nil
}

// seedOrg: Preflight isolation asserts, org metadata, the scoped purge, projects, features, users, memberships.
func (s *seeder) seedOrg(ctx context.Context) error {
	if err := s.orgPreflight(ctx); err != nil {
		return err
	}
	if err := s.orgMetadata(ctx); err != nil {
		return err
	}
	if err := s.orgPurge(ctx); err != nil {
		return err
	}
	if err := s.orgProject(ctx); err != nil {
		return err
	}
	if err := s.orgFeaturesAndUsers(ctx); err != nil {
		return err
	}
	return s.orgMemberships(ctx)
}

// orgPreflight refuses to run if the demo constants collide with anything
// that is not unambiguously the demo org. The identity check deliberately
// does NOT use gram_account_type: the auth callback's org-metadata upsert
// overwrites it (e.g. to 'pro') whenever someone impersonates the org, and
// the seed's own upsert restores it. A WorkOS link or a foreign slug,
// however, means this id belongs to a real org — abort.
func (s *seeder) orgPreflight(ctx context.Context) error {
	foreignOrg, err := s.q.OrgForeignOrgExists(ctx, repo.OrgForeignOrgExistsParams{
		OrgID:    s.orgID(),
		WorkosID: s.spec.WorkOSOrgID,
		OrgSlug:  s.spec.OrgSlug,
	})
	if err != nil {
		return fmt.Errorf("check demo org identity: %w", err)
	}
	if foreignOrg {
		return fmt.Errorf("demo seed aborted: org %s exists but is not the demo org", s.orgID())
	}

	foreignProject, err := s.q.OrgForeignProjectExists(ctx, repo.OrgForeignProjectExistsParams{
		ProjectID: s.projectID(),
		OrgID:     s.orgID(),
	})
	if err != nil {
		return fmt.Errorf("check demo project ownership: %w", err)
	}
	if foreignProject {
		return errors.New("demo seed aborted: demo project id owned by another org")
	}
	return nil
}

func (s *seeder) orgMetadata(ctx context.Context) error {
	org := s.orgID()
	return runOrgSteps([]orgStep{
		{"upsert demo org metadata", func() error {
			return s.q.OrgUpsertMetadata(ctx, repo.OrgUpsertMetadataParams{
				OrgID:   org,
				OrgName: s.spec.OrgName,
				OrgSlug: s.spec.OrgSlug,
			})
		}},
		{"upsert demo org onboarding", func() error { return s.q.OrgUpsertOnboarding(ctx, org) }},
		{"delete demo onboarding vendors", func() error { return s.q.OrgDeleteOnboardingVendors(ctx, org) }},
		{"insert demo onboarding vendors", func() error { return s.q.OrgInsertOnboardingVendors(ctx, org) }},
	})
}

// orgPurge clears the tenant before it is recreated. projects has ON DELETE
// RESTRICT from deployments, and toolsets only SET NULL their project_id — so
// the deployment stack and toolsets are deleted explicitly before the
// projects delete cascades everything else (chats -> chat_messages ->
// risk_results, risk_policies, skills, ...). The toolsets and projects
// deletes span every project the org owns, so each explicit delete of a row
// that would block them (RESTRICT / NO ACTION, or SET NULL on a NOT NULL
// column) spans the org's projects too, not just the seeded one.
func (s *seeder) orgPurge(ctx context.Context) error {
	if err := s.q.OrgDeleteGoogleChatMembers(ctx, repo.OrgDeleteGoogleChatMembersParams{OrgID: s.orgID(), ProjectID: s.projectID()}); err != nil {
		return fmt.Errorf("delete demo Google members: %w", err)
	}
	if err := s.q.OrgDeleteGoogleChatBindings(ctx, repo.OrgDeleteGoogleChatBindingsParams{OrgID: s.orgID(), ProjectID: s.projectID()}); err != nil {
		return fmt.Errorf("delete demo Google spaces: %w", err)
	}
	if err := s.q.OrgDeleteGoogleChatConnections(ctx, s.orgID()); err != nil {
		return fmt.Errorf("delete demo Google connection: %w", err)
	}

	org := s.orgID()
	proj := s.projectID()
	return runOrgSteps([]orgStep{
		// Killswitch aggregates retain canonical MCP server keys in immutable
		// snapshots. Clear every org-scoped aggregate and replay receipt before
		// the referenced servers/toolsets, including rows created by local
		// visitors. Header deletion cascades versions, resource snapshots, and
		// expiry markers.
		{"delete killswitch operations", func() error { return s.q.OrgDeleteKillswitchOperations(ctx, org) }},
		{"delete killswitch prescriptions", func() error { return s.q.OrgDeleteKillswitchPrescriptions(ctx, org) }},

		// Private-network access is intentionally absent from the shared demo
		// and local seed. Remove both active rows and tombstones before
		// recreating the tenant so a prior admin test cannot retain credentials
		// or expose controls when the temporary rollout flag is enabled later.
		{"delete network ingresses", func() error { return s.q.OrgDeleteNetworkIngresses(ctx, org) }},
		{"delete network ingress feature", func() error { return s.q.OrgDeleteNetworkIngressFeature(ctx, org) }},

		// Catalog registrations have both direct and transitive NO ACTION
		// children. Evidence pins its distribution and selected workflow;
		// feedback also pins selected workflows. Remove those rows first, then
		// every registration child, before replacing the canonical
		// registrations. Keep the workflow subqueries
		// organization-scoped so another tenant's rows cannot be reached even if
		// identifiers are malformed.
		{"delete platform mcp selected use evidence", func() error {
			return s.q.OrgDeletePlatformMCPSelectedUseEvidence(ctx, org)
		}},
		{"delete platform mcp feedback", func() error {
			return s.q.OrgDeletePlatformMCPFeedback(ctx, org)
		}},
		{"delete platform mcp onboarding milestones", func() error {
			return s.q.OrgDeletePlatformMCPOnboardingMilestones(ctx, org)
		}},
		{"delete platform mcp onboarding workflows", func() error {
			return s.q.OrgDeletePlatformMCPOnboardingWorkflows(ctx, org)
		}},
		{"delete platform mcp operation receipts", func() error {
			return s.q.OrgDeletePlatformMCPOperationReceipts(ctx, org)
		}},
		{"delete platform mcp setup handoffs", func() error {
			return s.q.OrgDeletePlatformMCPSetupHandoffs(ctx, org)
		}},
		{"delete platform mcp readiness", func() error {
			return s.q.OrgDeletePlatformMCPReadiness(ctx, org)
		}},
		{"delete platform mcp distributions", func() error {
			return s.q.OrgDeletePlatformMCPDistributions(ctx, org)
		}},
		{"delete platform mcp catalog registrations", func() error {
			return s.q.OrgDeletePlatformMCPCatalogRegistrations(ctx, org)
		}},

		// Binding rows point at project resources; clear them by tenant and
		// project keys, including rows whose pointers were nulled by resource
		// deletion.
		{"delete trigger workload bindings", func() error {
			return s.q.OrgDeleteTriggerWorkloadBindings(ctx, repo.OrgDeleteTriggerWorkloadBindingsParams{OrgID: org, ProjectID: proj})
		}},
		{"delete assistant agent bindings", func() error {
			return s.q.OrgDeleteAssistantAgentBindings(ctx, repo.OrgDeleteAssistantAgentBindingsParams{OrgID: org, ProjectID: proj})
		}},
		// Assistants and trigger instances cascade from projects below,
		// together with their dependent distributions/attachments. Do not
		// delete them early.

		// assistant_mcp_servers and plugin_servers RESTRICT hard server
		// deletion. Other direct mcp_servers dependents (meta members,
		// collection attachments, metadata, endpoints, and tool metadata)
		// cascade. mcp_servers in turn pins its toolset with RESTRICT, so it
		// must still go before the toolsets delete.
		{"delete assistant mcp servers", func() error { return s.q.OrgDeleteAssistantMCPServers(ctx, org) }},
		// Toolsets are cleared across the organization, including other
		// projects. Clear their plugin attachments across the same scope before
		// deleting them.
		{"delete plugin servers", func() error { return s.q.OrgDeletePluginServers(ctx, org) }},
		// Required binding references cascade from their parents. Clear tenant
		// bindings explicitly before reseeding the referenced rows.
		{"delete principal remote session bindings", func() error { return s.q.OrgDeletePrincipalRemoteSessionBindings(ctx, org) }},
		// Clear upstream fixtures before their issuer/client and project
		// cascade.
		{"delete remote sessions", func() error {
			return s.q.OrgDeleteRemoteSessions(ctx, repo.OrgDeleteRemoteSessionsParams{ProjectID: proj, OrgID: org})
		}},
		// Cached EMA credentials and the trusted issuer sessions they cite SET
		// NULL on their parents, so clear them by tenant first.
		{"delete remote session ema credentials", func() error { return s.q.OrgDeleteRemoteSessionEMACredentials(ctx, org) }},
		{"delete trusted issuer sessions", func() error { return s.q.OrgDeleteTrustedIssuerSessions(ctx, org) }},
		// A reseed explicitly resets preparation claims before hard-deleting
		// their parents. Do not depend on lifecycle triggers or SET NULL owner
		// references.
		{"delete remote session ema bindings", func() error { return s.q.OrgDeleteRemoteSessionEMABindings(ctx, org) }},
		// EMA provider orders reference issuers without cascading, retired rows
		// too.
		{"delete remote session ema providers", func() error { return s.q.OrgDeleteRemoteSessionEMAProviders(ctx, org) }},
		{"delete remote session clients", func() error {
			return s.q.OrgDeleteRemoteSessionClients(ctx, repo.OrgDeleteRemoteSessionClientsParams{ProjectID: proj, OrgID: org})
		}},
		{"delete remote session issuers", func() error {
			return s.q.OrgDeleteRemoteSessionIssuers(ctx, repo.OrgDeleteRemoteSessionIssuersParams{ProjectID: proj, OrgID: org})
		}},
		{"delete mcp servers", func() error { return s.q.OrgDeleteMCPServers(ctx, org) }},
		{"delete remote protected resources", func() error { return s.q.OrgDeleteRemoteProtectedResources(ctx, proj) }},
		{"delete meta mcp servers", func() error { return s.q.OrgDeleteMetaMCPServers(ctx, org) }},
		// meta_mcp_servers RESTRICTs its issuer, so issuers clear after it.
		{"delete user session issuers", func() error {
			return s.q.OrgDeleteUserSessionIssuers(ctx, repo.OrgDeleteUserSessionIssuersParams{ProjectID: proj, OrgID: org})
		}},
		{"delete external oauth server metadata", func() error {
			return s.q.OrgDeleteExternalOAuthServerMetadata(ctx, repo.OrgDeleteExternalOAuthServerMetadataParams{ProjectID: proj, OrgID: org})
		}},
		{"delete toolsets", func() error { return s.q.OrgDeleteToolsets(ctx, org) }},
		// environments.project_id is NOT NULL but its FK is ON DELETE SET NULL,
		// so the projects delete below fails outright on any environment row.
		// The demo tenant seeds none, but the local development tenant does
		// (see RunLocalFixtures), and a reseed there must not wedge.
		{"delete environments", func() error { return s.q.OrgDeleteEnvironments(ctx, org) }},
		{"delete deployment statuses", func() error { return s.q.OrgDeleteDeploymentStatuses(ctx, org) }},
		{"delete deployment logs", func() error { return s.q.OrgDeleteDeploymentLogs(ctx, org) }},
		{"delete deployments", func() error { return s.q.OrgDeleteDeployments(ctx, org) }},
		{"delete assets", func() error { return s.q.OrgDeleteAssets(ctx, proj) }},
		// api_keys.project_id is ON DELETE SET NULL, so the projects delete
		// below only orphans the row — the key keeps authenticating, scoped to
		// the org. Demo visitors hold org:admin (authz.DemoScopeGrants) and can
		// mint keys, so without this delete those keys outlive every reseed and
		// keep working long after the session that created them is gone. The
		// local tenant's own key is reinserted by RunLocalFixtures immediately
		// after this seed runs. litellm_instances.api_key_id is ON DELETE
		// RESTRICT, so an instance a visitor created would abort the run here;
		// it goes first.
		{"delete litellm instances", func() error { return s.q.OrgDeleteLitellmInstances(ctx, org) }},
		{"delete api keys", func() error { return s.q.OrgDeleteAPIKeys(ctx, org) }},
		{"delete setup tasks", func() error { return s.q.OrgDeleteSetupTasks(ctx, org) }},
		{"delete business memories", func() error { return s.q.OrgDeleteBusinessMemories(ctx, org) }},
		{"delete policy bypass grants", func() error {
			return s.q.OrgDeletePolicyBypassGrants(ctx, repo.OrgDeletePolicyBypassGrantsParams{OrgID: org, PolicyID: s.policySM().String()})
		}},
		{"delete github grants", func() error {
			return s.q.OrgDeleteGrantsByID(ctx, repo.OrgDeleteGrantsByIDParams{
				OrgID: org,
				Ids:   []uuid.UUID{s.det("github-direct-grant"), s.det("github-wildcard-grant")},
			})
		}},
		// Workload trust is organization-scoped: an organization-tier issuer or
		// admission would outlive the projects cascade, so all three tables are
		// cleared explicitly, children first.
		{"delete workload agent assignments", func() error { return s.q.OrgDeleteWorkloadAgentAssignments(ctx, org) }},
		{"delete workload identity admissions", func() error { return s.q.OrgDeleteWorkloadIdentityAdmissions(ctx, org) }},
		{"delete workload issuers", func() error { return s.q.OrgDeleteWorkloadIssuers(ctx, org) }},
		// Participant project IDs are NOT NULL, so delete them before SET NULL
		// project deletion.
		{"delete chat message participants", func() error { return s.q.OrgDeleteChatMessageParticipants(ctx, org) }},
		// Sigint ownership/reference FKs use ON DELETE SET NULL with non-null
		// columns, so every row in the org's projects, including tombstones,
		// must be physically removed child-first before the projects delete.
		{"delete sigint sensor signals", func() error {
			return s.q.OrgDeleteSigintSensorSignals(ctx, org)
		}},
		{"delete sigint sensors", func() error {
			return s.q.OrgDeleteSigintSensors(ctx, org)
		}},
		{"delete sigint custom signals", func() error {
			return s.q.OrgDeleteSigintCustomSignals(ctx, org)
		}},
		{"delete projects", func() error { return s.q.OrgDeleteProjects(ctx, org) }},
	})
}

func orgText(v string) pgtype.Text { return pgtype.Text{String: v, Valid: v != ""} }

// orgProject recreates the single project and its signals intelligence
// examples.
func (s *seeder) orgProject(ctx context.Context) error {
	proj := s.projectID()
	if err := s.q.OrgInsertProject(ctx, repo.OrgInsertProjectParams{ProjectID: proj, OrgID: s.orgID()}); err != nil {
		return fmt.Errorf("insert demo project: %w", err)
	}

	// Signals intelligence examples cover every authoring mode. API is shared
	// by two sensors, while Draft campaign intentionally has no memberships so
	// the incomplete authoring state remains visible. An empty criteria or
	// instructions string is written as NULL.
	signals := []struct{ key, name, desc, criteria string }{
		{"api", "API", "Requests involving the public or internal API.",
			"The conversation concerns API endpoints, requests, responses, or contracts."},
		{"database", "Database", "Requests involving persistent data stores.",
			"The conversation concerns queries, migrations, schemas, or database operations."},
		{"authentication", "Authentication", "Requests involving identity and authentication.",
			"The conversation concerns sign-in, credentials, sessions, or identity providers."},
		{"worker", "Worker", "Requests primarily involving asynchronous workers.",
			"The conversation concerns background jobs, queues, or worker processes."},
		{"low", "Low", "Little or no customer impact.",
			"The issue is cosmetic or has a straightforward workaround."},
		{"medium", "Medium", "Noticeable but bounded customer impact.",
			"The issue impairs a workflow but does not make the product broadly unavailable."},
		{"high", "High", "Severe or widespread customer impact.",
			"The issue blocks a critical workflow or affects many customers."},
		{"launch", "Launch", "A future product launch campaign.", ""},
		{"renewal", "Renewal", "A future customer renewal campaign.", ""},
	}
	for _, sig := range signals {
		if err := s.q.OrgInsertSigintCustomSignal(ctx, repo.OrgInsertSigintCustomSignalParams{
			ID:                 s.det("sigint-signal-" + sig.key),
			ProjectID:          proj,
			Name:               sig.name,
			Slug:               sig.key,
			Description:        sig.desc,
			ClassifierCriteria: orgText(sig.criteria),
		}); err != nil {
			return fmt.Errorf("insert demo sigint signal %s: %w", sig.key, err)
		}
	}

	for _, sen := range demoSigintSensors {
		// Input configuration is an application-enforced enum. Reject an invalid
		// shared definition so the ClickHouse readings never report a context the
		// evaluator would not run.
		if err := evaluation.ValidateInputConfiguration(sen.inputStrategy, int(sen.windowSize)); err != nil {
			return fmt.Errorf("demo sigint sensor %s: %w", sen.key, err)
		}
		if err := s.q.OrgInsertSigintSensor(ctx, repo.OrgInsertSigintSensorParams{
			ID:            s.det("sigint-sensor-" + sen.key),
			ProjectID:     proj,
			Name:          sen.name,
			Slug:          sen.slug,
			Description:   sen.desc,
			Instructions:  orgText(sen.instructions),
			Mode:          sen.mode,
			Enabled:       sen.enabled,
			InputStrategy: sen.inputStrategy,
			WindowSize:    sen.windowSize,
		}); err != nil {
			return fmt.Errorf("insert demo sigint sensor %s: %w", sen.key, err)
		}
	}

	for _, m := range demoSigintMemberships {
		if err := s.q.OrgInsertSigintSensorSignal(ctx, repo.OrgInsertSigintSensorSignalParams{
			ID:        s.det("sigint-membership-" + m.key),
			ProjectID: proj,
			SensorID:  s.det("sigint-sensor-" + m.sensor),
			SignalID:  s.det("sigint-signal-" + m.signal),
			SortOrder: m.order,
		}); err != nil {
			return fmt.Errorf("insert demo sigint membership %s: %w", m.key, err)
		}
	}
	return nil
}

func (s *seeder) orgFeaturesAndUsers(ctx context.Context) error {
	org := s.orgID()
	// 'rbac' is required for the agent-sessions list: without it
	// authz.ShouldEnforce is false and chatVisibilityScope falls back to
	// own-sessions-only, hiding every seeded chat (owned by the seeded users).
	if err := s.q.OrgInsertFeatures(ctx, repo.OrgInsertFeaturesParams{
		OrgID:    org,
		Features: []string{"logs", "tool_io_logs", "session_capture", "skills", "rbac", "signals_intelligence"},
	}); err != nil {
		return fmt.Errorf("insert demo features: %w", err)
	}
	// Unlike demo entitlements, preserve an explicit fail-closed choice on
	// reseed.
	if err := s.q.OrgInsertHooksFailOpenFeature(ctx, org); err != nil {
		return fmt.Errorf("insert demo hooks_fail_open feature: %w", err)
	}

	for _, u := range demoUsers {
		id := s.userID(u.handle)
		if err := s.q.OrgUpsertUser(ctx, repo.OrgUpsertUserParams{
			ID:          id,
			Email:       s.email(u.handle),
			DisplayName: u.name,
			WorkosID:    "workos_" + id,
		}); err != nil {
			return fmt.Errorf("upsert demo user %s: %w", id, err)
		}
	}

	// Customized Security selection: defer Anthropic admin controls and
	// include server distribution. Both views read this same explicit
	// selection.
	if err := s.q.OrgInsertSetupTasks(ctx, repo.OrgInsertSetupTasksParams{
		OrgID:                org,
		InstrumentAssignee:   s.userID("priya"),
		SupportAssigneeEmail: s.email("security-owner"),
	}); err != nil {
		return fmt.Errorf("insert demo setup tasks: %w", err)
	}
	return nil
}

// orgMemberships writes fake, credential-less members so
// team/enrollment/facepile surfaces render. Real users still never join the
// demo org — access is by impersonation only. No WorkOS sync job iterates
// local rows, so fake workos_* ids are inert while
// organization_metadata.workos_id stays NULL.
func (s *seeder) orgMemberships(ctx context.Context) error {
	org := s.orgID()
	if err := runOrgSteps([]orgStep{
		{"delete agent grants", func() error { return s.q.OrgDeleteAgentGrants(ctx, org) }},
		{"delete agents", func() error { return s.q.OrgDeleteAgents(ctx, org) }},
		{"delete slack identity mappings", func() error { return s.q.OrgDeleteSlackIdentityMappings(ctx, org) }},
		{"delete memberships", func() error { return s.q.OrgDeleteMemberships(ctx, org) }},
	}); err != nil {
		return err
	}
	for i, u := range demoUsers {
		id := s.userID(u.handle)
		if err := s.q.OrgInsertMembership(ctx, repo.OrgInsertMembershipParams{
			OrgID:              org,
			UserID:             id,
			WorkosUserID:       "workos_" + id,
			WorkosMembershipID: "demo_mem_" + id,
			Position:           int32(i + 1),
		}); err != nil {
			return fmt.Errorf("insert demo membership %s: %w", id, err)
		}
	}

	// Managed identities are distinct from OAuth client registrations.
	// Existing fictional owners exercise name/initials rendering without
	// adding external avatar dependencies. The two active release agents
	// receive only project/server-scoped direct grants alongside their inert
	// attachments.
	if err := s.q.OrgInsertManagedAgents(ctx, repo.OrgInsertManagedAgentsParams{
		Agent1: s.det("managed-agent-1"),
		OrgID:  org,
		Owner1: s.userID(demoUsers[0].handle),
		Agent2: s.det("managed-agent-2"),
		Owner2: s.userID(demoUsers[1].handle),
		Agent3: s.det("managed-agent-3"),
		Owner3: s.userID(demoUsers[2].handle),
		Agent4: s.det("managed-agent-4"),
	}); err != nil {
		return fmt.Errorf("insert demo managed agents: %w", err)
	}

	// Role assignments (Roles column on the team page). Global roles are
	// synced from WorkOS in real envs; tolerate their absence locally.
	adminURN, err := s.orgGlobalRoleURN(ctx, "admin")
	if err != nil {
		return err
	}
	memberURN, err := s.orgGlobalRoleURN(ctx, "member")
	if err != nil {
		return err
	}
	if err := s.q.OrgDeleteRoleAssignments(ctx, org); err != nil {
		return fmt.Errorf("delete demo role assignments: %w", err)
	}
	if adminURN == "" || memberURN == "" {
		return nil
	}
	for _, u := range demoUsers {
		id := s.userID(u.handle)
		role := memberURN
		if id == s.userID("lucas") || id == s.userID("priya") {
			role = adminURN
		}
		if err := s.q.OrgInsertRoleAssignment(ctx, repo.OrgInsertRoleAssignmentParams{
			OrgID:        org,
			WorkosUserID: "workos_" + id,
			UserID:       id,
			RoleUrn:      role,
		}); err != nil {
			return fmt.Errorf("insert demo role assignment %s: %w", id, err)
		}
	}
	return nil
}

// orgGlobalRoleURN is "role:global:<id>" for the global role with this WorkOS
// slug, or "" when none is synced.
func (s *seeder) orgGlobalRoleURN(ctx context.Context, slug string) (string, error) {
	urn, err := s.q.OrgGlobalRoleURN(ctx, slug)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("read global role %s: %w", slug, err)
	default:
		return urn, nil
	}
}

// demoSigintSensor is one seeded signals intelligence sensor.
type demoSigintSensor struct {
	key, name, slug, desc, instructions, mode, inputStrategy string
	enabled                                                  bool
	windowSize                                               int32
}

// demoSigintSensors show current-message context, five- and ten-message
// sliding windows, and a disabled draft. The ClickHouse readings are generated
// for the enabled ones.
var demoSigintSensors = []demoSigintSensor{
	{"service-areas", "Service areas", "service-areas", "Labels every service area discussed in a conversation.",
		"Select every product service that materially appears in the target message.", "multi_label", "single_message", true, 5},
	{"primary-service", "Primary service", "primary-service", "Chooses the service most central to a conversation.",
		"Choose the single service that best describes the target request.", "exclusive", "sliding_window", true, 5},
	{"impact", "Customer impact", "customer-impact", "Orders conversations by customer impact.",
		"Score the target message impact from low to high using the supplied levels.", "ordered_score", "sliding_window", true, 10},
	{"draft-campaign", "Draft campaign", "draft-campaign", "An incomplete campaign classifier awaiting its labels.",
		"", "multi_label", "single_message", false, 5},
}

// demoSigintMembership places one signal at a position in a sensor.
type demoSigintMembership struct {
	key, sensor, signal string
	order               int32
}

// demoSigintMemberships order each enabled sensor's signals. Signal slugs equal
// their keys.
var demoSigintMemberships = []demoSigintMembership{
	{"service-api", "service-areas", "api", 0},
	{"service-database", "service-areas", "database", 1},
	{"service-authentication", "service-areas", "authentication", 2},
	{"primary-api", "primary-service", "api", 0},
	{"primary-database", "primary-service", "database", 1},
	{"primary-worker", "primary-service", "worker", 2},
	{"impact-low", "impact", "low", 0},
	{"impact-medium", "impact", "medium", 1},
	{"impact-high", "impact", "high", 2},
}
