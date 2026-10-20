package demoseed

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
)

// postflight asserts the demo data landed and nothing leaked outside the demo
// org. Any failure rolls the whole Postgres seed back.
func (s *seeder) postflight(ctx context.Context) error {
	if err := s.postflightCore(ctx); err != nil {
		return err
	}
	return s.postflightIdentity(ctx)
}

// postflightCountCheck is one count query and the condition that fails it.
type postflightCountCheck struct {
	// what names the query in a read error.
	what  string
	count func() (int64, error)
	// fail returns the exception message when n violates the assert, or "".
	fail func(n int64) string
}

// postflightMustEqual fails unless n equals want, with msg formatted on n.
func postflightMustEqual(want int64, msg string) func(int64) string {
	return func(n int64) string {
		if n != want {
			return fmt.Sprintf(msg, n)
		}
		return ""
	}
}

// postflightMustBeZero fails when n is positive, with msg formatted on n.
func postflightMustBeZero(msg string) func(int64) string {
	return func(n int64) string {
		if n > 0 {
			return fmt.Sprintf(msg, n)
		}
		return ""
	}
}

// postflightMessage fails with a fixed message when cond holds on n.
func postflightMessage(cond func(int64) bool, msg string) func(int64) string {
	return func(n int64) string {
		if cond(n) {
			return msg
		}
		return ""
	}
}

func (s *seeder) runPostflightChecks(checks []postflightCountCheck) error {
	for _, c := range checks {
		n, err := c.count()
		if err != nil {
			return fmt.Errorf("demo seed postflight: count %s: %w", c.what, err)
		}
		if msg := c.fail(n); msg != "" {
			return errors.New("demo seed postflight: " + msg)
		}
	}
	return nil
}

// postflightCore covers postgres.sql lines 3139-3693: demo data landed, and
// nothing leaked outside the demo org. It also logs the run's summary line.
func (s *seeder) postflightCore(ctx context.Context) error {
	org := s.orgID()
	proj := s.projectID()
	// The roster's user id prefix as a LIKE pattern ('_' is a wildcard).
	userLike := strings.ReplaceAll(s.spec.UserPrefix, "_", `\_`) + "%"

	var chatCount, findingCount, memberCount, toolCount int64
	read := func(what string, dst *int64, f func() (int64, error)) postflightCountCheck {
		return postflightCountCheck{
			what: what,
			count: func() (int64, error) {
				n, err := f()
				*dst = n
				return n, err
			},
			fail: func(int64) string { return "" },
		}
	}
	byOrg := func(f func(context.Context, string) (int64, error)) func() (int64, error) {
		return func() (int64, error) { return f(ctx, org) }
	}
	byProject := func(f func(context.Context, uuid.UUID) (int64, error)) func() (int64, error) {
		return func() (int64, error) { return f(ctx, proj) }
	}
	flag := func(what string, f func(context.Context, string) (bool, error), msg string) postflightCountCheck {
		return postflightCountCheck{
			what: what,
			count: func() (int64, error) {
				bad, err := f(ctx, org)
				if bad {
					return 1, err
				}
				return 0, err
			},
			fail: postflightMessage(func(n int64) bool { return n != 0 }, msg),
		}
	}

	checks := []postflightCountCheck{
		{
			what: "inference hook messages",
			count: func() (int64, error) {
				return s.q.PostflightCountInferenceHookMessages(ctx, repo.PostflightCountInferenceHookMessagesParams{
					ProjectID: proj,
					ChatID:    s.det("anthropic-inference-chat"),
				})
			},
			fail: postflightMustEqual(3, "expected 3 inference hook messages, found %d"),
		},
		read("chats", &chatCount, byOrg(s.q.PostflightCountChats)),
		read("risk findings", &findingCount, byOrg(s.q.PostflightCountFindings)),
		read("members", &memberCount, byOrg(s.q.PostflightCountMembers)),
		{
			what:  "setup tasks",
			count: byOrg(s.q.PostflightCountSetupTasks),
			fail:  postflightMustEqual(11, "expected 11 setup task selections, found %d"),
		},
		flag("security onboarding", s.q.PostflightSecurityOnboardingInvalid, "expected customized Security onboarding selection"),
		flag("onboarding stack", s.q.PostflightOnboardingStackInvalid, "expected the recorded onboarding stack"),
		{
			what:  "network_ingress feature",
			count: byOrg(s.q.PostflightCountNetworkIngressFeature),
			fail:  postflightMessage(func(n int64) bool { return n != 0 }, "network_ingress entitlement remains"),
		},
		{
			what:  "network ingresses",
			count: byOrg(s.q.PostflightCountNetworkIngresses),
			fail:  postflightMessage(func(n int64) bool { return n != 0 }, "network ingress rows remain"),
		},
		read("http tools", &toolCount, byProject(s.q.PostflightCountHTTPTools)),

		// Killswitch aggregate counts are exact: six headers, two successor
		// versions, eight complete selected snapshots, one expiry marker, and
		// one completed operation/audit event for every lifecycle mutation.
		{
			what:  "killswitch prescriptions",
			count: byOrg(s.q.PostflightCountKillswitchPrescriptions),
			fail:  postflightMustEqual(6, "expected 6 killswitch prescriptions, found %d"),
		},
		{
			what:  "killswitch versions",
			count: byOrg(s.q.PostflightCountKillswitchVersions),
			fail:  postflightMustEqual(8, "expected 8 killswitch versions, found %d"),
		},
		{
			what:  "killswitch resource snapshots",
			count: byOrg(s.q.PostflightCountKillswitchResources),
			fail:  postflightMustEqual(8, "expected 8 killswitch resource snapshots, found %d"),
		},
		{
			what:  "killswitch operations",
			count: byOrg(s.q.PostflightCountKillswitchOperations),
			fail:  postflightMustEqual(8, "expected 8 killswitch operations, found %d"),
		},
		{
			what:  "killswitch expiry markers",
			count: byOrg(s.q.PostflightCountKillswitchExpiryEvents),
			fail:  postflightMustEqual(1, "expected 1 killswitch expiry marker, found %d"),
		},
		// current_version always names the newest immutable version. Only
		// historical versions are superseded, and every version retains its
		// activation time.
		{
			what:  "killswitch current versions",
			count: byOrg(s.q.PostflightCountKillswitchInvalidCurrentVersion),
			fail:  postflightMustBeZero("%d killswitch headers have an invalid current version"),
		},
		{
			what:  "killswitch lifecycle timestamps",
			count: byOrg(s.q.PostflightCountKillswitchInvalidLifecycle),
			fail:  postflightMustBeZero("%d killswitch versions violate lifecycle timestamps"),
		},
		// Immediate versions have no explicit start; the only non-NULL start
		// is the future scheduled window. Every version has a historical
		// activation time.
		{
			what:  "killswitch start semantics",
			count: byOrg(s.q.PostflightKillswitchStartSemanticsInvalid),
			fail:  postflightMessage(func(n int64) bool { return n > 0 }, "killswitch start semantics changed"),
		},
		// An all-resource snapshot has no children. Every selected version has
		// a complete non-empty snapshot, including copied snapshots on lift.
		{
			what:  "killswitch resource cardinality",
			count: byOrg(s.q.PostflightCountKillswitchInvalidResourceCardinality),
			fail:  postflightMustBeZero("%d killswitch versions have invalid resource cardinality"),
		},
		// Principal and resource keys are canonical records owned by this org.
		{
			what:  "killswitch principals",
			count: byOrg(s.q.PostflightCountKillswitchInvalidPrincipal),
			fail:  postflightMustBeZero("%d killswitch prescriptions have an invalid principal or contract"),
		},
		{
			what:  "killswitch resources",
			count: byOrg(s.q.PostflightCountKillswitchInvalidResources),
			fail:  postflightMustBeZero("%d killswitch resources are not live canonical MCP servers"),
		},
		// Database-time status projection: three active, one scheduled, one
		// lifted, and one expired current aggregate. The two Amara rows
		// overlap because one selected-server interval intersects one dynamic
		// all-server interval.
		{
			what:  "killswitch status coverage",
			count: byOrg(s.q.PostflightKillswitchStatusCoverageInvalid),
			fail:  postflightMessage(func(n int64) bool { return n > 0 }, "killswitch current status coverage changed"),
		},
		{
			what: "overlapping active killswitches",
			count: func() (int64, error) {
				return s.q.PostflightCountKillswitchOverlappingActive(ctx, repo.PostflightCountKillswitchOverlappingActiveParams{
					OrgID:        org,
					PrincipalKey: s.userID(demoUsers[0].handle),
					ResourceKey:  s.det("mcpserver-support").String(),
				})
			},
			fail: postflightMustEqual(2, "expected 2 overlapping active killswitches, found %d"),
		},
		// Completed replay receipts use the production response envelope,
		// point to the exact committed version/state, expire 30 days after
		// claim, and belong to a current organization member.
		{
			what:  "killswitch operation receipts",
			count: byOrg(s.q.PostflightCountKillswitchInvalidOperations),
			fail:  postflightMustBeZero("%d killswitch operation receipts are invalid"),
		},
		// Every mutation has one canonical audit action joined by
		// operation_id. The expiry marker has one separate history-only
		// expiry event.
		{
			what:  "killswitch lifecycle audits",
			count: byOrg(s.q.PostflightCountKillswitchLifecycleAudits),
			fail:  postflightMustEqual(8, "expected 8 killswitch lifecycle audits, found %d"),
		},
		{
			what:  "invalid killswitch lifecycle audits",
			count: byOrg(s.q.PostflightCountKillswitchInvalidLifecycleAudits),
			fail:  postflightMustBeZero("%d killswitch lifecycle audits are invalid"),
		},
		{
			what:  "killswitch expiry audits",
			count: byOrg(s.q.PostflightCountKillswitchExpiryAudits),
			fail:  postflightMustEqual(1, "expected 1 matching killswitch expiry audit, found %d"),
		},
	}
	if err := s.runPostflightChecks(checks); err != nil {
		return err
	}

	if chatCount < bulkChats {
		return fmt.Errorf("demo seed postflight: expected >= %d chats, found %d", bulkChats, chatCount)
	}
	// Floor, not an equality: the count follows from riskType()'s weighted
	// draw over bulkChats. Well under the ~110 the weights produce, but high
	// enough to catch the draw collapsing to nothing.
	if findingCount < 90 {
		return fmt.Errorf("demo seed postflight: expected >= 90 risk findings, found %d", findingCount)
	}

	checks = []postflightCountCheck{
		{
			what: "shadow policy grants",
			count: func() (int64, error) {
				return s.q.PostflightCountShadowPolicyGrants(ctx, repo.PostflightCountShadowPolicyGrantsParams{
					OrgID:    org,
					PolicyID: s.policySM().String(),
				})
			},
			fail: postflightMustEqual(2, "expected 2 shadow policy target grants, found %d"),
		},
		// The Watchdog scores each signal from its findings' policy, so a
		// rotation that collapsed onto one policy would render every signal at
		// one severity.
		{
			what:  "finding policies",
			count: byOrg(s.q.PostflightCountFindingPolicies),
			fail: func(n int64) string {
				if n < 5 {
					return fmt.Sprintf("findings span only %d policies; the Watchdog severity spread needs at least 5", n)
				}
				return ""
			},
		},
		// Suppressed findings back the exclusion and Dismissed surfaces. Each
		// mechanism must be represented, or a broken rendering path stays
		// invisible.
		{
			what:  "excluded findings",
			count: byOrg(s.q.PostflightCountExcludedFindings),
			fail:  postflightMessage(func(n int64) bool { return n == 0 }, "no findings suppressed by an exclusion rule"),
		},
		{
			what:  "dismissed findings",
			count: byOrg(s.q.PostflightCountDismissedFindings),
			fail:  postflightMessage(func(n int64) bool { return n == 0 }, "no dismissed or swept findings"),
		},
		// An exclusion whose match_value no finding carries would make the
		// suppressed rows above untraceable to the rule that claims them.
		{
			what:  "idle exact exclusions",
			count: byOrg(s.q.PostflightCountIdleExactExclusions),
			fail:  postflightMustBeZero("%d exact exclusions suppress no matching finding"),
		},
	}
	if err := s.runPostflightChecks(checks); err != nil {
		return err
	}

	if memberCount != int64(len(demoUsers)) {
		return fmt.Errorf("demo seed postflight: expected %d members, found %d", len(demoUsers), memberCount)
	}
	if toolCount != int64(len(toolNames)) {
		return fmt.Errorf("demo seed postflight: expected %d http tools, found %d", len(toolNames), toolCount)
	}

	checks = []postflightCountCheck{
		// Both checks guard an opaque string that fails silently: an
		// unrecognized source disables scanning for the policy ('regex'
		// shipped that way once), and a custom_rule_id with no rule row makes
		// the policy a no-op. The source list mirrors validateSources
		// (internal/risk/impl.go); the seed inserts past it.
		{
			what:  "policies with unknown sources",
			count: byOrg(s.q.PostflightCountPoliciesUnknownSource),
			fail:  postflightMustBeZero("%d risk policies carry an unrecognized source"),
		},
		{
			what:  "policy custom rule ids",
			count: byOrg(s.q.PostflightCountPolicyMissingCustomRules),
			fail:  postflightMustBeZero("%d policy custom_rule_ids have no rule row"),
		},
		{
			what: "foreign-org chats",
			count: func() (int64, error) {
				return s.q.PostflightCountForeignOrgChats(ctx, repo.PostflightCountForeignOrgChatsParams{ProjectID: proj, OrgID: org})
			},
			fail: postflightMustBeZero("%d chats on demo projects belong to another org"),
		},
		{
			what:  "ownerless chats",
			count: byOrg(s.q.PostflightCountOwnerlessChats),
			fail:  postflightMustBeZero("%d chats have no owner (sessions would render as anonymous)"),
		},
		// Every table that carries both the demo org id and a project id must
		// agree with the demo constants: a mismatch means an insert escaped
		// its scoping.
		{
			what: "foreign-project rows",
			count: func() (int64, error) {
				return s.q.PostflightCountForeignProjectRows(ctx, repo.PostflightCountForeignProjectRowsParams{OrgID: org, ProjectID: proj})
			},
			fail: postflightMustBeZero("%d demo-org rows reference non-demo projects"),
		},
		// The gateway is only a gateway if its members survived the reseed,
		// and an endpoint is what gives it a URL — without either the MCP
		// pages render it as an empty shell.
		{
			what:  "gateway members",
			count: byProject(s.q.PostflightCountGatewayMembers),
			fail:  postflightMustEqual(4, "expected 4 gateway members, found %d"),
		},
		{
			what:  "removed gateway members",
			count: byProject(s.q.PostflightCountRemovedGatewayMembers),
			fail:  postflightMustEqual(1, "expected 1 removed gateway member, found %d"),
		},
		{
			what:  "mcp skill links",
			count: byProject(s.q.PostflightCountMCPSkillLinks),
			fail:  postflightMustEqual(5, "expected 5 skills served over MCP, found %d"),
		},
		{
			what:  "hosted toolset wrappers",
			count: byProject(s.q.PostflightCountUnwrappedToolsets),
			fail:  postflightMustBeZero("%d hosted toolsets lack a matching canonical wrapper and endpoint"),
		},
		{
			what:  "gateway endpoints",
			count: byProject(s.q.PostflightCountGatewayEndpoints),
			fail:  postflightMustEqual(1, "expected 1 gateway endpoint, found %d"),
		},
		// A member whose server lost its backend can never be dispatched to,
		// so it would sit in the gateway's table permanently unavailable.
		{
			what:  "unservable gateway members",
			count: byProject(s.q.PostflightCountUnservableGatewayMembers),
			fail:  postflightMustBeZero("%d gateway members are not servable"),
		},
		{
			what: "active session quarantines",
			count: func() (int64, error) {
				return s.q.PostflightCountActiveQuarantines(ctx, repo.PostflightCountActiveQuarantinesParams{OrgID: org, ProjectID: proj})
			},
			fail: postflightMustEqual(1, "expected 1 active session quarantine, found %d"),
		},
		{
			what: "session quarantine open audits",
			count: func() (int64, error) {
				return s.q.PostflightCountQuarantineOpenAudits(ctx, repo.PostflightCountQuarantineOpenAuditsParams{OrgID: org, ProjectID: proj})
			},
			fail: postflightMustEqual(1, "expected 1 matching session quarantine open audit record, found %d"),
		},
		// Global (non-org-scoped) tables must only carry rows for the demo
		// roster: memberships/accounts/devices reference the roster's user ids
		// exclusively.
		{
			what: "foreign-user rows",
			count: func() (int64, error) {
				return s.q.PostflightCountForeignUserRows(ctx, repo.PostflightCountForeignUserRowsParams{OrgID: org, UserLike: userLike})
			},
			fail: postflightMustBeZero("%d demo-org rows reference non-demo users"),
		},
		// The MDM fleet: seven devices under one integration. Counted because
		// the coverage buckets are the point — a rerun that dropped the
		// unresolved or agentless rows would leave the widgets showing a
		// uniformly healthy fleet.
		{
			what:  "mdm devices",
			count: byOrg(s.q.PostflightCountMDMDevices),
			fail:  postflightMustEqual(7, "expected 7 mdm devices, found %d"),
		},
		{
			what:  "device integration configs",
			count: byOrg(s.q.PostflightCountDeviceIntegrationConfigs),
			fail:  postflightMustEqual(1, "expected 1 device integration config, found %d"),
		},
		// Personal accounts are the reading the governance note exists for;
		// one surviving row would make the pattern look like an edge case.
		{
			what:  "personal accounts",
			count: byOrg(s.q.PostflightCountPersonalAccounts),
			fail:  postflightMustEqual(3, "expected 3 personal accounts, found %d"),
		},
	}
	if err := s.runPostflightChecks(checks); err != nil {
		return err
	}

	s.logger.InfoContext(ctx, fmt.Sprintf("demo seed ok: %d chats, %d findings, %d members, %d tools", chatCount, findingCount, memberCount, toolCount))
	return nil
}
