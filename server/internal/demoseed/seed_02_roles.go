package demoseed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
)

// rolesCustomRole is one custom organization role. members are user handles
// (see seeder.userID); agents are managed-agent names (see seeder.det).
type rolesCustomRole struct {
	slug        string
	name        string
	description string
	scopes      []string
	members     []string
	agents      []string
}

// Custom roles. Real organizations do not run on Admin and Member alone: the
// roles page is only legible with the shapes people actually create — a
// read-only audience, a per-team grant, a narrow escalation. Ids are
// generated, never literal, so each tenant's rows stay its own.
var rolesCustomRoles = []rolesCustomRole{
	{
		slug: "session-reviewer", name: "Session Reviewer",
		description: "Reads chat transcripts across the organization for quality review.",
		scopes:      []string{"chat:read"},
		members:     []string{"hana", "jonas"},
		agents:      []string{},
	},
	{
		slug: "tool-log-viewer", name: "Tool Log Viewer",
		description: "Reads one project's tool-call logs, including arguments and results, without organization admin access.",
		scopes:      []string{"log:read"},
		members:     []string{"mateo"},
		agents:      []string{},
	},
	{
		slug: "collaborator", name: "Collaborator",
		description: "Builds and ships MCP servers, skills and assistants, without organization settings.",
		scopes: []string{"org:read", "project:read", "project:write", "mcp:read",
			"mcp:write", "mcp:connect", "skill:read", "skill:write", "plugin:write",
			"assistant:read", "assistant:write", "environment:read", "agent:read"},
		members: []string{"jonas"},
		agents:  []string{},
	},
	{
		slug: "engineer", name: "Engineer",
		description: "Creates and configures MCP servers in this project.",
		scopes:      []string{"mcp:read", "mcp:write", "mcp:connect"},
		members:     []string{"priya", "mateo"},
		agents:      []string{},
	},
	{
		slug: "automation-agent", name: "Automation Agent",
		description: "Held by unattended agents, not people: authorizes an agent to act.",
		scopes:      []string{"agent:read", "agent:authorize"},
		members:     []string{},
		agents:      []string{"managed-agent-1", "managed-agent-2"},
	},
	{
		slug: "analyst", name: "Analyst",
		description: "Read-only across servers, skills, assistants and sessions. No configuration changes.",
		scopes: []string{"org:read", "project:read", "mcp:read", "mcp:connect",
			"skill:read", "assistant:read", "chat:read"},
		members: []string{"amara", "hana"},
		agents:  []string{},
	},
	{
		slug: "read-only-tools", name: "Read-only Tools",
		description: "Connects to every server, but only for tools annotated read-only.",
		scopes:      []string{"mcp:connect"},
		members:     []string{"amara"},
		agents:      []string{"managed-agent-1"},
	},
	{
		slug: "environment-manager", name: "Environment Manager",
		description: "Manages environments and the credentials they hold.",
		scopes: []string{"org:read", "project:read", "environment:read",
			"environment:write", "mcp:read"},
		members: []string{"lucas"},
		agents:  []string{},
	},
	{
		slug: "contractors", name: "Contractors",
		description: "Outside contractors. Holds no access of its own; it blocks GitHub.",
		scopes:      []string{},
		members:     []string{"priya"},
		agents:      []string{},
	},
	{
		slug: "support-desk", name: "Support Desk",
		description: "Connects to Acme Support Tools with every tool, and to Slack for read-only tools only.",
		scopes:      []string{},
		members:     []string{"hana"},
		agents:      []string{},
	},
	{
		slug: "temporary-escalation", name: "Temporary Escalation",
		description: "Elevated access granted for a fixed period and reviewed each quarter.",
		scopes: []string{"org:read", "org:admin", "project:read", "project:write",
			"mcp:read", "mcp:write", "mcp:connect", "environment:read",
			"skill:read", "skill:write", "assistant:read", "assistant:write",
			"agent:read", "agent:write", "chat:read"},
		members: []string{"priya", "mateo"},
		agents:  []string{},
	},
}

// rolesSlackMember is one retained Slack directory membership. email is a
// user handle (see seeder.email), or "" for none.
type rolesSlackMember struct {
	n           int
	teamID      string
	displayName string
	email       string
	status      string
	memberType  string
}

var rolesSlackMembers = []rolesSlackMember{
	{1, "T0DEMO0001", "Amara Okafor", "amara", "active", "person"},
	{2, "T0DEMO0001", "Jonas Lindqvist", "jonas", "active", "person"},
	{3, "T0DEMO0001", "Priya Raman", "", "active", "guest"},
	{4, "T0DEMO0001", "Release Helper", "", "active", "bot"},
	{5, "T0DEMO0001", "Mateo Alvarez", "mateo", "unknown", "person"},
	{6, "T0DEMO0002", "Amara Okafor", "amara", "active", "person"},
	{7, "T0DEMO0002", "Hana Sato", "hana", "deactivated", "person"},
	{8, "T0DEMO0002", "Taylor Reed", "", "invited", "unknown"},
	{9, "T0DEMO0002", "Lucas Meyer", "", "active", "single_channel_guest"},
	{10, "T0DEMO0001", "Amara Okafor (guest)", "", "deactivated", "guest"},
}

// rolesSlackMappings pairs a Slack membership n with the 1-based demoUsers
// index of the person it was mapped to.
var rolesSlackMappings = [][2]int{{1, 1}, {3, 3}, {5, 4}, {6, 1}, {7, 5}, {10, 1}}

// rolesMDMDevice is one device in the Jamf inventory. owner is the handle its
// email is built from; hasUser is false for the device whose address resolves
// to nobody.
type rolesMDMDevice struct {
	n          int
	serial     string
	hostname   string
	osName     string
	osVersion  string
	owner      string
	hasUser    bool
	checkInAgo string
}

var rolesMDMDevices = []rolesMDMDevice{
	{1, "C02DEMO0001", "amara-mbp", "macOS", "15.3", "amara", true, "2 hours"},
	{2, "C02DEMO0002", "jonas-mbp", "macOS", "15.2", "jonas", true, "5 hours"},
	{3, "C02DEMO0003", "priya-mbp", "macOS", "14.7", "priya", true, "1 day"},
	// The contractor's machine: enrolled, but the agent stopped reporting.
	{4, "C02DEMO0004", "mateo-mbp", "macOS", "14.6", "mateo", true, "6 days"},
	// The one Windows machine, and one of the two with no agent at all.
	{5, "WINDEMO0005", "HANA-WIN11", "Windows", "11 23H2", "hana", true, "8 hours"},
	{6, "C02DEMO0006", "lucas-mbp", "macOS", "15.3", "lucas", true, "3 hours"},
	// A spare laptop the MDM reports against an address no member holds: the
	// unresolved-email bucket, which is a real state and not an error.
	{7, "C02DEMO0007", "acme-spare-01", "macOS", "14.4", "contractor.pool", false, "4 days"},
}

// seedRoles: Custom roles, plugins, Slack and directory sync, devices, AI scan targets, billing contract.
func (s *seeder) seedRoles(ctx context.Context) error {
	org := s.orgID()

	if err := s.q.RolesDeleteCustomRoleGrants(ctx, org); err != nil {
		return fmt.Errorf("delete demo custom role grants: %w", err)
	}
	if err := s.q.RolesDeleteAgentRoleAssignments(ctx, org); err != nil {
		return fmt.Errorf("delete demo agent role assignments: %w", err)
	}
	if err := s.q.RolesDeleteOrganizationRoles(ctx, org); err != nil {
		return fmt.Errorf("delete demo organization roles: %w", err)
	}

	for _, role := range rolesCustomRoles {
		roleID, err := s.q.RolesInsertCustomRole(ctx, repo.RolesInsertCustomRoleParams{
			OrganizationID: org,
			Slug:           role.slug,
			Name:           role.name,
			Description:    role.description,
		})
		if err != nil {
			return fmt.Errorf("insert demo custom role %s: %w", role.slug, err)
		}
		roleURN := "role:organization:" + roleID.String()

		if err := s.q.RolesInsertCustomRoleGrants(ctx, repo.RolesInsertCustomRoleGrantsParams{
			OrganizationID: org,
			PrincipalUrn:   roleURN,
			Scopes:         role.scopes,
			ProjectID:      s.projectID().String(),
		}); err != nil {
			return fmt.Errorf("insert demo custom role grants %s: %w", role.slug, err)
		}

		for _, handle := range role.members {
			if err := s.q.RolesInsertRoleAssignment(ctx, repo.RolesInsertRoleAssignmentParams{
				OrganizationID: org,
				WorkosUserID:   "workos_" + s.userID(handle),
				UserID:         s.userID(handle),
				RoleUrn:        roleURN,
			}); err != nil {
				return fmt.Errorf("insert demo custom role assignment %s: %w", role.slug, err)
			}
		}

		// Agent members of a role. Automation Agent carries agent scopes no
		// agent can hold at runtime, so the role editor marks them; Read-only
		// Tools is the case that actually widens an agent, and the suspended
		// agent shows a membership that is kept rather than dropped.
		for _, agent := range role.agents {
			if err := s.q.RolesInsertAgentRoleAssignment(ctx, repo.RolesInsertAgentRoleAssignmentParams{
				OrganizationID: org,
				AgentID:        s.det(agent),
				RoleUrn:        roleURN,
			}); err != nil {
				return fmt.Errorf("insert demo agent role assignment %s: %w", role.slug, err)
			}
		}
	}

	// Show the completed one-time onboarding result, not a pending repair job.
	// Role plugins stay role-only and install by default; the Default plugin
	// remains Everyone and is available for members to turn on.
	// Matching server content is populated after the servers exist below.
	defaultPluginID, err := s.q.RolesInsertDefaultPlugin(ctx, repo.RolesInsertDefaultPluginParams{
		OrganizationID: org,
		ProjectID:      s.projectID(),
	})
	if err != nil {
		return fmt.Errorf("insert demo default plugin: %w", err)
	}
	if err := s.q.RolesInsertEveryonePluginAssignment(ctx, repo.RolesInsertEveryonePluginAssignmentParams{
		PluginID:       defaultPluginID,
		OrganizationID: org,
	}); err != nil {
		return fmt.Errorf("assign demo default plugin: %w", err)
	}

	roles, err := s.q.RolesListRolesForPlugins(ctx, org)
	if err != nil {
		return fmt.Errorf("list demo roles for plugins: %w", err)
	}
	for _, role := range roles {
		pluginID, err := s.q.RolesFindPluginBySlug(ctx, repo.RolesFindPluginBySlugParams{
			OrganizationID: org,
			ProjectID:      s.projectID(),
			Slug:           role.Slug,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			pluginID, err = s.q.RolesInsertRolePlugin(ctx, repo.RolesInsertRolePluginParams{
				OrganizationID: org,
				ProjectID:      s.projectID(),
				Name:           role.Name,
				Slug:           role.Slug,
			})
		}
		if err != nil {
			return fmt.Errorf("resolve demo role plugin %s: %w", role.Slug, err)
		}
		if err := s.q.RolesInsertRolePluginAssignment(ctx, repo.RolesInsertRolePluginAssignmentParams{
			PluginID:       pluginID,
			OrganizationID: org,
			PrincipalUrn:   role.RoleUrn,
		}); err != nil {
			return fmt.Errorf("assign demo role plugin %s: %w", role.Slug, err)
		}
	}

	if err := s.q.RolesSetReadOnlyDisposition(ctx, org); err != nil {
		return fmt.Errorf("set demo read-only disposition: %w", err)
	}

	if err := s.seedRolesSlack(ctx); err != nil {
		return err
	}
	if err := s.seedRolesDirectory(ctx); err != nil {
		return err
	}
	if err := s.seedRolesAccountsAndDevices(ctx); err != nil {
		return err
	}

	if err := s.q.RolesUpsertBillingContract(ctx, org); err != nil {
		return fmt.Errorf("upsert demo billing contract: %w", err)
	}
	return nil
}

func (s *seeder) seedRolesSlack(ctx context.Context) error {
	org := s.orgID()

	if err := s.q.RolesDeleteSlackMemberships(ctx, org); err != nil {
		return fmt.Errorf("delete demo slack memberships: %w", err)
	}
	if err := s.q.RolesDeleteSlackConnections(ctx, org); err != nil {
		return fmt.Errorf("delete demo slack connections: %w", err)
	}
	if err := s.q.RolesInsertSlackConnections(ctx, repo.RolesInsertSlackConnectionsParams{
		Conn1ID:        s.det("slackconn-1"),
		OrganizationID: org,
		Conn2ID:        s.det("slackconn-2"),
	}); err != nil {
		return fmt.Errorf("insert demo slack connections: %w", err)
	}
	if err := s.q.RolesMarkSlackConnectionsSynced(ctx, org); err != nil {
		return fmt.Errorf("mark demo slack connections synced: %w", err)
	}

	for _, m := range rolesSlackMembers {
		email := pgtype.Text{String: "", Valid: false}
		if m.email != "" {
			email = pgtype.Text{String: s.email(m.email), Valid: true}
		}
		lastSeenAgo := "3 days"
		if m.n == 5 {
			lastSeenAgo = "5 days"
		}
		if err := s.q.RolesInsertSlackMembership(ctx, repo.RolesInsertSlackMembershipParams{
			ID:             s.rolesSlackMemberID(m.n),
			OrganizationID: org,
			SlackTeamID:    m.teamID,
			SlackUserID:    fmt.Sprintf("U0DEMO%05d", m.n),
			DisplayName:    m.displayName,
			Email:          email,
			Status:         m.status,
			MemberType:     m.memberType,
			LastSeenAgo:    lastSeenAgo,
		}); err != nil {
			return fmt.Errorf("insert demo slack membership %d: %w", m.n, err)
		}
	}

	// Synthetic admin decisions remain visible in disconnected directory
	// history.
	for _, mapping := range rolesSlackMappings {
		n, person := mapping[0], mapping[1]
		if err := s.q.RolesInsertSlackIdentityMapping(ctx, repo.RolesInsertSlackIdentityMappingParams{
			ID:             s.det("slackmapping-" + strconv.Itoa(n)),
			OrganizationID: org,
			UserID:         s.userID(demoUsers[person-1].handle),
			MembershipID:   s.rolesSlackMemberID(n),
		}); err != nil {
			return fmt.Errorf("insert demo slack identity mapping %d: %w", n, err)
		}
	}
	if err := s.q.RolesMarkSlackMappingConflicts(ctx, org); err != nil {
		return fmt.Errorf("mark demo slack mapping conflicts: %w", err)
	}
	return nil
}

func (s *seeder) rolesSlackMemberID(n int) uuid.UUID {
	return s.det("slackmember-" + strconv.Itoa(n))
}

// seedRolesDirectory writes directory profiles: they feed spend-rule
// audiences, enrollment attributes, and mirror the user.attributes.* identity
// on the ClickHouse telemetry.
func (s *seeder) seedRolesDirectory(ctx context.Context) error {
	org := s.orgID()

	if err := s.q.RolesDeleteDirectoryReviewItems(ctx, org); err != nil {
		return fmt.Errorf("delete demo directory review items: %w", err)
	}
	if err := s.q.RolesDeleteDirectoryReviewOrgState(ctx, org); err != nil {
		return fmt.Errorf("delete demo directory review state: %w", err)
	}
	if err := s.q.RolesDeleteDirectoryReviewOptOuts(ctx, org); err != nil {
		return fmt.Errorf("delete demo directory review opt-outs: %w", err)
	}
	if err := s.q.RolesDeleteDirectoryRoleMappings(ctx, org); err != nil {
		return fmt.Errorf("delete demo directory role mappings: %w", err)
	}
	if err := s.q.RolesDeleteDirectoryGroupMemberships(ctx, org); err != nil {
		return fmt.Errorf("delete demo directory group memberships: %w", err)
	}
	if err := s.q.RolesDeleteDirectoryUsers(ctx, org); err != nil {
		return fmt.Errorf("delete demo directory users: %w", err)
	}
	if err := s.q.RolesDeleteDirectoryGroups(ctx, org); err != nil {
		return fmt.Errorf("delete demo directory groups: %w", err)
	}

	teams := make([]string, 0, len(demoUsers))
	for _, u := range demoUsers {
		teams = append(teams, u.team)
	}
	if err := s.q.RolesInsertDirectoryGroups(ctx, repo.RolesInsertDirectoryGroupsParams{
		OrganizationID: org,
		GroupPrefix:    s.spec.GroupPrefix,
		Teams:          teams,
	}); err != nil {
		return fmt.Errorf("insert demo directory groups: %w", err)
	}

	for _, u := range demoUsers {
		dirUserID := "demo_dir_" + s.userID(u.handle)
		firstName, lastName := splitDemoName(u.name)
		if err := s.q.RolesInsertDirectoryUser(ctx, repo.RolesInsertDirectoryUserParams{
			OrganizationID:        org,
			UserID:                conv.ToPGText(s.userID(u.handle)),
			WorkosDirectoryUserID: dirUserID,
			Email:                 s.email(u.handle),
			FirstName:             conv.ToPGText(firstName),
			LastName:              conv.ToPGText(lastName),
			DivisionName:          u.division,
			DepartmentName:        u.department,
			JobTitle:              u.title,
			EmployeeType:          u.empType,
			CostCenterName:        u.costCenter,
		}); err != nil {
			return fmt.Errorf("insert demo directory user %s: %w", u.handle, err)
		}
		if err := s.q.RolesInsertDirectoryGroupMembership(ctx, repo.RolesInsertDirectoryGroupMembershipParams{
			WorkosDirectoryUserID: dirUserID,
			OrganizationID:        org,
			Team:                  u.team,
		}); err != nil {
			return fmt.Errorf("insert demo directory group membership %s: %w", u.handle, err)
		}
	}

	// Directory people who have not signed in, including one address the
	// directory reports twice.
	for _, p := range demoDirectoryPeople {
		dirUserID := "demo_dir_" + s.userID(p.handle)
		if err := s.q.RolesInsertDirectoryUser(ctx, repo.RolesInsertDirectoryUserParams{
			OrganizationID:        org,
			UserID:                conv.ToPGTextEmpty(""),
			WorkosDirectoryUserID: dirUserID,
			Email:                 s.email(p.emailHandle),
			FirstName:             conv.ToPGText(p.firstName),
			LastName:              conv.ToPGText(p.lastName),
			DivisionName:          "",
			DepartmentName:        p.department,
			JobTitle:              p.title,
			EmployeeType:          "full-time",
			CostCenterName:        "",
		}); err != nil {
			return fmt.Errorf("insert demo directory person %s: %w", p.handle, err)
		}
		if err := s.q.RolesInsertDirectoryGroupMembership(ctx, repo.RolesInsertDirectoryGroupMembershipParams{
			WorkosDirectoryUserID: dirUserID,
			OrganizationID:        org,
			Team:                  p.team,
		}); err != nil {
			return fmt.Errorf("insert demo directory group membership %s: %w", p.handle, err)
		}
	}

	// Directory role mappings: Infra grants two roles from one source, alongside
	// attribute mappings, on top of what members already hold directly.
	for _, slug := range []string{"collaborator", "analyst"} {
		if err := s.q.RolesInsertGroupRoleMapping(ctx, repo.RolesInsertGroupRoleMappingParams{
			OrganizationID: org,
			GroupName:      "Infra",
			RoleSlug:       slug,
		}); err != nil {
			return fmt.Errorf("insert demo group role mapping %s: %w", slug, err)
		}
	}
	if err := s.q.RolesInsertAttributeRoleMapping(ctx, repo.RolesInsertAttributeRoleMappingParams{
		OrganizationID: org,
		AttributeKey:   "department_name",
		AttributeValue: "Support Engineering",
		RoleSlug:       "analyst",
	}); err != nil {
		return fmt.Errorf("insert demo department role mapping: %w", err)
	}
	// A group deleted from the directory keeps its mapping, which grants
	// nothing; the mappings page lists it as a missing source.
	if err := s.q.RolesInsertDeletedDirectoryGroup(ctx, repo.RolesInsertDeletedDirectoryGroupParams{
		OrganizationID: org,
		GroupPrefix:    s.spec.GroupPrefix,
		Slug:           "legacy-platform",
		Name:           "Legacy Platform",
	}); err != nil {
		return fmt.Errorf("insert demo deleted directory group: %w", err)
	}
	if err := s.q.RolesInsertGroupRoleMapping(ctx, repo.RolesInsertGroupRoleMappingParams{
		OrganizationID: org,
		GroupName:      "Legacy Platform",
		RoleSlug:       "analyst",
	}); err != nil {
		return fmt.Errorf("insert demo deleted group role mapping: %w", err)
	}
	// Contractors arrive through the directory, so Check Access on GitHub can
	// name the mapping that put a person in the role that blocks them.
	if err := s.q.RolesInsertAttributeRoleMapping(ctx, repo.RolesInsertAttributeRoleMappingParams{
		OrganizationID: org,
		AttributeKey:   "employee_type",
		AttributeValue: "contractor",
		RoleSlug:       "contractors",
	}); err != nil {
		return fmt.Errorf("insert demo contractor role mapping: %w", err)
	}
	return s.seedRolesDirectoryReview(ctx)
}

// seedRolesDirectoryReview starts directory review after the groups above
// exist, then raises one change of each kind for the SSO tab's review list:
// a group first seen since, a new value of a mapped attribute key, and the
// deleted group that still has a mapping.
func (s *seeder) seedRolesDirectoryReview(ctx context.Context) error {
	org := s.orgID()

	if err := s.q.RolesInsertDirectoryReviewOrgState(ctx, org); err != nil {
		return fmt.Errorf("insert demo directory review state: %w", err)
	}
	if err := s.q.RolesInsertNewDirectoryGroup(ctx, repo.RolesInsertNewDirectoryGroupParams{
		OrganizationID: org,
		GroupPrefix:    s.spec.GroupPrefix,
		Slug:           "field-engineering",
		Name:           "Field Engineering",
	}); err != nil {
		return fmt.Errorf("insert demo new directory group: %w", err)
	}
	for _, handle := range []string{"priya", "mateo"} {
		if err := s.q.RolesInsertDirectoryGroupMembership(ctx, repo.RolesInsertDirectoryGroupMembershipParams{
			WorkosDirectoryUserID: "demo_dir_" + s.userID(handle),
			OrganizationID:        org,
			Team:                  "Field Engineering",
		}); err != nil {
			return fmt.Errorf("insert demo new directory group membership %s: %w", handle, err)
		}
	}

	for _, item := range []repo.RolesInsertDirectoryReviewGroupItemParams{
		{OrganizationID: org, Kind: "new_group", GroupName: "Field Engineering"},
		{OrganizationID: org, Kind: "source_missing", GroupName: "Legacy Platform"},
	} {
		if err := s.q.RolesInsertDirectoryReviewGroupItem(ctx, item); err != nil {
			return fmt.Errorf("insert demo directory review item for %s: %w", item.GroupName, err)
		}
	}
	// department_name already has a mapping, so a value nobody has mapped is
	// raised when the directory first gives it to someone.
	if err := s.q.RolesInsertDirectoryReviewAttributeItem(ctx, repo.RolesInsertDirectoryReviewAttributeItemParams{
		OrganizationID: org,
		Kind:           "new_attribute_value",
		AttributeKey:   "department_name",
		AttributeValue: "Platform Engineering",
	}); err != nil {
		return fmt.Errorf("insert demo directory review attribute item: %w", err)
	}
	return nil
}

func (s *seeder) seedRolesAccountsAndDevices(ctx context.Context) error {
	org := s.orgID()

	// AI provider accounts (the identity pages' Accounts column and panel):
	// everyone has a team account under one shared fake provider org, and
	// three people also work through a personal one — the reading the
	// personal-account governance note exists for, and the state a single
	// example made look like an edge case rather than a pattern worth a
	// filter.
	if err := s.q.RolesDeleteUserAccounts(ctx, org); err != nil {
		return fmt.Errorf("delete demo user accounts: %w", err)
	}
	if err := s.q.RolesDeleteDeviceOwners(ctx, org); err != nil {
		return fmt.Errorf("delete demo device owners: %w", err)
	}
	if err := s.q.RolesDeleteDeviceAgentSyncs(ctx, org); err != nil {
		return fmt.Errorf("delete demo device agent syncs: %w", err)
	}
	if err := s.q.RolesDeleteDeviceAgentDeviceSyncs(ctx, org); err != nil {
		return fmt.Errorf("delete demo device agent device syncs: %w", err)
	}
	if err := s.q.RolesDeleteDeviceAgentStateReports(ctx, org); err != nil {
		return fmt.Errorf("delete demo device agent state reports: %w", err)
	}
	// The policy every active agent was last served, and the one it replaced.
	currentEtag := s.policyEtag("current")
	previousEtag := s.policyEtag("previous")
	for i, u := range demoUsers {
		userID := s.userID(u.handle)
		if err := s.q.RolesInsertUserAccount(ctx, repo.RolesInsertUserAccountParams{
			OrganizationID:      org,
			UserID:              userID,
			Provider:            "anthropic",
			ExternalOrgID:       "demo-ext-org-acme",
			ExternalAccountUuid: "demo-acct-" + userID,
			ExternalAccountID:   userID + "_acct",
			Email:               s.email(u.handle),
			AccountType:         "team",
			BillingMode:         "metered",
		}); err != nil {
			return fmt.Errorf("insert demo user account %s: %w", u.handle, err)
		}

		if err := s.q.RolesInsertDeviceOwner(ctx, repo.RolesInsertDeviceOwnerParams{
			OrganizationID: org,
			DeviceID:       "demo-device-" + userID,
			LinkedUserID:   userID,
		}); err != nil {
			return fmt.Errorf("insert demo device owner %s: %w", u.handle, err)
		}

		// Agent heartbeats, split so coverage has more than one answer to
		// give. The first three reported minutes ago (agent_active); the
		// contractor's last check-in is six days old (agent_stale); the last
		// two never installed it (no_agent). A fleet where every row is the
		// same colour tells an admin nothing, which is the whole job of this
		// page.
		if i < 4 {
			lastSeenAgo := "6 days"
			servedEtag := conv.ToPGTextEmpty("")
			if i < 3 {
				lastSeenAgo = "12 minutes"
				servedEtag = conv.ToPGText(currentEtag)
			}
			if err := s.q.RolesInsertDeviceAgentSync(ctx, repo.RolesInsertDeviceAgentSyncParams{
				OrganizationID: org,
				Email:          s.email(u.handle),
				LastSeenAgo:    lastSeenAgo,
				ServedEtag:     servedEtag,
			}); err != nil {
				return fmt.Errorf("insert demo device agent sync %s: %w", u.handle, err)
			}
		}
	}

	// Three personal accounts across two providers: a contractor on his own
	// Claude subscription, an engineer signed into Cursor personally, and a
	// manager whose second Claude login is not the team one. Spread across
	// providers because the column labels the provider, and one-provider data
	// makes that column look constant.
	personal := []struct{ handle, provider, email string }{
		{"mateo", "anthropic", "mateo.alvarez@personal.example"},
		{"priya", "cursor", "priya.raman@personal.example"},
		{"lucas", "anthropic", "lucas.meyer@personal.example"},
	}
	for _, p := range personal {
		if err := s.q.RolesInsertUserAccount(ctx, repo.RolesInsertUserAccountParams{
			OrganizationID:      org,
			UserID:              s.userID(p.handle),
			Provider:            p.provider,
			ExternalOrgID:       "demo-ext-org-personal",
			ExternalAccountUuid: "demo-acct-" + p.handle + "-personal",
			ExternalAccountID:   s.userID(p.handle) + "_personal",
			Email:               p.email,
			AccountType:         "personal",
			BillingMode:         "flat_rate",
		}); err != nil {
			return fmt.Errorf("insert demo personal account %s: %w", p.handle, err)
		}
	}

	// Shadow AI access decisions (the AI Tools tab of the Shadow AI section).
	// Three states, because a column where every row reads the same tells the
	// reader nothing about what the page is for:
	//   claude-code is approved and enforceable — it publishes a CIMD
	//     document, so a decision on it actually reaches the gateway;
	//   codex is blocked and enforceable, the case the block warning is about;
	//   hermes-agent is approved and enforceable, so the Assistants tab shows
	//     a decision rather than a column of unreviewed;
	//   zed is approved and goose blocked — both publish CIMD, so both
	//     decisions reach the gateway, and together with codex they keep the
	//     Harnesses status column from being one answer repeated;
	//   cline, continue, warp and msty publish nothing, so they stay
	//     unreviewed however they are detected — which is the majority case
	//     and should look like the majority case;
	//   cursor, openclaw, aider, ollama and lmstudio are left without rows.
	//     Cursor and OpenClaw are the case worth seeing: they publish no CIMD
	//     document, so Speakeasy cannot recognize them at the gateway and
	//     refuses to record any decision about them. They read unreviewed,
	//     and that is the honest answer rather than a block that enforces
	//     nothing.
	if err := s.q.RolesDeleteAIScanTargets(ctx, org); err != nil {
		return fmt.Errorf("delete demo ai scan targets: %w", err)
	}
	if err := s.q.RolesInsertAIScanTargets(ctx, org); err != nil {
		return fmt.Errorf("insert demo ai scan targets: %w", err)
	}

	// MDM inventory (the identity Accounts & devices tab, and the device
	// coverage widgets). One Jamf-shaped integration holding the fleet:
	// mostly MacBooks, one Windows laptop, and deliberate gaps — a machine
	// whose agent has gone quiet, one with no agent at all, and one assigned
	// to an address that resolves to nobody. Coverage exists to surface
	// exactly those, and a fleet where every row is healthy shows the reader
	// nothing.
	if err := s.q.RolesDeleteMDMDevices(ctx, org); err != nil {
		return fmt.Errorf("delete demo mdm devices: %w", err)
	}
	if err := s.q.RolesDeleteDeviceIntegrationConfigs(ctx, org); err != nil {
		return fmt.Errorf("delete demo device integration configs: %w", err)
	}

	jamfConfigID := s.det("mdm-config-jamf")
	if err := s.q.RolesInsertJamfIntegration(ctx, repo.RolesInsertJamfIntegrationParams{
		ID:             jamfConfigID,
		OrganizationID: org,
		Settings:       fmt.Appendf(nil, `{"instance_url": "https://%s.jamfcloud.example"}`, s.spec.OrgSlug),
	}); err != nil {
		return fmt.Errorf("insert demo jamf integration: %w", err)
	}

	// user_id is resolved here rather than left to a sync: the seed knows the
	// member each address belongs to, and a NULL would put every device in
	// the unresolved bucket.
	for _, d := range rolesMDMDevices {
		userID := pgtype.Text{String: "", Valid: false}
		if d.hasUser {
			userID = pgtype.Text{String: s.userID(d.owner), Valid: true}
		}
		if err := s.q.RolesInsertMDMDevice(ctx, repo.RolesInsertMDMDeviceParams{
			DeviceIntegrationConfigID: jamfConfigID,
			OrganizationID:            org,
			ExternalID:                s.name("mdm-" + strconv.Itoa(d.n)),
			SerialNumber:              d.serial,
			Hostname:                  d.hostname,
			OsName:                    d.osName,
			OsVersion:                 d.osVersion,
			UserEmail:                 s.email(d.owner),
			UserID:                    userID,
			CheckInAgo:                d.checkInAgo,
		}); err != nil {
			return fmt.Errorf("insert demo mdm device %d: %w", d.n, err)
		}
	}

	// Device-level heartbeats, keyed on the serial the MDM reports. Coverage
	// prefers this branch over the email one: a machine's own agent answers
	// for that machine, where an email heartbeat only says its owner is
	// running an agent somewhere. With none of these rows the fleet could
	// never read better than "an agent exists for this person", which is the
	// weaker claim the page exists to stop an admin making.
	deviceSyncs := []struct{ serial, owner, hostname, lastSeenAgo string }{
		{"C02DEMO0001", "amara", "amara-mbp", "12 minutes"},
		{"C02DEMO0002", "jonas", "jonas-mbp", "25 minutes"},
		{"C02DEMO0003", "priya", "priya-mbp", "5 minutes"},
		// Installed, then went quiet: the drift case, and the row worth
		// chasing.
		{"C02DEMO0004", "mateo", "mateo-mbp", "6 days"},
		// Two machines Jamf never enrolled, so only their agents list them
		// (the Devices tab's "Not in an MDM" rows). Amara's build box
		// enforces policy; Priya's old laptop went quiet after she moved to
		// a new one. Both owners' enrolled laptops already answer by their
		// own serials, so these leave the MDM coverage buckets unchanged.
		{"H4DEMO0101", "amara", "amara-build-mini", "9 minutes"},
		{"C02DEMO0102", "priya", "priya-old-mbp", "5 days"},
	}
	for _, d := range deviceSyncs {
		if err := s.q.RolesInsertDeviceAgentDeviceSync(ctx, repo.RolesInsertDeviceAgentDeviceSyncParams{
			OrganizationID: org,
			SerialNumber:   d.serial,
			Email:          s.email(d.owner),
			Hostname:       d.hostname,
			LastSeenAgo:    d.lastSeenAgo,
		}); err != nil {
			return fmt.Errorf("insert demo device agent device sync %s: %w", d.serial, err)
		}
	}

	// State reports, so coverage can say whether each active machine
	// enforces the policy it was served. Amara's applied it, Jonas's is still
	// on the previous one because Claude Code's managed settings are not
	// writable, and Priya's runs a build that does not report. Three answers,
	// for the same reason the heartbeats above are split. Amara's build box
	// reports too, so the agent-only rows include an enforcing machine.
	reports := []struct {
		serial, owner, hostname, reportedAgo, etag, appliedAgo, tools string
		degraded                                                      int32
	}{
		{
			"C02DEMO0001", "amara", "amara-mbp", "8 minutes", currentEtag, "2 days",
			`[{"tool":"claude_code","layer":"managed","outcome":"armed","restart_required":false,"action_required":false,"drift_corrections":2},` +
				`{"tool":"cursor","layer":"user","outcome":"armed","restart_required":false,"action_required":false,"drift_corrections":0},` +
				`{"tool":"codex","layer":"off","outcome":"skipped","restart_required":false,"action_required":false}]`,
			0,
		},
		{
			"C02DEMO0002", "jonas", "jonas-mbp", "14 minutes", previousEtag, "9 days",
			`[{"tool":"claude_code","layer":"managed","outcome":"error","error_kind":"permission_denied","restart_required":false,"action_required":true},` +
				`{"tool":"cursor","layer":"user","outcome":"armed","restart_required":true,"action_required":false,"drift_corrections":0}]`,
			1,
		},
		{
			"H4DEMO0101", "amara", "amara-build-mini", "6 minutes", currentEtag, "2 days",
			`[{"tool":"claude_code","layer":"managed","outcome":"armed","restart_required":false,"action_required":false,"drift_corrections":0}]`,
			0,
		},
	}
	for _, r := range reports {
		if err := s.q.RolesInsertDeviceAgentStateReport(ctx, repo.RolesInsertDeviceAgentStateReportParams{
			OrganizationID:    org,
			Email:             s.email(r.owner),
			SerialNumber:      r.serial,
			Hostname:          r.hostname,
			ReportedAgo:       r.reportedAgo,
			AppliedEtag:       r.etag,
			AppliedAgo:        r.appliedAgo,
			Tools:             []byte(r.tools),
			DegradedToolCount: r.degraded,
			SpeakeasydVersion: "0.42.0",
		}); err != nil {
			return fmt.Errorf("insert demo device agent state report %s: %w", r.serial, err)
		}
	}
	return nil
}

// policyEtag is a device-agent policy etag in the shape GetPlugins serves: a
// hex sha256.
func (s *seeder) policyEtag(label string) string {
	sum := sha256.Sum256([]byte(s.name("policy-etag-" + label)))
	return hex.EncodeToString(sum[:])
}
