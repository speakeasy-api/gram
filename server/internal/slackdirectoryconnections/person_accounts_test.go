package slackdirectoryconnections_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/slack_directory_connections"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func personAccountsRequest(userID string) *gen.ListPersonAccountsPayload {
	return &gen.ListPersonAccountsPayload{SessionToken: nil, UserID: userID, Cursor: nil}
}

func TestPersonAccountsSelfReadDoesNotGrantAdministration(t *testing.T) {
	t.Parallel()
	ctx, f, _, m := mappingFixture(t)
	mapped, err := f.service.SetMapping(ctx, mappingRequest(m, &f.auth.UserID))
	require.NoError(t, err)
	self := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, f.auth.ActiveOrganizationID))
	result, err := f.service.ListPersonAccounts(self, personAccountsRequest(f.auth.UserID))
	require.NoError(t, err)
	require.Len(t, result.Accounts, 1)
	require.Equal(t, mapped, result.Accounts[0].Member)
	require.Equal(t, "current", result.Accounts[0].DirectoryStatus)
	require.NotNil(t, result.Accounts[0].LastFullSyncSucceededAt)
	serialized, err := json.Marshal(result) //nolint:musttag // Verify generated service views contain no credentials.
	require.NoError(t, err)
	require.NotContains(t, string(serialized), "synthetic-access-token")
	require.NotContains(t, string(serialized), "synthetic-refresh-token")
	_, err = f.service.GetMember(self, &gen.GetMemberPayload{SessionToken: nil, ID: m.ID})
	requireMappingCode(t, err, oops.CodeForbidden)
	_, err = f.service.SetMapping(self, mappingRequest(mapped, nil))
	requireMappingCode(t, err, oops.CodeForbidden)
	_, err = f.service.ListMembers(self, memberRequest())
	requireMappingCode(t, err, oops.CodeForbidden)
	require.Equal(t, mapped.MappingRevision, readMapping(t, ctx, f, m.ID).MappingRevision)
}

func TestPersonAccountsOtherPersonRequiresOrganizationAdmin(t *testing.T) {
	t.Parallel()
	ctx, f, _, m := mappingFixture(t)
	person := addPerson(t, ctx, f)
	_, err := f.service.SetMapping(ctx, mappingRequest(m, &person))
	require.NoError(t, err)
	self := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, f.auth.ActiveOrganizationID))
	_, err = f.service.ListPersonAccounts(self, personAccountsRequest(person))
	requireMappingCode(t, err, oops.CodeForbidden)
	result, err := f.service.ListPersonAccounts(ctx, personAccountsRequest(person))
	require.NoError(t, err)
	require.Len(t, result.Accounts, 1)
	require.Equal(t, person, result.Accounts[0].Member.Mapping.UserID)
	empty, err := f.service.ListPersonAccounts(self, personAccountsRequest(f.auth.UserID))
	require.NoError(t, err)
	require.Empty(t, empty.Accounts)
}

func TestPersonAccountsRequiresSessionAndOrganizationProductFeature(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	request := personAccountsRequest(f.auth.UserID)
	noSession := *f.auth
	noSession.SessionID = nil
	noID := *f.auth
	noID.UserID = ""
	support := *f.auth
	support.SupportOrganizationID = support.ActiveOrganizationID
	support.IsAdmin = true
	for _, denied := range []context.Context{t.Context(), contextvalues.SetAuthContext(ctx, &noSession), contextvalues.SetAuthContext(ctx, &noID), contextvalues.WithLegacyAPIKeyAuthorization(ctx, f.auth), contextvalues.WithValidatedSupportSession(ctx, &support)} {
		result, err := f.service.ListPersonAccounts(denied, request)
		require.Error(t, err)
		require.Nil(t, result)
	}
	require.NoError(t, f.productFeatures.SetFeatureEnabled(ctx, f.auth.ActiveOrganizationID, productfeatures.FeatureClaudeTagSupport, false))
	_, err := f.service.ListPersonAccounts(ctx, request)
	requireMappingCode(t, err, oops.CodeNotFound)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = f.service.ListPersonAccounts(cancelled, request)
	requireMappingCode(t, err, oops.CodeUnavailable)
}

func TestPersonAccountsRejectsInactiveTargetAndCaller(t *testing.T) {
	t.Parallel()
	ctx, f, _, m := mappingFixture(t)
	person := addPerson(t, ctx, f)
	_, err := f.service.SetMapping(ctx, mappingRequest(m, &person))
	require.NoError(t, err)
	require.NoError(t, repo.New(f.db).DeactivateSlackMappingPersonForTest(ctx, repo.DeactivateSlackMappingPersonForTestParams{OrganizationID: f.auth.ActiveOrganizationID, UserID: conv.ToPGText(person)}))
	_, err = f.service.ListPersonAccounts(ctx, personAccountsRequest(person))
	requireMappingCode(t, err, oops.CodeNotFound)
	former := *f.auth
	former.UserID = person
	_, err = f.service.ListPersonAccounts(contextvalues.SetAuthContext(ctx, &former), personAccountsRequest(person))
	requireMappingCode(t, err, oops.CodeNotFound)
	// A surviving org admin grant also cannot outlive the caller's membership.
	_, err = f.service.ListPersonAccounts(contextvalues.SetAuthContext(ctx, &former), personAccountsRequest(f.auth.UserID))
	requireMappingCode(t, err, oops.CodeNotFound)
	deleted := addPerson(t, ctx, f)
	require.NoError(t, repo.New(f.db).DeleteSlackMappingUserForTest(ctx, deleted))
	_, err = f.service.ListPersonAccounts(ctx, personAccountsRequest(deleted))
	requireMappingCode(t, err, oops.CodeNotFound)
}

func TestPersonAccountsDemoVisitorReadsActiveDemoPerson(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	visitor := *f.auth
	visitor.ActiveOrganizationID = constants.DemoOrganizationID
	visitor.OrganizationSlug = "acme-demo"
	f.auth = &visitor
	ctx = authztest.WithExactGrants(t, contextvalues.SetAuthContext(ctx, &visitor), authz.DemoScopeGrants()...)
	require.NoError(t, f.productFeatures.SetFeatureEnabled(ctx, constants.DemoOrganizationID, productfeatures.FeatureClaudeTagSupport, true))
	emptyTime := pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite}
	require.NoError(t, testrepo.New(f.db).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID: constants.DemoOrganizationID, Name: "Acme Demo Workspace", Slug: "acme-demo", GramAccountType: "demo",
		WorkosID: pgtype.Text{String: "", Valid: false}, Whitelisted: true,
		FreeTrialStartedAt: conv.ToPGTimestamptz(time.Now()), FreeTrialEndsAt: conv.ToPGTimestamptz(time.Now().Add(14 * 24 * time.Hour)),
		DisabledAt: emptyTime, CreatedAt: emptyTime,
	}))
	active, err := orgrepo.New(f.db).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{OrganizationID: constants.DemoOrganizationID, UserID: visitor.UserID})
	require.NoError(t, err)
	require.False(t, active, "Explore Demo keeps the visitor outside the demo membership directory")
	person := addPerson(t, ctx, f)
	queries := repo.New(f.db)
	connection, err := queries.CreateSlackDirectoryConnection(ctx, repo.CreateSlackDirectoryConnectionParams{
		OrganizationID: constants.DemoOrganizationID, SlackTeamID: "TEXAMPLE01", SlackTeamName: conv.ToPGText("Example workspace"),
		CredentialsEncrypted: conv.ToPGTextEmpty(""), GrantedScopes: []string{}, Generation: uuid.New(),
	})
	require.NoError(t, err)
	published := conv.ToPGTimestamptz(time.Now())
	_, err = queries.PublishSlackDirectorySync(ctx, repo.PublishSlackDirectorySyncParams{
		OrganizationID: constants.DemoOrganizationID, ID: connection.ID, Generation: connection.Generation, PublishedAt: published,
	})
	require.NoError(t, err)
	require.NoError(t, queries.UpsertSlackDirectoryMembershipBatch(ctx, repo.UpsertSlackDirectoryMembershipBatchParams{
		OrganizationID: constants.DemoOrganizationID, SlackTeamID: "TEXAMPLE01", LastSeenAt: published,
		UserIds: []string{"UEXAMPLE01"}, DisplayNames: []string{"Example person"}, Emails: []string{"person@demo.getgram.ai"},
		Statuses: []string{"active"}, MemberTypes: []string{"person"}, ProviderUpdatedAts: []pgtype.Timestamptz{emptyTime},
	}))
	_, err = queries.CreateSlackMappingForTest(ctx, repo.CreateSlackMappingForTestParams{
		OrganizationID: constants.DemoOrganizationID, SlackTeamID: "TEXAMPLE01", SlackUserID: "UEXAMPLE01", UserID: person,
	})
	require.NoError(t, err)
	result, err := f.service.ListPersonAccounts(ctx, personAccountsRequest(person))
	require.NoError(t, err)
	require.Len(t, result.Accounts, 1)
	require.Equal(t, person, result.Accounts[0].Member.Mapping.UserID)
	support := visitor
	support.SupportOrganizationID = constants.DemoOrganizationID
	support.IsAdmin = true
	supportCtx := contextvalues.WithValidatedSupportSession(ctx, &support)
	require.True(t, contextvalues.IsSupportSession(supportCtx))
	_, err = f.service.ListPersonAccounts(supportCtx, personAccountsRequest(person))
	requireMappingCode(t, err, oops.CodeForbidden)
	// The demo exception applies only to the caller; the subject must still be an active demo person.
	_, err = f.service.ListPersonAccounts(ctx, personAccountsRequest(visitor.UserID))
	requireMappingCode(t, err, oops.CodeNotFound)
	require.NoError(t, repo.New(f.db).DeactivateSlackMappingPersonForTest(ctx, repo.DeactivateSlackMappingPersonForTestParams{OrganizationID: constants.DemoOrganizationID, UserID: conv.ToPGText(person)}))
	_, err = f.service.ListPersonAccounts(ctx, personAccountsRequest(person))
	requireMappingCode(t, err, oops.CodeNotFound)
}

func TestPersonAccountsNeverAuthorizesByEmailOrForeignPerson(t *testing.T) {
	t.Parallel()
	ctx, f, _, m := mappingFixture(t)
	_, err := f.service.SetMapping(ctx, mappingRequest(m, &f.auth.UserID))
	require.NoError(t, err)
	foreign := "user_synthetic_foreign"
	// A real Gram user without membership. The forged caller below retains the session email.
	require.NoError(t, testrepo.New(f.db).InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{ID: foreign, Email: "foreign@demo.getgram.ai", DisplayName: "Synthetic foreign person"}))
	_, err = f.service.ListPersonAccounts(ctx, personAccountsRequest(foreign))
	requireMappingCode(t, err, oops.CodeNotFound)
	caller := *f.auth
	caller.UserID = foreign
	foreignCtx := contextvalues.SetAuthContext(ctx, &caller)
	_, err = f.service.ListPersonAccounts(authztest.WithExactGrants(t, foreignCtx), personAccountsRequest(f.auth.UserID))
	requireMappingCode(t, err, oops.CodeForbidden)
	_, err = f.service.ListPersonAccounts(foreignCtx, personAccountsRequest(foreign))
	requireMappingCode(t, err, oops.CodeNotFound)
	_, err = f.service.ListPersonAccounts(ctx, personAccountsRequest(conv.PtrValOr(f.auth.Email, "")))
	requireMappingCode(t, err, oops.CodeNotFound)
	caller = *f.auth
	caller.ActiveOrganizationID = "org_synthetic_other"
	require.NoError(t, f.productFeatures.SetFeatureEnabled(ctx, caller.ActiveOrganizationID, productfeatures.FeatureClaudeTagSupport, true))
	crossOrg := authztest.WithExactGrants(t, contextvalues.SetAuthContext(ctx, &caller), authz.NewGrant(authz.ScopeOrgAdmin, caller.ActiveOrganizationID))
	_, err = f.service.ListPersonAccounts(crossOrg, personAccountsRequest(f.auth.UserID))
	requireMappingCode(t, err, oops.CodeNotFound)
}

func TestPersonAccountsShowsEveryMembershipAndRetainedReviewState(t *testing.T) {
	t.Parallel()
	ctx, f, first, m := mappingFixture(t)
	_, err := f.service.SetMapping(ctx, mappingRequest(m, &f.auth.UserID))
	require.NoError(t, err)
	second := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE02")
	require.NoError(t, syncer(f, snapshot("UEXAMPLE01", "UEXAMPLE02")).Run(ctx, syncRequest(f, second), nil))
	for _, row := range members(t, ctx, f) {
		if row.ConnectionID.String() == second.ID {
			_, err = f.service.SetMapping(ctx, mappingRequest(readMapping(t, ctx, f, row.ID.String()), &f.auth.UserID))
			require.NoError(t, err)
		}
	}
	require.NoError(t, syncer(f, profileSnapshot("deactivated", "person", "example@demo.getgram.ai", "Synthetic person")).Run(ctx, syncRequest(f, first), nil))
	_, err = f.service.Disconnect(ctx, &gen.DisconnectPayload{SessionToken: nil, ID: first.ID, Generation: first.Generation})
	require.NoError(t, err)
	result, err := f.service.ListPersonAccounts(ctx, personAccountsRequest(f.auth.UserID))
	require.NoError(t, err)
	require.Len(t, result.Accounts, 3)
	for _, account := range result.Accounts {
		if account.Member.ID == m.ID {
			require.Equal(t, "deactivated", account.Member.Status)
			require.Equal(t, "needs_review", account.Member.MappingStatus)
			require.Equal(t, "stale", account.DirectoryStatus)
			require.Equal(t, "member_deactivated", *account.Member.MappingConflictReason)
		} else {
			require.Equal(t, "mapped", account.Member.MappingStatus)
			require.Equal(t, "current", account.DirectoryStatus)
		}
	}
	// Revoked mappings disappear, with no email-based replacement.
	_, err = f.service.SetMapping(ctx, mappingRequest(readMapping(t, ctx, f, m.ID), nil))
	require.NoError(t, err)
	result, err = f.service.ListPersonAccounts(ctx, personAccountsRequest(f.auth.UserID))
	require.NoError(t, err)
	require.Len(t, result.Accounts, 2)
}

func TestPersonAccountsPaginationAndEmptyState(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	request := personAccountsRequest(f.auth.UserID)
	result, err := f.service.ListPersonAccounts(ctx, request)
	require.NoError(t, err)
	require.NotNil(t, result.Accounts)
	require.Empty(t, result.Accounts)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	ids := make([]string, 51)
	for i := range ids {
		ids[i] = fmt.Sprintf("UEXAMPLE%03d", i)
	}
	require.NoError(t, syncer(f, snapshot(ids...)).Run(ctx, syncRequest(f, c), nil))
	for _, id := range ids {
		require.NoError(t, repo.New(f.db).ConfirmSlackIdentityMapping(ctx, repo.ConfirmSlackIdentityMappingParams{OrganizationID: f.auth.ActiveOrganizationID, SlackTeamID: c.WorkspaceID, SlackUserID: id, UserID: f.auth.UserID}))
	}
	result, err = f.service.ListPersonAccounts(ctx, request)
	require.NoError(t, err)
	require.Len(t, result.Accounts, 50)
	require.NotNil(t, result.NextCursor)
	request.Cursor = result.NextCursor
	last, err := f.service.ListPersonAccounts(ctx, request)
	require.NoError(t, err)
	require.Len(t, last.Accounts, 1)
	require.Nil(t, last.NextCursor)
	for _, account := range result.Accounts {
		require.NotEqual(t, account.Member.ID, last.Accounts[0].Member.ID)
	}
	request.Cursor = conv.PtrEmpty("invalid-cursor")
	_, err = f.service.ListPersonAccounts(ctx, request)
	requireMappingCode(t, err, oops.CodeBadRequest)
}

func TestPersonAccountsReconnectRetainsMappingWithStaleDirectory(t *testing.T) {
	t.Parallel()
	ctx, f, c, m := mappingFixture(t)
	_, err := f.service.SetMapping(ctx, mappingRequest(m, &f.auth.UserID))
	require.NoError(t, err)
	authorize(t, ctx, f, begin(t, ctx, f, &c.ID), c.WorkspaceID)
	result, err := f.service.ListPersonAccounts(ctx, personAccountsRequest(f.auth.UserID))
	require.NoError(t, err)
	require.Len(t, result.Accounts, 1)
	require.Equal(t, "stale", result.Accounts[0].DirectoryStatus)
	require.Equal(t, "mapped", result.Accounts[0].Member.MappingStatus)
	require.NotNil(t, result.Accounts[0].LastFullSyncSucceededAt)
}
