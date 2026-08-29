package metering_test

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, ClickHouse: true})
	if err != nil {
		log.Fatalf("launch metering test infrastructure: %v", err)
	}
	infra = res
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup metering test infrastructure: %v", err)
	}
	os.Exit(code)
}

func newMeteringPostgres(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	conn, err := infra.CloneTestDatabase(t, "metering_"+strings.ReplaceAll(uuid.NewString(), "-", ""))
	require.NoError(t, err)

	organizationID := "org_" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:       organizationID,
		Name:     "Metering Test Organization",
		Slug:     organizationID,
		WorkosID: conv.PtrToPGText(conv.PtrEmpty("workos-" + organizationID)),
	})
	require.NoError(t, err)
	return conn, organizationID
}
func seedMeteringAccount(t *testing.T, conn *pgxpool.Pool, organizationID, userID, email string) {
	t.Helper()
	ctx := t.Context()
	_, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          userID,
		Email:       email,
		DisplayName: userID,
		PhotoUrl:    pgtype.Text{},
		Admin:       false,
	})
	require.NoError(t, err)
	_, err = orgrepo.New(conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)
}

func seedMeteringUser(t *testing.T, conn *pgxpool.Pool, organizationID, userID, email, division, department string, linkDirectoryByUserID bool) {
	t.Helper()
	ctx := t.Context()
	seedMeteringAccount(t, conn, organizationID, userID, email)

	attributes, err := json.Marshal(map[string]string{
		"division_name":   division,
		"department_name": department,
	})
	require.NoError(t, err)
	directoryUserID := pgtype.Text{}
	if linkDirectoryByUserID {
		directoryUserID = conv.ToPGText(userID)
	}
	now := conv.ToPGTimestamptz(time.Now().UTC())
	_, err = directoryrepo.New(conn).UpsertDirectoryUser(ctx, directoryrepo.UpsertDirectoryUserParams{
		OrganizationID:        organizationID,
		UserID:                directoryUserID,
		WorkosDirectoryUserID: "directory-" + userID,
		Email:                 conv.ToPGText(email),
		Attributes:            attributes,
		WorkosCreatedAt:       now,
		WorkosUpdatedAt:       now,
		WorkosLastEventID:     pgtype.Text{},
		RestoreDeleted:        true,
	})
	require.NoError(t, err)
}

func seedMeteringRole(t *testing.T, conn *pgxpool.Pool, organizationID, userID, slug string, global bool) {
	t.Helper()
	ctx := t.Context()
	now := conv.ToPGTimestamptz(time.Now().UTC())
	queries := accessrepo.New(conn)
	if global {
		require.NoError(t, queries.UpsertGlobalRole(ctx, accessrepo.UpsertGlobalRoleParams{
			WorkosSlug:        slug,
			WorkosName:        slug,
			WorkosDescription: pgtype.Text{},
			WorkosCreatedAt:   now,
			WorkosUpdatedAt:   now,
			WorkosLastEventID: pgtype.Text{},
		}))
	} else {
		_, err := queries.UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
			OrganizationID:    organizationID,
			WorkosSlug:        slug,
			WorkosName:        slug,
			WorkosDescription: pgtype.Text{},
			WorkosCreatedAt:   now,
			WorkosUpdatedAt:   now,
			WorkosLastEventID: pgtype.Text{},
		})
		require.NoError(t, err)
	}
	affected, err := queries.UpsertOrganizationRoleAssignment(ctx, accessrepo.UpsertOrganizationRoleAssignmentParams{
		OrganizationID:     organizationID,
		WorkosUserID:       "workos-" + userID,
		UserID:             conv.ToPGText(userID),
		WorkosMembershipID: conv.ToPGText("membership-" + userID),
		WorkosUpdatedAt:    now,
		WorkosLastEventID:  pgtype.Text{},
		WorkosRoleSlug:     slug,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), affected)
}
