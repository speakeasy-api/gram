package access

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

func TestService_ListDirectoryPeople_States(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	orgID := authCtx.ActiveOrganizationID
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, orgID)})

	suffix := uuid.NewString()[:8]
	linkedMember := seedOrgMember(t, ctx, ti, orgID, "linked-"+suffix+"@example.com")
	emailMember := seedOrgMember(t, ctx, ti, orgID, "email-member-"+suffix+"@example.com")
	outsider := seedNonMemberUser(t, ctx, ti, "outsider-"+suffix+"@example.com")

	// Linked profiles use the link even when the email points elsewhere.
	linkedProfile, _ := seedMappingDirectoryUser(t, ctx, ti.conn, orgID, linkedMember, "someone-else-"+suffix+"@example.com", `{}`)
	emailProfile, _ := seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "", "EMAIL-MEMBER-"+suffix+"@example.com", `{}`)
	outsiderProfile, _ := seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "", "outsider-"+suffix+"@example.com", `{}`)
	newcomerProfile, _ := seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "", "newcomer-"+suffix+"@example.com", `{}`)
	noEmailProfile, _ := seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "", "", `{}`)
	// Two live profiles sharing an email are never matched, even though one
	// account has that address.
	sharedEmail := "shared-" + suffix + "@example.com"
	seedOrgMember(t, ctx, ti, orgID, sharedEmail)
	sharedA, _ := seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "", sharedEmail, `{}`)
	sharedB, _ := seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "", sharedEmail, `{}`)

	result, err := ti.service.ListDirectoryPeople(ctx, &gen.ListDirectoryPeoplePayload{Limit: 100})
	require.NoError(t, err)
	require.Nil(t, result.NextCursor)

	byID := make(map[string]*gen.DirectoryPerson, len(result.People))
	for _, person := range result.People {
		byID[person.ID] = person
	}
	require.Len(t, byID, 7)

	requireState := func(profile uuid.UUID, state string, userID string) {
		t.Helper()
		person, ok := byID[profile.String()]
		require.True(t, ok, "profile %s missing", profile)
		require.Equal(t, state, person.State)
		if userID == "" {
			require.Nil(t, person.UserID)
		} else {
			require.NotNil(t, person.UserID)
			require.Equal(t, userID, *person.UserID)
		}
	}
	requireState(linkedProfile, "member", linkedMember)
	requireState(emailProfile, "member", emailMember)
	requireState(outsiderProfile, "account_not_member", outsider)
	requireState(newcomerProfile, "not_signed_in", "")
	requireState(noEmailProfile, "not_signed_in", "")
	requireState(sharedA, "ambiguous_email", "")
	requireState(sharedB, "ambiguous_email", "")

	require.Equal(t, &gen.DirectoryPeopleStateCounts{Member: 2, AccountNotMember: 1, NotSignedIn: 2, AmbiguousEmail: 2}, result.Counts)
}

func TestService_ListDirectoryPeople_NamesGroupsAndExclusions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	orgID := authCtx.ActiveOrganizationID
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, orgID)})

	now := time.Now().UTC()
	named, err := directoryrepo.New(ti.conn).UpsertDirectoryUser(ctx, directoryrepo.UpsertDirectoryUserParams{
		OrganizationID:        orgID,
		WorkosDirectoryUserID: "du_" + uuid.NewString(),
		Email:                 conv.ToPGText("named@example.com"),
		FirstName:             conv.ToPGText("Ada"),
		LastName:              conv.ToPGText("Lovelace"),
		Attributes:            []byte(`{"department_name":"Engineering"}`),
		RestoreDeleted:        true,
		WorkosCreatedAt:       conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:       conv.ToPGTimestamptz(now),
		WorkosLastEventID:     conv.ToPGText("event_named"),
	})
	require.NoError(t, err)

	live, liveWorkOS := seedMappingDirectoryGroupWithWorkOSID(t, ctx, ti.conn, orgID, "Beta")
	gone, goneWorkOS := seedMappingDirectoryGroupWithWorkOSID(t, ctx, ti.conn, orgID, "Alpha")
	addMappingGroupMember(t, ctx, ti.conn, named, "", live, liveWorkOS)
	addMappingGroupMember(t, ctx, ti.conn, named, "", gone, goneWorkOS)
	deleteMappingDirectoryGroup(t, ctx, ti.conn, goneWorkOS)

	// A tombstoned profile is not a live directory person.
	_, deletedWorkOS := seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "", "deleted@example.com", `{}`)
	_, err = directoryrepo.New(ti.conn).DeleteDirectoryUserByWorkOSID(ctx, directoryrepo.DeleteDirectoryUserByWorkOSIDParams{
		OrganizationID:        orgID,
		WorkosDeletedAt:       conv.ToPGTimestamptz(now),
		WorkosLastEventID:     conv.ToPGText("event_deleted"),
		WorkosDirectoryUserID: deletedWorkOS,
	})
	require.NoError(t, err)

	// Another organization's profiles are never returned.
	otherOrg := uuid.NewString()
	_, err = orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:   otherOrg,
		Name: "Other Org",
		Slug: "other-org-" + uuid.NewString()[:8],
	})
	require.NoError(t, err)
	seedMappingDirectoryUser(t, ctx, ti.conn, otherOrg, "", "named@example.com", `{}`)

	result, err := ti.service.ListDirectoryPeople(ctx, &gen.ListDirectoryPeoplePayload{Limit: 100})
	require.NoError(t, err)
	require.Len(t, result.People, 1)
	person := result.People[0]
	require.Equal(t, named.String(), person.ID)
	require.Equal(t, "named@example.com", person.Email)
	require.Equal(t, "Ada", conv.PtrValOr(person.FirstName, ""))
	require.Equal(t, "Lovelace", conv.PtrValOr(person.LastName, ""))
	require.Equal(t, []*gen.DirectoryPersonGroup{{ID: live.String(), Name: "Beta"}}, person.Groups)
	// The profile email is unique within the organization, so the other
	// organization's copy does not make it ambiguous.
	require.Equal(t, "not_signed_in", person.State)
}

func TestService_ListDirectoryPeople_Pages(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	orgID := authCtx.ActiveOrganizationID
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, orgID)})

	for i := range 5 {
		seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "", uuid.NewString()[:8]+"-"+string(rune('a'+i))+"@example.com", `{}`)
	}

	seen := make(map[string]struct{})
	var cursor *string
	pages := 0
	for {
		result, err := ti.service.ListDirectoryPeople(ctx, &gen.ListDirectoryPeoplePayload{Limit: 2, Cursor: cursor})
		require.NoError(t, err)
		pages++
		require.Equal(t, int64(5), result.Counts.NotSignedIn, "counts cover the organization on every page")
		for _, person := range result.People {
			_, dup := seen[person.ID]
			require.False(t, dup)
			seen[person.ID] = struct{}{}
		}
		if result.NextCursor == nil {
			break
		}
		cursor = result.NextCursor
	}
	require.Len(t, seen, 5)
	require.Equal(t, 3, pages)

	invalid := "not-a-uuid"
	_, err := ti.service.ListDirectoryPeople(ctx, &gen.ListDirectoryPeoplePayload{Limit: 2, Cursor: &invalid})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestService_ListDirectoryPeople_RequiresOrgRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, uuid.NewString())})

	_, err := ti.service.ListDirectoryPeople(ctx, &gen.ListDirectoryPeoplePayload{Limit: 10})
	requireOopsCode(t, err, oops.CodeForbidden)
}
