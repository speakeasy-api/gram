package demoseed

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
)

// postflightIDCheck is one count assert: the count must equal want. msg is
// the failure message; a "%d" in it receives the count.
type postflightIDCheck struct {
	count func(context.Context) (int64, error)
	want  int64
	msg   string
}

// runPostflightIDChecks runs checks in order and fails on the first count
// that does not match.
func runPostflightIDChecks(ctx context.Context, checks []postflightIDCheck) error {
	for _, c := range checks {
		n, err := c.count(ctx)
		if err != nil {
			return fmt.Errorf("postflight check %q: %w", c.msg, err)
		}
		if n == c.want {
			continue
		}
		if strings.Contains(c.msg, "%d") {
			return fmt.Errorf(c.msg, n)
		}
		return errors.New(c.msg)
	}
	return nil
}

// postflightIdentity covers postgres.sql lines 3693-4302: the identity,
// connection, workload, Slack, widget, and sigint asserts.
func (s *seeder) postflightIdentity(ctx context.Context) error {
	org := s.orgID()
	proj := s.projectID()
	jamfIssuer := s.det("remote-identity-provider-jamf-pro")
	jamfClient := s.det("remote-identity-client-jamf-pro")
	linearRemoteIssuer := s.det("remote-identity-provider-linear")

	byOrg := func(f func(context.Context, string) (int64, error)) func(context.Context) (int64, error) {
		return func(ctx context.Context) (int64, error) { return f(ctx, org) }
	}
	byProj := func(f func(context.Context, uuid.UUID) (int64, error)) func(context.Context) (int64, error) {
		return func(ctx context.Context) (int64, error) { return f(ctx, proj) }
	}

	userIDs := make([]string, len(demoUsers))
	for i, u := range demoUsers {
		userIDs[i] = s.userID(u.handle)
	}

	identity := []postflightIDCheck{
		// The seed never creates API keys: any row left here was minted by a
		// demo visitor and would grant programmatic access that survives the
		// reseed. (SQL: IF stray > 0; a count is never negative, so == 0.)
		{byOrg(s.q.PostflightIDAPIKeys), 0, "demo seed postflight: %d api keys survived the reseed"},
		// Two scoped connect grants, four for the identity-bound assistant's
		// agent, and two letting the release notes agent enroll devices.
		{byOrg(s.q.PostflightIDAgentGrants), 8, "demo seed postflight: expected 8 scoped agent grants, found %d"},
		{byOrg(s.q.PostflightIDDeviceJoinTokens), 4, "demo seed postflight: expected 4 bootstrap tokens, found %d"},
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDScopedAgentGrants(ctx, repo.PostflightIDScopedAgentGrantsParams{
				OrgID:         org,
				AgentUrn1:     "agent:" + s.det("managed-agent-1").String(),
				AgentUrn4:     "agent:" + s.det("managed-agent-4").String(),
				McpServerID:   s.det("mcpserver-linear").String(),
				ProjectIDText: proj.String(),
			})
		}, 2, "demo seed postflight: expected exact project/server-scoped agent grants"},
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDSessionAttachments(ctx, repo.PostflightIDSessionAttachmentsParams{
				UserSessionID:   s.det("user-session-6"),
				OrgID:           org,
				ProjectID:       proj,
				RemoteSessionID: s.det("attachment-session"),
			})
		}, 2, "demo seed postflight: expected 2 inert reachable exact-session attachments, found %d"},
		{byOrg(s.q.PostflightIDSessionBindings), 2, "demo seed postflight: expected 2 principal session bindings, found %d"},
		{byProj(s.q.PostflightIDUserOwnedUpstreamSessions), 7, "demo seed postflight: expected 7 user-owned upstream sessions, found %d"},
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDInertUpstreamSessions(ctx, s.det("remote-identity-client-linear"))
		}, 6, "demo seed postflight: expected 6 inert validated-spread upstream sessions, found %d"},
		{byOrg(s.q.PostflightIDAgents), 5, "demo seed postflight: expected 5 managed agents, found %d"},
		{byOrg(s.q.PostflightIDAgentSessions), 1, "demo seed postflight: expected 1 managed agent session, found %d"},
		// This External OAuth row drives the metadata recommendation. Keep its
		// count stable so a rerun cannot duplicate it or silently drop the demo
		// surface.
		{byProj(s.q.PostflightIDExternalOAuthMetadata), 1, "demo seed postflight: expected 1 external OAuth metadata row, found %d"},
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDChainingIssuer(ctx, repo.PostflightIDChainingIssuerParams{
				ProjectID:      proj,
				OrgID:          org,
				ChainingIssuer: s.chainingIssuer(),
			})
		}, 1, "demo seed postflight: declared chaining metadata must not claim discovery"},
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDChainingClient(ctx, repo.PostflightIDChainingClientParams{
				ProjectID:      proj,
				ChainingClient: s.chainingClient(),
				ChainingIssuer: s.chainingIssuer(),
			})
		}, 1, "demo seed postflight: expected one declared chaining registration, found %d"},
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDJamfIssuer(ctx, repo.PostflightIDJamfIssuerParams{
				ProjectID:  proj,
				OrgID:      org,
				JamfIssuer: jamfIssuer,
			})
		}, 1, "demo seed postflight: Jamf Pro issuer must have only a token endpoint and must not claim discovery"},
		// Exactly one self client in the tenant.
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDSelfClients(ctx, repo.PostflightIDSelfClientsParams{ProjectID: proj, OrgID: org})
		}, 1, "demo seed postflight: expected 1 self remote session client, found %d"},
		// Its secret is the inert ciphertext.
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDJamfSelfClient(ctx, repo.PostflightIDJamfSelfClientParams{
				ProjectID:  proj,
				JamfClient: jamfClient,
				JamfIssuer: jamfIssuer,
			})
		}, 1, "demo seed postflight: expected the inert Jamf Pro self client, found %d"},
		// Nobody connects a self client, and no MCP server uses this one.
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDJamfClientIssuerBindings(ctx, jamfClient)
		}, 0, "demo seed postflight: Jamf Pro self client must not be bound to a user session issuer, found %d bindings"},
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDJamfClientSessions(ctx, jamfClient)
		}, 0, "demo seed postflight: Jamf Pro self client must have no remote sessions, found %d"},
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDJamfIssuerServers(ctx, repo.PostflightIDJamfIssuerServersParams{ProjectID: proj, JamfIssuer: jamfIssuer})
		}, 0, "demo seed postflight: no MCP server may route to the Jamf Pro issuer, found %d"},
		// The registrations are the point of the Connections surfaces: one per
		// credential kind, plus the pre-column row. A rerun that dropped or
		// duplicated any of them would leave the badges telling a different
		// story than the one they were seeded to tell.
		// One issuer per Connections credential story (acme-partner-gateway),
		// three project MCP issuers, and the organization-wide workforce issuer
		// used by GitHub.
		{byProj(s.q.PostflightIDProjectUserSessionIssuers), 4, "demo seed postflight: expected 4 project user session issuers, found %d"},
		{byOrg(s.q.PostflightIDOrgUserSessionIssuers), 1, "demo seed postflight: expected 1 organization user session issuer, found %d"},
		// The workforce issuer shows a shared authorization server.
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDWorkforceIssuer(ctx, repo.PostflightIDWorkforceIssuerParams{
				WorkforceIssuer: s.det("issuer-workforce"),
				OrgID:           org,
			})
		}, 1, "demo seed postflight: expected the workforce user session issuer in shared mode, found %d"},
		{byProj(s.q.PostflightIDUserSessionClients), 8, "demo seed postflight: expected 8 registered agents, found %d"},
		// EMA: two ready bindings, no cached credentials, and one saved
		// provider order. The workforce sign-in must not trust the fictional
		// provider.
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDWorkforceTrustsEMAProvider(ctx, repo.PostflightIDWorkforceTrustsEMAProviderParams{
				WorkforceIssuer: s.det("issuer-workforce"),
				OrgID:           org,
				EmaProvider:     s.det("ema-idp"),
			})
		}, 0, "demo seed postflight: the workforce issuer must not trust Acme Identity, found %d"},
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDReadyEMABindings(ctx, repo.PostflightIDReadyEMABindingsParams{OrgID: org, ProjectID: proj})
		}, 2, "demo seed postflight: expected 2 ready EMA bindings, found %d"},
		{byOrg(s.q.PostflightIDEMACredentials), 0, "demo seed postflight: expected no EMA credentials, found %d"},
		{byOrg(s.q.PostflightIDTrustedIssuerSessions), 0, "demo seed postflight: expected no trusted issuer sessions, found %d"},
		{byOrg(s.q.PostflightIDSavedEMAProviders), 1, "demo seed postflight: expected 1 saved EMA provider, found %d"},
		// Linear's remote identity provider, the identity chaining example, Jamf
		// Pro, and the two EMA services' authorization servers.
		{byProj(s.q.PostflightIDProjectRemoteSessionIssuers), 5, "demo seed postflight: expected 5 project remote session issuers, found %d"},
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDRemoteIdentityClients(ctx, repo.PostflightIDRemoteIdentityClientsParams{ProjectID: proj, OrgID: org})
		}, 1, "demo seed postflight: expected 1 Remote MCP identity client, found %d"},
		{byProj(s.q.PostflightIDRemoteIdentityBindings), 2, "demo seed postflight: expected 2 Remote MCP User Identity bindings, found %d"},
		// The stamp has to be what ResyncMCPServerRemoteSessionIssuers would
		// derive: every live client bound to Linear's user session issuer names
		// one remote issuer, and it is the stamped one. The query returns no
		// row when its HAVING fails (the SQL's IF NOT FOUND), so a row counts
		// as 1 and no row as 0.
		{func(ctx context.Context) (int64, error) {
			_, err := s.q.PostflightIDLinearIssuerStamp(ctx, repo.PostflightIDLinearIssuerStampParams{
				LinearIssuer:       s.det("issuer-linear"),
				LinearRemoteIssuer: linearRemoteIssuer,
			})
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				return 0, nil
			case err != nil:
				return 0, fmt.Errorf("linear issuer stamp: %w", err)
			}
			return 1, nil
		}, 1, "demo seed postflight: Linear user session issuer must bind clients of exactly one remote issuer, its stamped provider"},
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDLinearStampedServers(ctx, repo.PostflightIDLinearStampedServersParams{
				ProjectID:          proj,
				LinearRemoteIssuer: linearRemoteIssuer,
			})
		}, 1, "demo seed postflight: expected 1 MCP server stamped with the Remote MCP identity provider, found %d"},
		{byProj(s.q.PostflightIDLinearProtectedResource), 1, "demo seed postflight: expected Linear's protected resource pinned to read, found %d matching rows"},
		// Linear's two clients, and each EMA service's own client.
		{byProj(s.q.PostflightIDServerClientAssociations), 4, "demo seed postflight: expected 4 MCP server upstream client associations, found %d"},
		{byProj(s.q.PostflightIDServiceAccountHeaders), 1, "demo seed postflight: expected 1 Remote MCP Service Account header, found %d"},
		// Managed-agent credentials are a separate surface from ordinary MCP
		// connections, even though both fixtures belong to the same project.
		{byProj(s.q.PostflightIDProjectAgentSessions), 1, "demo seed postflight: expected 1 project-scoped managed agent session, found %d"},
		{byProj(s.q.PostflightIDConnections), 11, "demo seed postflight: expected 11 MCP connections, found %d"},
		// Spread across servers, not pooled on one: the connections tab groups
		// by MCP server, and a single group makes that view look broken.
		{byProj(s.q.PostflightIDConnectionServers), 4, "demo seed postflight: connections span %d MCP servers, expected 4"},
		// Everyone holds one. A person whose usage panels show a week of
		// traffic beside an empty connections tab reads as a broken join.
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDUsersWithoutConnection(ctx, repo.PostflightIDUsersWithoutConnectionParams{UserIds: userIDs, ProjectID: proj})
		}, 0, "demo seed postflight: %d demo users hold no MCP connection"},
		// Every seeded connection hangs off a registration. One with none would
		// be filed under "Unknown client" and carry no credential reading at
		// all.
		{byProj(s.q.PostflightIDUnregisteredConnections), 0, "demo seed postflight: %d MCP connections have no registration"},
		// The legacy assistant stays without a dedicated agent.
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDAssistants(ctx, repo.PostflightIDAssistantsParams{OrgID: org, ProjectID: proj})
		}, 2, "demo seed postflight: expected 2 assistants, found %d"},
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDTriggerInstances(ctx, repo.PostflightIDTriggerInstancesParams{OrgID: org, ProjectID: proj})
		}, 2, "demo seed postflight: expected 2 dashboard roots, found %d"},
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDAssistantAgentBindings(ctx, repo.PostflightIDAssistantAgentBindingsParams{OrgID: org, ProjectID: proj})
		}, 1, "demo seed postflight: expected 1 assistant binding, found %d"},
		{byProj(s.q.PostflightIDAssistantThreads), 2, "demo seed postflight: expected 2 assistant threads, found %d"},
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDTriggerWorkloadBindings(ctx, repo.PostflightIDTriggerWorkloadBindingsParams{OrgID: org, ProjectID: proj})
		}, 1, "demo seed postflight: expected 1 root binding, found %d"},
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDBoundAssistantRoot(ctx, repo.PostflightIDBoundAssistantRootParams{
				OrgID:       org,
				ProjectID:   proj,
				AssistantID: s.det("assistant-bound"),
				OwnerUserID: s.userID(demoUsers[0].handle),
			})
		}, 1, "demo seed postflight: identity-bound assistant root is incoherent"},
		// Workload sessions, and the trust configuration that labels them. A
		// workload whose issuer or assignment went missing would render as an
		// unnamed issuer with no agent, which is a real state but not the one
		// seeded.
		{byOrg(s.q.PostflightIDWorkloadIssuers), 3, "demo seed postflight: expected 3 workload issuers, found %d"},
		{byOrg(s.q.PostflightIDWorkloadAdmissions), 5, "demo seed postflight: expected 5 workload admissions, found %d"},
	}
	if err := runPostflightIDChecks(ctx, identity); err != nil {
		return err
	}

	// The seed writes the workload tables directly, so it never passes through
	// workloadidentity.ValidateSubjectRule the way the management API does.
	// This assert is that validation, applied to what was actually written
	// (see PostflightIDInvalidSubjectRules for the rules).
	rules, err := s.q.PostflightIDInvalidSubjectRules(ctx, org)
	if err != nil {
		return fmt.Errorf("postflight check workload subject rules: %w", err)
	}
	if rules.Stray != 0 {
		return fmt.Errorf("demo seed postflight: %d workload subject rules ValidateSubjectRule would refuse: %s", rules.Stray, rules.StrayDetail)
	}

	sig := func(name string) uuid.UUID { return s.det("sigint-" + name) }
	rest := []postflightIDCheck{
		{byOrg(s.q.PostflightIDWorkloadAssignments), 4, "demo seed postflight: expected 4 workload agent assignments, found %d"},
		// An admission may not out-reach its issuer's tier: an
		// organization-tier admission has to name an organization-tier issuer.
		// The management API refuses the pairing and the admission lookup
		// ignores it, so a seeded row in that shape would be silently inert —
		// the page would list it and no workload would match.
		{byOrg(s.q.PostflightIDAdmissionsOutreachingIssuer), 0, "demo seed postflight: %d admissions whose tier out-reaches their issuer's"},
		// A wildcard rule standing for a fleet is the headline of this fixture,
		// so its agent has to be one a workload can actually inherit. A
		// suspended or revoked agent resolves and is then denied, which reads
		// as a broken feature.
		{byOrg(s.q.PostflightIDWildcardAssignmentsToInactiveAgents), 0, "demo seed postflight: %d wildcard assignments pointing at a suspended or revoked agent"},
		{byProj(s.q.PostflightIDWorkloadSessions), 2, "demo seed postflight: expected 2 workload sessions, found %d"},
		// The Shadow AI status column is only worth looking at when it shows
		// more than one answer. Only a decided tool gets a row here —
		// unreviewed IS the absence of one — so the table stores just the two
		// decided states, and both have to survive the reseed with their full
		// complement of rows.
		{byOrg(s.q.PostflightIDApprovedAITools), 3, "demo seed postflight: %d approved AI tools, expected 3"},
		{byOrg(s.q.PostflightIDBlockedAITools), 2, "demo seed postflight: %d blocked AI tools, expected 2"},
		// Anything else in the column would be a row the seed no longer means
		// to write: an undecided overlay row renders as unreviewed and is
		// indistinguishable on the page from a tool nobody has touched.
		{byOrg(s.q.PostflightIDUndecidedAITools), 0, "demo seed postflight: %d AI tool rows carry a status other than approved or blocked"},
		{byOrg(s.q.PostflightIDDirectoryRoleMappings), 4, "demo seed postflight: expected 4 directory role mappings, found %d"},
		// One group grants a set of roles, so the mapping page shows two role
		// chips on a single source.
		{byOrg(s.q.PostflightIDInfraGroupMappedRoles), 2, "demo seed postflight: expected 2 roles for Infra, found %d"},
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDGitHubCheckAccessGrants(ctx, repo.PostflightIDGitHubCheckAccessGrantsParams{
				OrgID:            org,
				ContractorsBlock: s.det("github-contractors-block"),
				DirectGrant:      s.det("github-direct-grant"),
				WildcardGrant:    s.det("github-wildcard-grant"),
			})
		}, 3, "demo seed postflight: expected 3 GitHub Check Access grants, found %d"},
		{byOrg(s.q.PostflightIDSlackIdentityMappings), 6, "demo seed postflight: expected 6 Slack mapping examples, found %d"},
		{byProj(s.q.PostflightIDMessageParticipants), 3, "demo seed postflight: expected 3 message participants, found %d"},
		{byOrg(s.q.PostflightIDSlackDirectoryMemberships), 10, "demo seed postflight: expected 10 Slack workspace memberships, found %d"},
		{byOrg(s.q.PostflightIDSlackDirectoryConnections), 2, "demo seed postflight: expected 2 Slack workspace connections, found %d"},
		{byProj(s.q.PostflightIDWidgets), 6, "demo seed postflight: expected 6 Explore widgets, found %d"},
		// Sigint postflights preserve the complete authoring story: all modes,
		// ordered memberships, a shared catalog entry, and an incomplete draft.
		{byProj(s.q.PostflightIDSigintSignals), 9, "demo seed postflight: expected 9 sigint signals, found %d"},
		{byProj(s.q.PostflightIDSigintSensors), 4, "demo seed postflight: expected 4 sigint sensors, found %d"},
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDSigintSensorModes(ctx, repo.PostflightIDSigintSensorModesParams{
				ProjectID:      proj,
				ServiceAreas:   sig("sensor-service-areas"),
				PrimaryService: sig("sensor-primary-service"),
				Impact:         sig("sensor-impact"),
				DraftCampaign:  sig("sensor-draft-campaign"),
			})
		}, 4, "demo seed postflight: sigint sensor modes do not match the seeded examples"},
		{byProj(s.q.PostflightIDSigintMemberships), 9, "demo seed postflight: expected 9 sigint sensor memberships, found %d"},
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDSigintSignalMemberships(ctx, repo.PostflightIDSigintSignalMembershipsParams{ProjectID: proj, SignalID: sig("signal-api")})
		}, 2, "demo seed postflight: shared API signal has %d memberships, expected 2"},
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDSigintSensorMemberships(ctx, repo.PostflightIDSigintSensorMembershipsParams{ProjectID: proj, SensorID: sig("sensor-draft-campaign")})
		}, 0, "demo seed postflight: incomplete sigint sensor unexpectedly has %d memberships"},
		{func(ctx context.Context) (int64, error) {
			return s.q.PostflightIDSigintOrderedScore(ctx, repo.PostflightIDSigintOrderedScoreParams{
				ProjectID:    proj,
				Impact:       sig("sensor-impact"),
				SignalLow:    sig("signal-low"),
				SignalMedium: sig("signal-medium"),
				SignalHigh:   sig("signal-high"),
			})
		}, 1, "demo seed postflight: ordered-score sigint membership order is invalid"},
		{byProj(s.q.PostflightIDSigintNonConsecutiveOrder), 0, "demo seed postflight: %d sigint memberships have non-consecutive order"},
		{byProj(s.q.PostflightIDSigintDanglingMemberships), 0, "demo seed postflight: %d sigint memberships reference missing or deleted resources"},
	}
	return runPostflightIDChecks(ctx, rest)
}
