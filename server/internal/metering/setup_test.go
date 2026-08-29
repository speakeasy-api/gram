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

func seedMeteringOrganization(t *testing.T, conn *pgxpool.Pool, organizationID string) {
	t.Helper()
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:       organizationID,
		Name:     "Metering Test Organization",
		Slug:     organizationID,
		WorkosID: conv.PtrToPGText(conv.PtrEmpty("workos-" + organizationID)),
	})
	require.NoError(t, err)
}

func newMeteringPostgres(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	conn, err := infra.CloneTestDatabase(t, "metering_"+strings.ReplaceAll(uuid.NewString(), "-", ""))
	require.NoError(t, err)

	organizationID := "org_" + uuid.NewString()
	seedMeteringOrganization(t, conn, organizationID)
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

func upsertMeteringDirectoryUser(
	t *testing.T,
	conn *pgxpool.Pool,
	organizationID, userID, email string,
	attributes map[string]string,
	linkByUserID bool,
) (uuid.UUID, string) {
	t.Helper()
	encodedAttributes, err := json.Marshal(attributes)
	require.NoError(t, err)
	directoryUserID := pgtype.Text{}
	if linkByUserID {
		directoryUserID = conv.ToPGText(userID)
	}
	workosDirectoryUserID := "directory-" + uuid.NewString()
	now := conv.ToPGTimestamptz(time.Now().UTC())
	id, err := directoryrepo.New(conn).UpsertDirectoryUser(t.Context(), directoryrepo.UpsertDirectoryUserParams{
		OrganizationID:        organizationID,
		UserID:                directoryUserID,
		WorkosDirectoryUserID: workosDirectoryUserID,
		Email:                 conv.ToPGText(email),
		Attributes:            encodedAttributes,
		WorkosCreatedAt:       now,
		WorkosUpdatedAt:       now,
		WorkosLastEventID:     pgtype.Text{},
		RestoreDeleted:        true,
	})
	require.NoError(t, err)
	return id, workosDirectoryUserID
}

func seedMeteringDirectoryUser(t *testing.T, conn *pgxpool.Pool, organizationID, userID, email, division, department string, linkByUserID bool) {
	t.Helper()
	upsertMeteringDirectoryUser(t, conn, organizationID, userID, email, map[string]string{
		"division_name":   division,
		"department_name": department,
	}, linkByUserID)
}

type meteringDirectoryFacets struct {
	DivisionName   string
	DepartmentName string
	JobTitle       string
	EmployeeType   string
	CostCenterName string
	Groups         []string
}

func seedMeteringFacetUser(
	t *testing.T,
	conn *pgxpool.Pool,
	organizationID, userID, email string,
	facets meteringDirectoryFacets,
	linkDirectoryByUserID bool,
) {
	t.Helper()
	seedMeteringAccount(t, conn, organizationID, userID, email)
	directoryUserID, workosDirectoryUserID := upsertMeteringDirectoryUser(
		t,
		conn,
		organizationID,
		userID,
		email,
		map[string]string{
			"division_name":    facets.DivisionName,
			"department_name":  facets.DepartmentName,
			"job_title":        facets.JobTitle,
			"employee_type":    facets.EmployeeType,
			"cost_center_name": facets.CostCenterName,
		},
		linkDirectoryByUserID,
	)
	now := conv.ToPGTimestamptz(time.Now().UTC())
	queries := directoryrepo.New(conn)
	for _, name := range facets.Groups {
		workosDirectoryGroupID := "group-" + uuid.NewString()
		directoryGroupID, err := queries.UpsertDirectoryGroup(t.Context(), directoryrepo.UpsertDirectoryGroupParams{
			OrganizationID:         organizationID,
			WorkosDirectoryGroupID: workosDirectoryGroupID,
			Name:                   name,
			Attributes:             []byte(`{}`),
			WorkosCreatedAt:        now,
			WorkosUpdatedAt:        now,
			WorkosLastEventID:      pgtype.Text{},
		})
		require.NoError(t, err)
		_, err = queries.OpenDirectoryUserGroupMembership(t.Context(), directoryrepo.OpenDirectoryUserGroupMembershipParams{
			DirectoryUserID:        directoryUserID,
			DirectoryGroupID:       directoryGroupID,
			WorkosDirectoryUserID:  workosDirectoryUserID,
			WorkosDirectoryGroupID: workosDirectoryGroupID,
			WorkosCreatedAt:        now,
		})
		require.NoError(t, err)
	}
}

func seedMeteringUser(t *testing.T, conn *pgxpool.Pool, organizationID, userID, email, division, department string, linkDirectoryByUserID bool) {
	t.Helper()
	seedMeteringAccount(t, conn, organizationID, userID, email)
	seedMeteringDirectoryUser(t, conn, organizationID, userID, email, division, department, linkDirectoryByUserID)
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
