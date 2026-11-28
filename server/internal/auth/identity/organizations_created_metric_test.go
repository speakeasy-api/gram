package identity_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgmetrics"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgmetrics/orgmetricstest"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const (
	mirrorUserID       = "user-login-mirror"
	mirrorWorkosUserID = "user_01LOGINMIRROR"
	mirrorWorkosOrgID  = "org_01LOGINMIRROR"
	mirrorMembershipID = "om_01LOGINMIRROR"
)

// memberSyncWorkOS adds the per-user membership listing and user external ID
// sync that the WorkOS stub lacks, so it satisfies identity.WorkOSClient.
type memberSyncWorkOS struct {
	*workos.StubClient
}

func (c *memberSyncWorkOS) ListUserMemberships(ctx context.Context, userID string) ([]workos.Member, error) {
	orgs, err := c.ListOrganizations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list stub organizations: %w", err)
	}

	var members []workos.Member
	for _, org := range orgs {
		orgMembers, err := c.ListMembers(ctx, org.ID)
		if err != nil {
			return nil, fmt.Errorf("list stub members of %q: %w", org.ID, err)
		}
		for _, member := range orgMembers {
			if member.UserID == userID {
				members = append(members, member)
			}
		}
	}
	return members, nil
}

func (c *memberSyncWorkOS) EnsureUserExternalID(context.Context, string, string) error {
	return nil
}

// newLoginMirrorResolver returns a resolver over an isolated database, that
// database, and a reader over the organizations created counter. The WorkOS
// stub reports one membership in a WorkOS organization with the given
// external ID.
func newLoginMirrorResolver(t *testing.T, workosExternalID string) (context.Context, *identity.Resolver, *pgxpool.Pool, *orgmetricstest.Reader) {
	t.Helper()

	ctx := t.Context()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	_, err = userRepo.New(conn).UpsertUser(ctx, userRepo.UpsertUserParams{
		ID:          mirrorUserID,
		Email:       "login-mirror@example.com",
		DisplayName: "Login Mirror",
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       false,
	})
	require.NoError(t, err)

	stub := workos.NewStubClient()
	stub.UpsertOrganization(workos.Organization{
		ID:         mirrorWorkosOrgID,
		Name:       "Login Mirror Org",
		ExternalID: workosExternalID,
		CreatedAt:  "",
		UpdatedAt:  "",
	})
	stub.UpsertOrganizationMembership(workos.Member{
		ID:             mirrorMembershipID,
		UserID:         mirrorWorkosUserID,
		OrganizationID: mirrorWorkosOrgID,
		Organization:   "Login Mirror Org",
		RoleSlugs:      []string{"member"},
		Status:         "active",
		CreatedAt:      "",
		UpdatedAt:      "",
	})

	orgs := orgRepo.New(conn)
	resolver := identity.NewResolver(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		nil, // redisClient
		"",  // idpBaseURL
		"",  // idpClientID
		nil, // idpClient
		&memberSyncWorkOS{StubClient: stub},
		orgs,
		userRepo.New(conn),
		nil, // pylon
		nil, // posthog
		nil, // growth signals
		"",  // cache suffix
	)

	created, reader := orgmetricstest.New(t)
	resolver.SetOrganizationsCreated(created)

	return ctx, resolver, conn, reader
}

func TestSyncMembershipsFromWorkOS_RecordsLoginMirrorCreation(t *testing.T) {
	t.Parallel()

	ctx, resolver, conn, reader := newLoginMirrorResolver(t, "")
	orgs := orgRepo.New(conn)

	require.NoError(t, resolver.SyncMembershipsFromWorkOS(ctx, mirrorUserID, mirrorWorkosUserID))

	_, err := orgs.GetOrganizationByWorkosID(ctx, pgtype.Text{String: mirrorWorkosOrgID, Valid: true})
	require.NoError(t, err, "membership sync inserts the organization")
	reader.RequireOnly(t, orgmetrics.CreationSourceLoginMirror, 1)
}

func TestSyncMembershipsFromWorkOS_RepeatSyncRecordsNothingMore(t *testing.T) {
	t.Parallel()

	ctx, resolver, _, reader := newLoginMirrorResolver(t, "")

	require.NoError(t, resolver.SyncMembershipsFromWorkOS(ctx, mirrorUserID, mirrorWorkosUserID))
	reader.RequireOnly(t, orgmetrics.CreationSourceLoginMirror, 1)

	// The row now resolves by workos_id, so the second sync leaves it alone.
	require.NoError(t, resolver.SyncMembershipsFromWorkOS(ctx, mirrorUserID, mirrorWorkosUserID))
	reader.RequireOnly(t, orgmetrics.CreationSourceLoginMirror, 1)
}

func TestSyncMembershipsFromWorkOS_LinkingExistingOrganizationRecordsNothing(t *testing.T) {
	t.Parallel()

	const gramOrgID = "login-mirror-existing-org"
	ctx, resolver, conn, reader := newLoginMirrorResolver(t, gramOrgID)
	orgs := orgRepo.New(conn)

	// The Speakeasy row exists under the WorkOS external ID but has no
	// workos_id yet, so membership sync links it instead of inserting.
	_, err := orgs.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:             gramOrgID,
		Name:           "Login Mirror Org",
		Slug:           "login-mirror-existing-org",
		WorkosID:       pgtype.Text{String: "", Valid: false},
		Whitelisted:    pgtype.Bool{Bool: false, Valid: false},
		CreationSource: pgtype.Text{String: "", Valid: false},
		DefaultHost:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	require.NoError(t, resolver.SyncMembershipsFromWorkOS(ctx, mirrorUserID, mirrorWorkosUserID))

	linked, err := orgs.GetOrganizationByWorkosID(ctx, pgtype.Text{String: mirrorWorkosOrgID, Valid: true})
	require.NoError(t, err)
	require.Equal(t, gramOrgID, linked.ID, "membership sync links the existing organization")
	require.Empty(t, reader.Counts(t))
}

// A sync that loses the insert race to another writer, such as a signup
// transaction committing the same organization, updates the row and records
// nothing; the writer that inserted it owns the count.
func TestSyncMembershipsFromWorkOS_LosingInsertRaceRecordsNothing(t *testing.T) {
	t.Parallel()

	const gramOrgID = "login-mirror-raced-org"
	ctx, resolver, conn, reader := newLoginMirrorResolver(t, gramOrgID)

	// The holder inserts the row and stays uncommitted, so the sync's lookups
	// miss it and its upsert waits on the holder's unique key.
	holder := testenv.BeginTx(t, ctx, conn)
	_, err := orgRepo.New(holder).UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:             gramOrgID,
		Name:           "Login Mirror Org",
		Slug:           "login-mirror-raced-org",
		WorkosID:       pgtype.Text{String: mirrorWorkosOrgID, Valid: true},
		Whitelisted:    pgtype.Bool{Bool: false, Valid: false},
		CreationSource: pgtype.Text{String: "", Valid: false},
		DefaultHost:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	syncErr := make(chan error, 1)
	go func() {
		syncErr <- resolver.SyncMembershipsFromWorkOS(ctx, mirrorUserID, mirrorWorkosUserID)
	}()

	testenv.WaitForBackendsBlockedBy(t, ctx, conn, testenv.BackendPID(holder), 1)
	require.NoError(t, holder.Commit(ctx))
	require.NoError(t, <-syncErr)

	linked, err := orgRepo.New(conn).GetOrganizationByWorkosID(ctx, pgtype.Text{String: mirrorWorkosOrgID, Valid: true})
	require.NoError(t, err)
	require.Equal(t, gramOrgID, linked.ID, "the sync updates the row the holder inserted")
	require.Empty(t, reader.Counts(t))
}
