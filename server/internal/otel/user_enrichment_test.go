package otel

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/singleflight"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestUserEnrichmentAttributes(t *testing.T) {
	t.Parallel()

	enrichment := userEnrichment{
		DirectoryID: "directory-user-id",
		DirectoryAttributes: map[string]any{
			"active":     true,
			"department": nil,
			"mixed":      []any{"one", float64(2), true},
			"nested":     map[string]any{"level": float64(7)},
			"skills":     []any{"Go", "SQL"},
		},
		DirectoryGroupIDs:   []string{"directory-group-id"},
		DirectoryGroupNames: []string{"Developers"},
		Roles:               []string{"member", "tool-author"},
	}

	require.ElementsMatch(t, []attribute.KeyValue{
		DirectoryID("directory-user-id"),
		DirectoryAttribute("active").Bool(true),
		DirectoryAttribute("mixed").Slice(
			attribute.StringValue("one"),
			attribute.Float64Value(2),
			attribute.BoolValue(true),
		),
		DirectoryAttribute("nested").String(`{"level":7}`),
		DirectoryAttribute("skills").Slice(attribute.StringValue("Go"), attribute.StringValue("SQL")),
		DirectoryGroupIDs([]string{"directory-group-id"}),
		DirectoryGroupNames([]string{"Developers"}),
		GramUserRoles([]string{"member", "tool-author"}),
	}, enrichment.attributes())
	require.Equal(t, "directory.attribute.active", string(DirectoryAttribute("active")))
}

func TestFetchUserEnrichmentLoadsDirectoryAndRoles(t *testing.T) {
	t.Parallel()

	db := newTestDatabase(t)
	seed := seedUserEnrichment(t, db)
	enrichmentCache := cache.NewTypedObjectCache[cachedUserEnrichment](testenv.NewLogger(t), cache.NoopCache, cache.SuffixNone)
	loads := singleflight.Group{}

	got, err := fetchUserEnrichment(t.Context(), db, &enrichmentCache, &loads, seed.organizationID, " User@Example.Invalid ")

	require.NoError(t, err)
	require.Equal(t, seed.want, got)
}

func TestFetchUserEnrichmentUsesCache(t *testing.T) {
	t.Parallel()

	db := newTestDatabase(t)
	seed := seedUserEnrichment(t, db)
	enrichmentCache := cache.NewTypedObjectCache[cachedUserEnrichment](testenv.NewLogger(t), testenv.NewMemoryCache(), cache.SuffixNone)
	loads := singleflight.Group{}

	first, err := fetchUserEnrichment(t.Context(), db, &enrichmentCache, &loads, seed.organizationID, seed.email)
	require.NoError(t, err)
	require.Equal(t, seed.want, first)

	db.Close()
	second, err := fetchUserEnrichment(t.Context(), db, &enrichmentCache, &loads, seed.organizationID, seed.email)
	require.NoError(t, err)
	require.Equal(t, first, second)
}

func TestFetchUserEnrichmentEnforcesOrganizationScope(t *testing.T) {
	t.Parallel()

	db := newTestDatabase(t)
	seed := seedUserEnrichment(t, db)
	enrichmentCache := cache.NewTypedObjectCache[cachedUserEnrichment](testenv.NewLogger(t), cache.NoopCache, cache.SuffixNone)
	loads := singleflight.Group{}

	got, err := fetchUserEnrichment(t.Context(), db, &enrichmentCache, &loads, "other-organization-id", seed.email)

	require.NoError(t, err)
	require.Empty(t, got)
}

func TestFetchUserEnrichmentReturnsEmptyWithoutMatchingUser(t *testing.T) {
	t.Parallel()

	db := newTestDatabase(t)
	enrichmentCache := cache.NewTypedObjectCache[cachedUserEnrichment](testenv.NewLogger(t), cache.NoopCache, cache.SuffixNone)
	loads := singleflight.Group{}

	got, err := fetchUserEnrichment(t.Context(), db, &enrichmentCache, &loads, "organization-id", "missing@example.invalid")

	require.NoError(t, err)
	require.Empty(t, got)
}

func TestFetchUserEnrichmentDoesNotCacheLookupFailure(t *testing.T) {
	t.Parallel()

	const organizationID = "organization-id"
	const email = "user@example.invalid"

	db := newTestDatabase(t)
	db.Close()
	enrichmentCache := cache.NewTypedObjectCache[cachedUserEnrichment](testenv.NewLogger(t), testenv.NewMemoryCache(), cache.SuffixNone)
	loads := singleflight.Group{}

	got, err := fetchUserEnrichment(t.Context(), db, &enrichmentCache, &loads, organizationID, email)

	require.Error(t, err)
	require.Empty(t, got)

	got, err = fetchUserEnrichment(t.Context(), db, &enrichmentCache, &loads, organizationID, email)
	require.Error(t, err)
	require.Empty(t, got)
}

func TestFetchUserEnrichmentBoundsLookupDuration(t *testing.T) {
	t.Parallel()

	enrichmentCache := cache.NewTypedObjectCache[cachedUserEnrichment](testenv.NewLogger(t), cache.NoopCache, cache.SuffixNone)
	loads := singleflight.Group{}

	got, err := fetchUserEnrichment(t.Context(), blockingUserEnrichmentDB{}, &enrichmentCache, &loads, "organization-id", "user@example.invalid")

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Empty(t, got)
}

type blockingUserEnrichmentDB struct{}

func (blockingUserEnrichmentDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected exec")
}

func (blockingUserEnrichmentDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected query")
}

func (blockingUserEnrichmentDB) QueryRow(ctx context.Context, _ string, _ ...any) pgx.Row {
	<-ctx.Done()
	return userEnrichmentErrorRow{err: ctx.Err()}
}

type userEnrichmentErrorRow struct {
	err error
}

func (r userEnrichmentErrorRow) Scan(...any) error {
	return r.err
}

type userEnrichmentSeed struct {
	organizationID string
	email          string
	want           userEnrichment
}

func seedUserEnrichment(t *testing.T, db *pgxpool.Pool) userEnrichmentSeed {
	t.Helper()

	const organizationID = "organization-user-enrichment"
	const userID = "user-enrichment"
	const email = "user@example.invalid"
	const directoryUserID = "directory-user-enrichment"
	const directoryGroupID = "directory-group-enrichment"
	const role = "tool-author"
	syncedAt := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)

	_, err := organizationsrepo.New(db).UpsertOrganizationMetadata(t.Context(), organizationsrepo.UpsertOrganizationMetadataParams{
		ID:       organizationID,
		Name:     organizationID,
		Slug:     organizationID,
		WorkosID: conv.ToPGText("workos-organization-user-enrichment"),
	})
	require.NoError(t, err)

	_, err = usersrepo.New(db).UpsertUser(t.Context(), usersrepo.UpsertUserParams{
		ID:          userID,
		Email:       email,
		DisplayName: "User Enrichment",
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       false,
	})
	require.NoError(t, err)

	_, err = organizationsrepo.New(db).UpsertOrganizationUserRelationship(t.Context(), organizationsrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)

	directoryUserRowID, err := directoryrepo.New(db).UpsertDirectoryUser(t.Context(), directoryrepo.UpsertDirectoryUserParams{
		OrganizationID:        organizationID,
		UserID:                conv.ToPGText(userID),
		WorkosDirectoryUserID: directoryUserID,
		Email:                 conv.ToPGText(email),
		Attributes:            []byte(`{"active":true,"department":"Engineering","nullable":null}`),
		RestoreDeleted:        true,
		WorkosCreatedAt:       conv.ToPGTimestamptz(syncedAt),
		WorkosUpdatedAt:       conv.ToPGTimestamptz(syncedAt),
		WorkosLastEventID:     conv.ToPGText("event-directory-user-enrichment"),
	})
	require.NoError(t, err)

	directoryGroupRowID, err := directoryrepo.New(db).UpsertDirectoryGroup(t.Context(), directoryrepo.UpsertDirectoryGroupParams{
		OrganizationID:         organizationID,
		WorkosDirectoryGroupID: directoryGroupID,
		Name:                   "Developers",
		Attributes:             []byte(`{}`),
		WorkosCreatedAt:        conv.ToPGTimestamptz(syncedAt),
		WorkosUpdatedAt:        conv.ToPGTimestamptz(syncedAt),
		WorkosLastEventID:      conv.ToPGText("event-directory-group-enrichment"),
	})
	require.NoError(t, err)

	_, err = directoryrepo.New(db).OpenDirectoryUserGroupMembership(t.Context(), directoryrepo.OpenDirectoryUserGroupMembershipParams{
		DirectoryUserID:        directoryUserRowID,
		DirectoryGroupID:       directoryGroupRowID,
		WorkosDirectoryUserID:  directoryUserID,
		WorkosDirectoryGroupID: directoryGroupID,
		WorkosCreatedAt:        conv.ToPGTimestamptz(syncedAt),
	})
	require.NoError(t, err)

	err = accessrepo.New(db).UpsertGlobalRole(t.Context(), accessrepo.UpsertGlobalRoleParams{
		WorkosSlug:        role,
		WorkosName:        role,
		WorkosDescription: conv.ToPGText(role),
		WorkosCreatedAt:   conv.ToPGTimestamptz(syncedAt),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(syncedAt),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	inserted, err := accessrepo.New(db).UpsertOrganizationRoleAssignment(t.Context(), accessrepo.UpsertOrganizationRoleAssignmentParams{
		OrganizationID:     organizationID,
		WorkosUserID:       "workos-user-enrichment",
		WorkosRoleSlug:     role,
		UserID:             conv.ToPGText(userID),
		WorkosMembershipID: conv.ToPGText("membership-user-enrichment"),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(syncedAt),
		WorkosLastEventID:  conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), inserted)

	return userEnrichmentSeed{
		organizationID: organizationID,
		email:          email,
		want: userEnrichment{
			DirectoryID: directoryUserID,
			DirectoryAttributes: map[string]any{
				"active":     true,
				"department": "Engineering",
				"nullable":   nil,
			},
			DirectoryGroupIDs:   []string{directoryGroupID},
			DirectoryGroupNames: []string{"Developers"},
			Roles:               []string{role},
		},
	}
}
