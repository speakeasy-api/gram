package auditapi_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auditlogs"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/auditapi"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestPlatformMCPAuditSelfPaginationAndDetails(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAuditService(t)
	auth := testAuthContext(t, ctx)
	ctx = contextvalues.WithAuthenticatedActor(ctx, auth, urn.NewPrincipal(urn.PrincipalTypeUser, auth.UserID))
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, auth.ActiveOrganizationID))
	ids := make([]string, 0, 55)
	for range 55 {
		for _, actorType := range []string{"user", "agent", "system"} {
			seed := auditLogSeed{
				organizationID: auth.ActiveOrganizationID, projectID: uuid.NullUUID{UUID: *auth.ProjectID, Valid: true},
				actorID: auth.UserID, actorType: actorType, action: "project:update", subjectID: uuid.NewString(), subjectType: "project",
				actingSurface: new("platform_mcp"), actingClientID: new("client-test"),
				beforeSnapshot: []byte(`{"name":"before"}`), afterSnapshot: []byte(`["after",true]`), metadata: []byte(`{"nested":{"key":"value"}}`),
			}
			id := insertAuditLog(t, ctx, ti, seed)
			if actorType == "user" {
				ids = append(ids, id.String())
			}
		}
	}
	for _, seed := range []auditLogSeed{
		{organizationID: auth.ActiveOrganizationID, actorID: "other-user", actorType: "user", action: "project:update", subjectID: "other", subjectType: "project"},
		{organizationID: "other-org", actorID: auth.UserID, actorType: "user", action: "project:update", subjectID: "other", subjectType: "project"},
		{organizationID: auth.ActiveOrganizationID, actorID: auth.UserID, actorType: "user", action: "assistant:update", subjectID: "assistant", subjectType: "assistant"},
	} {
		insertAuditLog(t, ctx, ti, seed)
	}
	request := auditapi.PlatformMCPAuditRequest{OrganizationID: auth.ActiveOrganizationID, UserID: auth.UserID, Cursor: ""}
	page, err := ti.service.ListPlatformMCPAuditLogs(ctx, request)
	require.NoError(t, err)
	require.Equal(t, "self", page.Visibility)
	require.Len(t, page.Logs, 50)
	require.NotNil(t, page.NextCursor)
	for i, entry := range page.Logs {
		require.Equal(t, ids[54-i], entry.ID)
		require.Equal(t, "user", entry.ActorType)
		require.Equal(t, auth.UserID, entry.ActorID)
		require.JSONEq(t, `{"name":"before"}`, string(entry.BeforeSnapshot))
		require.JSONEq(t, `["after",true]`, string(entry.AfterSnapshot))
		require.Equal(t, map[string]any{"nested": map[string]any{"key": "value"}}, entry.Metadata)
		require.Equal(t, auth.ProjectID.String(), *entry.ProjectID)
		require.Equal(t, *auth.ProjectSlug, *entry.ProjectSlug)
		require.Equal(t, "client-test", *entry.ActingClientID)
	}
	request.Cursor = *page.NextCursor
	page, err = ti.service.ListPlatformMCPAuditLogs(ctx, request)
	require.NoError(t, err)
	require.Len(t, page.Logs, 5)
	require.Nil(t, page.NextCursor)
	for i, entry := range page.Logs {
		require.Equal(t, ids[4-i], entry.ID)
	}

	admin := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, auth.ActiveOrganizationID), authz.NewGrant(authz.ScopeOrgAdmin, auth.ActiveOrganizationID))
	request.Cursor = ""
	page, err = ti.service.ListPlatformMCPAuditLogs(admin, request)
	require.NoError(t, err)
	require.Equal(t, "organization", page.Visibility)
	require.Equal(t, "other-user", page.Logs[0].ActorID)
	for _, entry := range page.Logs {
		require.NotEqual(t, "assistant", entry.SubjectType)
	}
	// Existing HTTP access remains broader than this MCP-specific self boundary.
	httpPage, err := ti.service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Equal(t, "other-user", httpPage.Logs[0].ActorID)
}

func TestPlatformMCPAuditInvestigationFilters(t *testing.T) {
	t.Parallel()
	for _, admin := range []bool{false, true} {
		t.Run(map[bool]string{false: "self", true: "organization"}[admin], func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAuditService(t)
			auth := testAuthContext(t, ctx)
			ctx = contextvalues.WithAuthenticatedActor(ctx, auth, urn.NewPrincipal(urn.PrincipalTypeUser, auth.UserID))
			grants := []authz.Grant{authz.NewGrant(authz.ScopeOrgRead, auth.ActiveOrganizationID)}
			if admin {
				grants = append(grants, authz.NewGrant(authz.ScopeOrgAdmin, auth.ActiveOrganizationID))
			}
			ctx = authztest.WithExactGrants(t, ctx, grants...)
			project := createProject(t, ctx, ti, auth.ActiveOrganizationID)
			otherProject := createProject(t, ctx, ti, auth.ActiveOrganizationID)
			base := auditLogSeed{
				organizationID: auth.ActiveOrganizationID, projectID: uuid.NullUUID{UUID: project.ID, Valid: true},
				actorID: auth.UserID, actorType: "user", action: "future:investigate", subjectType: "future_resource", subjectID: "target", actingSurface: new("future_surface"),
			}
			from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
			to := from.Add(time.Hour)
			seeds := []auditLogSeed{base}
			for _, change := range []func(*auditLogSeed){
				func(s *auditLogSeed) { s.organizationID = "foreign-org" },
				func(s *auditLogSeed) { s.projectID = uuid.NullUUID{} },
				func(s *auditLogSeed) { s.actorID = "other-user" },
				func(s *auditLogSeed) { s.actorType = "agent" },
				func(s *auditLogSeed) { s.action = "other:action" },
				func(s *auditLogSeed) { s.subjectType = "other_type" },
				func(s *auditLogSeed) { s.subjectID = "other-subject" },
				func(s *auditLogSeed) { s.actingSurface = nil },
				func(s *auditLogSeed) { s.subjectType = "assistant" },
				func(s *auditLogSeed) { s.subjectType, s.actorID = "assistant", "other-user" },
				func(s *auditLogSeed) { s.subjectType, s.actorType = "assistant", "agent" },
				func(s *auditLogSeed) { s.projectID = uuid.NullUUID{UUID: otherProject.ID, Valid: true} },
			} {
				seed := base
				change(&seed)
				seeds = append(seeds, seed)
			}
			ids := make([]uuid.UUID, 0, len(seeds))
			for _, seed := range seeds {
				ids = append(ids, insertAuditLog(t, ctx, ti, seed))
			}
			require.NoError(t, auditrepo.New(ti.conn).UpdateAuditLogCreatedAtForTesting(ctx, auditrepo.UpdateAuditLogCreatedAtForTestingParams{Ids: ids, CreatedAt: conv.ToPGTimestamptz(from)}))
			upper := insertAuditLog(t, ctx, ti, base)
			require.NoError(t, auditrepo.New(ti.conn).UpdateAuditLogCreatedAtForTesting(ctx, auditrepo.UpdateAuditLogCreatedAtForTestingParams{Ids: []uuid.UUID{upper}, CreatedAt: conv.ToPGTimestamptz(to)}))
			request := auditapi.PlatformMCPAuditRequest{OrganizationID: auth.ActiveOrganizationID, UserID: auth.UserID}
			// Each individual predicate is checked against a baseline scoped only by authority.
			baseline, err := ti.service.ListPlatformMCPAuditLogs(ctx, request)
			require.NoError(t, err)
			for name, narrowed := range map[string]auditapi.PlatformMCPAuditRequest{
				"project": {ProjectID: base.projectID}, "actor": {ActorID: auth.UserID},
				"action": {Action: base.action}, "subject type": {SubjectType: base.subjectType},
				"subject": {SubjectID: base.subjectID}, "surface": {ActingSurface: *base.actingSurface},
				"unknown": {ActingSurface: "unknown"}, "assistant": {SubjectType: "assistant"},
				"from": {From: &to}, "to": {To: &to},
			} {
				narrowed.OrganizationID, narrowed.UserID = request.OrganizationID, request.UserID
				page, err := ti.service.ListPlatformMCPAuditLogs(ctx, narrowed)
				require.NoError(t, err, name)
				expected := make([]string, 0)
				for i := len(seeds); i >= 0; i-- {
					seed, id, created := base, upper, to
					if i < len(seeds) {
						seed, id, created = seeds[i], ids[i], from
					}
					if seed.organizationID != auth.ActiveOrganizationID || (!admin && (seed.actorID != auth.UserID || seed.actorType != "user")) {
						continue
					}
					if narrowed.SubjectType == "" && seed.subjectType == "assistant" {
						continue
					}
					surface := "unknown"
					if seed.actingSurface != nil {
						surface = *seed.actingSurface
					}
					if (narrowed.ProjectID.Valid && seed.projectID != narrowed.ProjectID) ||
						(narrowed.ActorID != "" && seed.actorID != narrowed.ActorID) ||
						(narrowed.Action != "" && seed.action != narrowed.Action) ||
						(narrowed.SubjectType != "" && seed.subjectType != narrowed.SubjectType) ||
						(narrowed.SubjectID != "" && seed.subjectID != narrowed.SubjectID) ||
						(narrowed.ActingSurface != "" && surface != narrowed.ActingSurface) ||
						(narrowed.From != nil && created.Before(*narrowed.From)) ||
						(narrowed.To != nil && !created.Before(*narrowed.To)) {
						continue
					}
					expected = append(expected, id.String())
				}
				actual := make([]string, 0, len(page.Logs))
				for _, entry := range page.Logs {
					actual = append(actual, entry.ID)
				}
				require.Equal(t, expected, actual, name)
				if name != "assistant" {
					require.LessOrEqual(t, len(page.Logs), len(baseline.Logs), name)
				}
			}
			request.ProjectID, request.ActorID, request.Action = base.projectID, auth.UserID, base.action
			request.SubjectType, request.SubjectID, request.ActingSurface = base.subjectType, base.subjectID, *base.actingSurface
			request.From, request.To = &from, &to
			page, err := ti.service.ListPlatformMCPAuditLogs(ctx, request)
			require.NoError(t, err)
			if admin {
				require.Len(t, page.Logs, 2, "organization actor filter remains ID-only, including same-ID agents")
			} else {
				require.Len(t, page.Logs, 1)
			}
			require.Equal(t, ids[0].String(), page.Logs[len(page.Logs)-1].ID)
		})
	}
}

func TestPlatformMCPAuditHistoricalProjectTargeting(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"live", "deleted", "foreign", "missing"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAuditService(t)
			auth := testAuthContext(t, ctx)
			ctx = contextvalues.WithAuthenticatedActor(ctx, auth, urn.NewPrincipal(urn.PrincipalTypeUser, auth.UserID))
			ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, auth.ActiveOrganizationID))
			id := uuid.New()
			if target != "missing" {
				org := auth.ActiveOrganizationID
				if target == "foreign" {
					org = "foreign-org"
				}
				project := createProject(t, ctx, ti, org)
				id = project.ID
				if target == "deleted" {
					deleteProject(t, ctx, ti, id)
				}
			}
			projectID := uuid.NullUUID{UUID: id, Valid: true}
			event := insertAuditLog(t, ctx, ti, auditLogSeed{organizationID: auth.ActiveOrganizationID, projectID: projectID, actorID: auth.UserID, actorType: "user", action: "project:update", subjectID: "target", subjectType: "project"})
			insertAuditLog(t, ctx, ti, auditLogSeed{organizationID: auth.ActiveOrganizationID, actorID: auth.UserID, actorType: "user", action: "org:update", subjectID: "org", subjectType: "org"})
			page, err := ti.service.ListPlatformMCPAuditLogs(ctx, auditapi.PlatformMCPAuditRequest{OrganizationID: auth.ActiveOrganizationID, UserID: auth.UserID, ProjectID: projectID})
			if target == "foreign" || target == "missing" {
				require.Error(t, err, "synthetic matching audit rows cannot establish project ownership")
				require.Nil(t, page)
				return
			}
			require.NoError(t, err)
			require.Len(t, page.Logs, 1)
			require.Equal(t, event.String(), page.Logs[0].ID)
		})
	}
}

func TestPlatformMCPAuditFilteredPaginationAndChangedFilters(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAuditService(t)
	auth := testAuthContext(t, ctx)
	ctx = contextvalues.WithAuthenticatedActor(ctx, auth, urn.NewPrincipal(urn.PrincipalTypeUser, auth.UserID))
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, auth.ActiveOrganizationID))
	ids := make([]string, 0, 53)
	otherIDs := make([]string, 0, 53)
	for range 53 {
		for _, action := range []string{"target:action", "other:action"} {
			id := insertAuditLog(t, ctx, ti, auditLogSeed{organizationID: auth.ActiveOrganizationID, actorID: auth.UserID, actorType: "user", action: action, subjectID: "subject", subjectType: "project"})
			if action == "target:action" {
				ids = append(ids, id.String())
			} else {
				otherIDs = append(otherIDs, id.String())
			}
		}
	}
	request := auditapi.PlatformMCPAuditRequest{OrganizationID: auth.ActiveOrganizationID, UserID: auth.UserID, Action: "target:action"}
	page, err := ti.service.ListPlatformMCPAuditLogs(ctx, request)
	require.NoError(t, err)
	require.Len(t, page.Logs, 50)
	require.NotNil(t, page.NextCursor)
	for i, entry := range page.Logs {
		require.Equal(t, ids[52-i], entry.ID)
	}
	request.Cursor = *page.NextCursor
	page, err = ti.service.ListPlatformMCPAuditLogs(ctx, request)
	require.NoError(t, err)
	require.Len(t, page.Logs, 3)
	require.Nil(t, page.NextCursor)
	for i, entry := range page.Logs {
		require.Equal(t, ids[2-i], entry.ID)
	}
	request.Action = "other:action"
	page, err = ti.service.ListPlatformMCPAuditLogs(ctx, request)
	require.NoError(t, err)
	require.Len(t, page.Logs, 3, "new filter applies only below the old sequence")
	for i, entry := range page.Logs {
		require.Equal(t, otherIDs[2-i], entry.ID)
	}
}

func TestPlatformMCPAuditDeniesUntrustedIdentityAndMissingRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAuditService(t)
	auth := testAuthContext(t, ctx)
	ctx = contextvalues.WithAuthenticatedActor(ctx, auth, urn.NewPrincipal(urn.PrincipalTypeUser, auth.UserID))
	request := auditapi.PlatformMCPAuditRequest{OrganizationID: auth.ActiveOrganizationID, UserID: auth.UserID, Cursor: ""}
	for _, grants := range [][]authz.Grant{
		nil,
		{authz.NewGrant(authz.ScopeOrgRead, "other-org")},
		{authz.NewGrant(authz.ScopeOrgAdmin, "other-org")},
	} {
		page, err := ti.service.ListPlatformMCPAuditLogs(authztest.WithExactGrants(t, ctx, grants...), request)
		require.Error(t, err)
		require.Nil(t, page)
	}
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, auth.ActiveOrganizationID))
	for _, target := range []auditapi.PlatformMCPAuditRequest{
		{OrganizationID: "other-org", UserID: auth.UserID, Cursor: ""},
		{OrganizationID: auth.ActiveOrganizationID, UserID: "other-user", Cursor: ""},
	} {
		page, err := ti.service.ListPlatformMCPAuditLogs(ctx, target)
		require.Error(t, err)
		require.Nil(t, page)
	}
	ctx = contextvalues.WithAuthenticatedActor(ctx, auth, urn.NewPrincipal(urn.PrincipalTypeAgent, auth.UserID))
	page, err := ti.service.ListPlatformMCPAuditLogs(ctx, request)
	require.Error(t, err)
	require.Nil(t, page)
}

func TestPlatformMCPAuditCustomerMaskingParity(t *testing.T) {
	t.Parallel()
	for _, ownOrganization := range []bool{false, true} {
		t.Run(map[bool]string{false: "customer", true: "team"}[ownOrganization], func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAuditService(t)
			auth := testAuthContext(t, ctx)
			staff := "test-staff-user"
			seedSpeakeasyMember(t, ctx, ti, staff)
			if ownOrganization {
				auth.ActiveOrganizationID = speakeasyTeamOrganizationID
			}
			ctx = contextvalues.WithAuthenticatedActor(ctx, auth, urn.NewPrincipal(urn.PrincipalTypeUser, auth.UserID))
			ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, auth.ActiveOrganizationID), authz.NewGrant(authz.ScopeOrgAdmin, auth.ActiveOrganizationID))
			for _, surface := range []string{"admin", "admin_mcp", "dashboard"} {
				insertAuditLog(t, ctx, ti, auditLogSeed{
					organizationID: auth.ActiveOrganizationID, actorID: staff, actorType: "user",
					actorDisplayName: new("Test Staff"), actorSlug: new("staff"), actingSurface: &surface, actingClientID: new("staff-client"),
					action: "project:update", subjectID: "project", subjectType: "project",
					beforeSnapshot: []byte(`{"retained":"customer policy"}`), metadata: []byte(`{"source":"test"}`),
				})
			}
			insertAuditLog(t, ctx, ti, auditLogSeed{
				organizationID: auth.ActiveOrganizationID, actorID: staff, actorType: "agent", actorDisplayName: new("Automation"),
				action: "project:update", subjectID: "project", subjectType: "project",
			})
			httpPage, err := ti.service.List(ctx, &gen.ListPayload{})
			require.NoError(t, err)
			page, err := ti.service.ListPlatformMCPAuditLogs(ctx, auditapi.PlatformMCPAuditRequest{OrganizationID: auth.ActiveOrganizationID, UserID: auth.UserID, Cursor: ""})
			require.NoError(t, err)
			require.Equal(t, httpPage.Logs, page.Logs)
			require.Len(t, page.Logs, 4)
			require.Equal(t, "Automation", *page.Logs[0].ActorDisplayName)
			for _, entry := range page.Logs[1:] {
				if entry.ActingSurface == "dashboard" && ownOrganization {
					require.Equal(t, "Test Staff", *entry.ActorDisplayName)
				} else {
					require.Equal(t, "Speakeasy Team", *entry.ActorDisplayName)
					if entry.ActingSurface == "admin" || entry.ActingSurface == "admin_mcp" {
						require.Empty(t, entry.ActorID)
						require.Nil(t, entry.ActingClientID)
					}
				}
				require.JSONEq(t, `{"retained":"customer policy"}`, string(entry.BeforeSnapshot))
			}
		})
	}
}

func TestPlatformMCPAuditUnsupportedMetadataRefusesWholePage(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAuditService(t)
	auth := testAuthContext(t, ctx)
	ctx = contextvalues.WithAuthenticatedActor(ctx, auth, urn.NewPrincipal(urn.PrincipalTypeUser, auth.UserID))
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, auth.ActiveOrganizationID))
	for _, metadata := range []string{`["unsupported"]`, `{"valid":true}`} {
		insertAuditLog(t, ctx, ti, auditLogSeed{
			organizationID: auth.ActiveOrganizationID, actorID: auth.UserID, actorType: "user", action: "project:update", subjectID: "project", subjectType: "project", metadata: []byte(metadata),
		})
	}
	page, err := ti.service.ListPlatformMCPAuditLogs(ctx, auditapi.PlatformMCPAuditRequest{OrganizationID: auth.ActiveOrganizationID, UserID: auth.UserID, Cursor: ""})
	require.Error(t, err)
	require.Nil(t, page, "a valid first row must not escape before an unsupported metadata row")
}
