package telemetry_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// seedDirectoryMember adds an organization member with a directory department
// and, optionally, a directory group — the identity-provider state a narrowed
// logs:read grant resolves through.
func seedDirectoryMember(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID, suffix, department, group string) string {
	t.Helper()

	userID := "user_actor_" + suffix
	email := suffix + "@actor.example.com"
	seedTime := time.Now().UTC()

	_, err := userrepo.New(conn).UpsertUser(ctx, userrepo.UpsertUserParams{
		ID:          userID,
		Email:       email,
		DisplayName: "Actor " + suffix,
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       false,
	})
	require.NoError(t, err)

	_, err = orgrepo.New(conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: orgID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)

	attributes, err := json.Marshal(map[string]string{"department_name": department})
	require.NoError(t, err)

	directoryUserID, err := directoryrepo.New(conn).UpsertDirectoryUser(ctx, directoryrepo.UpsertDirectoryUserParams{
		OrganizationID:        orgID,
		UserID:                conv.ToPGText(userID),
		WorkosDirectoryUserID: "directory_user_actor_" + suffix,
		Email:                 conv.ToPGText(email),
		Attributes:            attributes,
		RestoreDeleted:        true,
		WorkosCreatedAt:       conv.ToPGTimestamptz(seedTime),
		WorkosUpdatedAt:       conv.ToPGTimestamptz(seedTime),
		WorkosLastEventID:     conv.ToPGText("event_directory_user_actor_" + suffix),
	})
	require.NoError(t, err)

	if group != "" {
		directoryGroupID, err := directoryrepo.New(conn).UpsertDirectoryGroup(ctx, directoryrepo.UpsertDirectoryGroupParams{
			OrganizationID:         orgID,
			WorkosDirectoryGroupID: "directory_group_actor_" + suffix,
			Name:                   group,
			Attributes:             []byte(`{"object":"directory_group"}`),
			WorkosCreatedAt:        conv.ToPGTimestamptz(seedTime),
			WorkosUpdatedAt:        conv.ToPGTimestamptz(seedTime),
			WorkosLastEventID:      conv.ToPGText("event_directory_group_actor_" + suffix),
		})
		require.NoError(t, err)

		_, err = directoryrepo.New(conn).OpenDirectoryUserGroupMembership(ctx, directoryrepo.OpenDirectoryUserGroupMembershipParams{
			DirectoryUserID:        directoryUserID,
			DirectoryGroupID:       directoryGroupID,
			WorkosDirectoryUserID:  "directory_user_actor_" + suffix,
			WorkosDirectoryGroupID: "directory_group_actor_" + suffix,
			WorkosCreatedAt:        conv.ToPGTimestamptz(seedTime),
		})
		require.NoError(t, err)
	}

	return email
}

// insertActorTelemetryLog writes one tool-call log attributed to an actor
// email, the identity every actor-scoped read filters on.
func insertActorTelemetryLog(t *testing.T, ctx context.Context, projectID, email string, timestamp time.Time) string {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes, err := json.Marshal(map[string]any{
		"gram.tool.name": "actor_scope_tool",
		"user":           map[string]any{"email": email},
	})
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "actor scope log body",
		strings.ReplaceAll(uuid.New().String(), "-", ""), "0000000000000001",
		string(attributes), `{"service.name":"gram-test"}`,
		projectID, "tools:http:gram:actor_scope_tool", "gram-test")
	require.NoError(t, err)

	testenv.FlushClickHouseAsyncInserts(t, conn)

	return id.String()
}

type actorScopeFixture struct {
	ctx             context.Context
	ti              *testInstance
	callerEmail     string
	engineeringMail string
	salesMail       string
	from            string
	to              string
}

func newActorScopeFixture(t *testing.T) actorScopeFixture {
	t.Helper()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.Email)

	suffix := strings.ReplaceAll(uuid.New().String()[:8], "-", "")
	engineeringMail := seedDirectoryMember(t, ctx, ti.conn, ti.orgID, "eng"+suffix, "Engineering", "platform-leads")
	salesMail := seedDirectoryMember(t, ctx, ti.conn, ti.orgID, "sales"+suffix, "Sales", "")

	now := time.Now().UTC().Add(-10 * time.Minute)
	insertActorTelemetryLog(t, ctx, ti.projectID, engineeringMail, now)
	insertActorTelemetryLog(t, ctx, ti.projectID, salesMail, now.Add(time.Minute))
	insertActorTelemetryLog(t, ctx, ti.projectID, *authCtx.Email, now.Add(2*time.Minute))

	return actorScopeFixture{
		ctx:             ctx,
		ti:              ti,
		callerEmail:     *authCtx.Email,
		engineeringMail: engineeringMail,
		salesMail:       salesMail,
		from:            time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339),
		to:              time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	}
}

func (f actorScopeFixture) searchLogEmails(t *testing.T, ctx context.Context) []string {
	t.Helper()

	result, err := f.ti.service.SearchLogs(ctx, &telem_gen.SearchLogsPayload{
		Limit:            50,
		Sort:             "desc",
		Cursor:           nil,
		From:             &f.from,
		To:               &f.to,
		Filters:          nil,
		Filter:           nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	emails := make([]string, 0, len(result.Logs))
	for _, log := range result.Logs {
		raw, err := json.Marshal(log.Attributes)
		require.NoError(t, err)

		var attributes struct {
			User struct {
				Email string `json:"email"`
			} `json:"user"`
		}
		require.NoError(t, json.Unmarshal(raw, &attributes))
		emails = append(emails, attributes.User.Email)
	}
	return emails
}

func (f actorScopeFixture) grants(t *testing.T, logsRead ...authz.Grant) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(f.ctx)
	require.True(t, ok)

	grants := append([]authz.Grant{authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String())}, logsRead...)
	return authztest.WithExactGrants(t, f.ctx, grants...)
}

func (f actorScopeFixture) narrowedGrant(key, value string) authz.Grant {
	return authz.NewGrantWithSelector(authz.ScopeLogsRead, authz.Selector{
		authz.SelectorKeyResourceKind: authz.ResourceKindLogs,
		authz.SelectorKeyResourceID:   f.ti.orgID,
		key:                           value,
	})
}

func TestActorScope_SearchLogs_UnrestrictedGrantSeesEveryone(t *testing.T) {
	t.Parallel()

	fixture := newActorScopeFixture(t)
	ctx := fixture.grants(t, authz.NewGrant(authz.ScopeLogsRead, authz.WildcardResource))

	emails := fixture.searchLogEmails(t, ctx)
	require.ElementsMatch(t, []string{fixture.engineeringMail, fixture.salesMail, fixture.callerEmail}, emails)
}

func TestActorScope_SearchLogs_DepartmentGrantSeesOnlyThatDepartment(t *testing.T) {
	t.Parallel()

	fixture := newActorScopeFixture(t)
	ctx := fixture.grants(t, fixture.narrowedGrant(authz.SelectorKeyActorDepartment, "Engineering"))

	emails := fixture.searchLogEmails(t, ctx)
	require.ElementsMatch(t, []string{fixture.engineeringMail, fixture.callerEmail}, emails,
		"a department-scoped grant covers that department plus the caller's own activity")
}

func TestActorScope_SearchLogs_GroupGrantSeesOnlyThatGroup(t *testing.T) {
	t.Parallel()

	fixture := newActorScopeFixture(t)
	ctx := fixture.grants(t, fixture.narrowedGrant(authz.SelectorKeyActorGroup, "platform-leads"))

	emails := fixture.searchLogEmails(t, ctx)
	require.ElementsMatch(t, []string{fixture.engineeringMail, fixture.callerEmail}, emails)
}

func TestActorScope_SearchLogs_UnmatchedDepartmentSeesOnlySelf(t *testing.T) {
	t.Parallel()

	fixture := newActorScopeFixture(t)
	ctx := fixture.grants(t, fixture.narrowedGrant(authz.SelectorKeyActorDepartment, "Legal"))

	emails := fixture.searchLogEmails(t, ctx)
	require.Equal(t, []string{fixture.callerEmail}, emails,
		"a grant that covers nobody must not widen the read")
}

func TestActorScope_SearchLogs_NoLogsGrantSeesOnlySelf(t *testing.T) {
	t.Parallel()

	fixture := newActorScopeFixture(t)
	ctx := fixture.grants(t)

	emails := fixture.searchLogEmails(t, ctx)
	require.Equal(t, []string{fixture.callerEmail}, emails,
		"absence of logs:read degrades to own activity, never to unrestricted")
}

func TestActorScope_UnscopableRead_RefusesNarrowedGrant(t *testing.T) {
	t.Parallel()

	fixture := newActorScopeFixture(t)
	ctx := fixture.grants(t, fixture.narrowedGrant(authz.SelectorKeyActorDepartment, "Engineering"))

	_, err := fixture.ti.service.GetProjectMetricsSummary(ctx, &telem_gen.GetProjectMetricsSummaryPayload{
		From:             fixture.from,
		To:               fixture.to,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code,
		"an endpoint that cannot filter by actor must refuse a narrowed grant rather than over-serve it")
}

func TestActorScope_UnscopableRead_AllowsUnrestrictedGrant(t *testing.T) {
	t.Parallel()

	fixture := newActorScopeFixture(t)
	ctx := fixture.grants(t, authz.NewGrant(authz.ScopeLogsRead, authz.WildcardResource))

	_, err := fixture.ti.service.GetProjectMetricsSummary(ctx, &telem_gen.GetProjectMetricsSummaryPayload{
		From:             fixture.from,
		To:               fixture.to,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}
