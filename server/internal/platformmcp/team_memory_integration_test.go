package platformmcp

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type teamMemoryFixture struct {
	db        *pgxpool.Pool
	principal Principal
	project   ResolvedProject
	flags     *feature.InMemory
	service   *TeamMemoryService
}

func newTeamMemoryFixture(t *testing.T) teamMemoryFixture {
	t.Helper()
	db, err := platformMCPInfra.CloneTestDatabase(t, "platform_team_memory")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, t.Context(), db)
	engine := authz.NewEngine(testenv.NewLogger(t), db, func(context.Context, string) (bool, error) { return false, nil }, nil)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagTeamMemory, principal.OrganizationID, true)
	service := NewTeamMemoryService(db, engine, audit.NewLogger(), flags, NewPostgresOrganizationSlugResolver(db), allowBudget(), NewChatSearchRedactor())
	require.NotNil(t, service)
	return teamMemoryFixture{db: db, principal: principal, project: project, flags: flags, service: service}
}

// context authenticates the fixture principal with grants on the project.
func (f teamMemoryFixture) context(ctx context.Context, grants ...authz.Grant) context.Context {
	actor := contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: f.principal.OrganizationID, UserID: f.principal.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, f.principal.UserID))
	return ContextWithPrincipal(authz.GrantsToContext(actor, grants), f.principal)
}

func (f teamMemoryFixture) writer(ctx context.Context) context.Context {
	return f.context(ctx, authz.NewGrant(authz.ScopeMemoryWrite, f.project.ID.String()))
}

func (f teamMemoryFixture) remember(content string) RememberInput {
	return RememberInput{ProjectSlug: f.project.Slug, Content: content}
}

func TestRememberStoresRedactedMemoAttributedToCaller(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	ctx := f.writer(t.Context())
	chatID := seedRecallChat(t, ctx, f.db, f.project.ID, f.principal.OrganizationID, "user_fixture_other_owner", "memo-"+uuid.NewString(), "Queue choice", uuid.NullUUID{})
	secret := "ghp_" + strings.Join([]string{"q1W2e3R4t5Y6", "u7I8o9P0a1S2", "d3F4g5H6j7K8"}, "")

	output, err := f.service.Remember(ctx, f.principal, RememberInput{ProjectSlug: f.project.Slug, ChatID: chatID.String(), Content: "  We chose the retry queue for webhooks. Deploy token " + secret + "  "})
	require.NoError(t, err)
	require.True(t, output.Redacted)
	require.Equal(t, TeamMemorySourceMemo, output.Memo.Source.Kind)
	require.Equal(t, chatID.String(), output.Memo.Source.ChatID)
	require.Equal(t, f.principal.UserID, output.Memo.AuthorUserID)
	require.True(t, strings.HasPrefix(output.Memo.Content, "We chose the retry queue for webhooks."))
	require.NotContains(t, output.Memo.Content, secret[:12])

	stored, err := testrepo.New(f.db).GetMemoryMemoFixture(ctx, testrepo.GetMemoryMemoFixtureParams{ID: uuid.MustParse(output.Memo.Source.ID), ProjectID: f.project.ID})
	require.NoError(t, err)
	require.Equal(t, output.Memo.Content, stored.Content)
	require.NotContains(t, stored.Content, secret[:12])
	require.Equal(t, f.principal.UserID, stored.AuthorUserID.String)
	require.Equal(t, chatID, stored.ChatID.UUID)

	event, err := audittest.LatestAuditLogByAction(ctx, f.db, audit.ActionMemoryMemoCreate)
	require.NoError(t, err)
	require.Equal(t, f.principal.UserID, event.ActorID)
	require.Equal(t, output.Memo.Source.ID, event.SubjectID)
	for _, recorded := range [][]byte{event.Metadata, event.BeforeSnapshot, event.AfterSnapshot, []byte(event.SubjectDisplay)} {
		require.NotContains(t, string(recorded), "retry queue")
	}
}

func TestRememberRequiresExplicitProjectMemoryWriteGrant(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	other, err := projectsrepo.New(f.db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{OrganizationID: f.principal.OrganizationID, Name: "Other memory project", Slug: "other-memory-project"})
	require.NoError(t, err)

	for _, grants := range [][]authz.Grant{
		{authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)},
		{authz.NewGrant(authz.ScopeProjectWrite, authz.WildcardResource)},
		{authz.NewGrant(authz.ScopeChatWrite, authz.WildcardResource)},
		{authz.NewGrant(authz.ScopeMemoryRead, f.project.ID.String())},
		{authz.NewGrant(authz.ScopeMemoryWrite, other.ID.String())},
	} {
		_, err := f.service.Remember(f.context(t.Context(), grants...), f.principal, f.remember("denied fact"))
		var permission *ExternalAuthorizationError
		require.ErrorAs(t, err, &permission, grants[0].Scope)
		require.Equal(t, "memory:write", permission.RequiredScope)
	}

	ctx := f.writer(t.Context())
	assistant := f.principal
	assistant.Surface = SurfaceProjectAssistant
	_, err = f.service.Remember(ctx, assistant, f.remember("assistant fact"))
	require.ErrorIs(t, err, ErrForbidden)
	mismatch := f.principal
	mismatch.UserID = "user_fixture_wrong_actor"
	_, err = f.service.Remember(ctx, mismatch, f.remember("mismatch fact"))
	require.ErrorIs(t, err, ErrForbidden)
	limited := *f.service
	limited.budget = denyBudget()
	_, err = limited.Remember(ctx, f.principal, f.remember("limited fact"))
	require.Error(t, err)

	count, err := testrepo.New(f.db).CountMemoryMemosFixture(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
	audits, err := audittest.AuditLogCountByAction(t.Context(), f.db, audit.ActionMemoryMemoCreate)
	require.NoError(t, err)
	require.Zero(t, audits)
}

func TestRememberRefusesWhenFlagOffOrInputInvalid(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	ctx := f.writer(t.Context())

	for _, input := range []RememberInput{
		{ProjectSlug: f.project.Slug, Content: "   "},
		{ProjectSlug: f.project.Slug, Content: strings.Repeat("x", memoMaxRunes+1)},
		{ProjectSlug: f.project.Slug, Content: "fact", ChatID: "not-a-uuid"},
		{Content: "no project"},
		{ProjectSlug: f.project.Slug, ProjectID: f.project.ID.String(), Content: "two projects"},
	} {
		_, err := f.service.Remember(ctx, f.principal, input)
		require.ErrorIs(t, err, errTeamMemoryInvalid)
	}

	// A chat from another project is not a valid source.
	other, err := projectsrepo.New(f.db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{OrganizationID: f.principal.OrganizationID, Name: "Other memo chats", Slug: "other-memo-chats"})
	require.NoError(t, err)
	foreign := seedRecallChat(t, ctx, f.db, other.ID, f.principal.OrganizationID, "user_fixture_other_owner", "foreign-"+uuid.NewString(), "Foreign", uuid.NullUUID{})
	_, err = f.service.Remember(ctx, f.principal, RememberInput{ProjectSlug: f.project.Slug, Content: "fact", ChatID: foreign.String()})
	require.ErrorIs(t, err, ErrRiskReadNotFound)

	f.flags.SetFlag(feature.FlagTeamMemory, f.principal.OrganizationID, false)
	_, err = f.service.Remember(ctx, f.principal, f.remember("flag off"))
	require.ErrorIs(t, err, errTeamMemoryNotEnabled)

	count, err := testrepo.New(f.db).CountMemoryMemosFixture(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestRememberFailsClosedWhenAuditFails(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	ctx := f.writer(t.Context())
	require.NoError(t, audittest.RejectAction(ctx, f.db, audit.ActionMemoryMemoCreate))
	_, err := f.service.Remember(ctx, f.principal, f.remember("audit failure fact"))
	require.Error(t, err)
	count, err := testrepo.New(f.db).CountMemoryMemosFixture(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestTeamMemoryUnknownProjectRefusesLikeMissingGrant(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	ctx := f.writer(t.Context())

	_, unknown := f.service.Remember(ctx, f.principal, RememberInput{ProjectSlug: "no-such-memory-project", Content: "fact"})
	var unknownDenied *ExternalAuthorizationError
	require.ErrorAs(t, unknown, &unknownDenied)

	other, err := projectsrepo.New(f.db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{OrganizationID: f.principal.OrganizationID, Name: "Ungranted memory project", Slug: "ungranted-memory-project"})
	require.NoError(t, err)
	_, ungranted := f.service.Remember(ctx, f.principal, RememberInput{ProjectSlug: other.Slug, Content: "fact"})
	var ungrantedDenied *ExternalAuthorizationError
	require.ErrorAs(t, ungranted, &ungrantedDenied)

	unknownResult, ok := teamMemoryToolResult(unknown)
	require.True(t, ok)
	ungrantedResult, ok := teamMemoryToolResult(ungranted)
	require.True(t, ok)
	require.Equal(t, ungrantedResult.Content, unknownResult.Content)
}

func TestTeamMemoryBudgetChargedOnlyAfterGrantAndFlag(t *testing.T) {
	t.Parallel()
	f := newTeamMemoryFixture(t)
	limited := *f.service
	limited.budget = denyBudget()

	// A spent budget never masks a missing grant or a disabled flag.
	other, err := projectsrepo.New(f.db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{OrganizationID: f.principal.OrganizationID, Name: "Unmetered memory project", Slug: "unmetered-memory-project"})
	require.NoError(t, err)
	ctx := f.writer(t.Context())
	_, err = limited.Remember(ctx, f.principal, RememberInput{ProjectSlug: other.Slug, Content: "ungranted"})
	var denied *ExternalAuthorizationError
	require.ErrorAs(t, err, &denied)

	f.flags.SetFlag(feature.FlagTeamMemory, f.principal.OrganizationID, false)
	_, err = limited.Remember(ctx, f.principal, f.remember("flag off"))
	require.ErrorIs(t, err, errTeamMemoryNotEnabled)
}
