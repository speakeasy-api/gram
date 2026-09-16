package admin

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestOrganizationDirectoryHandoffLifecycle(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	const (
		organizationID       = "org_directory_handoff_lifecycle"
		workosOrganizationID = "org_workos_directory_handoff"
		operatorEmail        = "operator@example.test"
		token                = "scim-token-value"
	)
	createDirectoryHandoffOrganization(t, conn, organizationID, workosOrganizationID)
	client := workos.NewStubClient()
	client.SetDirectories(workosOrganizationID, workos.Directory{
		ID:             "directory_test_1",
		OrganizationID: workosOrganizationID,
		Type:           "OktaSCIMV2.0",
		Name:           "Directory",
		State:          "linked",
		CreatedAt:      "2026-09-15T00:00:00Z",
		UpdatedAt:      "2026-09-15T00:00:00Z",
	})
	svc.workos = client
	svc.directoryWorkOS = client
	svc.workosEnvironment = "development"
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID:   "admin-session",
		OIDCSubject: "operator-subject",
		Name:        "Test Operator",
		Email:       operatorEmail,
		HD:          "example.test",
	})

	setResult, err := svc.SetOrganizationDirectoryHandoff(ctx, &gen.SetOrganizationDirectoryHandoffPayload{
		AdminSessionToken: nil,
		OrganizationID:    organizationID,
		ScimBaseURL:       "https://directory.example.test/scim/v2",
		ScimToken:         token,
	})
	require.NoError(t, err)
	require.Equal(t, organizationID, setResult.OrganizationID)
	require.Equal(t, operatorEmail, setResult.SetBy)
	require.Len(t, setResult.TokenFingerprint, 8)
	require.Nil(t, setResult.WorkosDirectoryID)
	require.Nil(t, setResult.WorkosDirectoryState)

	stored, err := repo.New(conn).AdminGetOrganizationDirectoryHandoff(ctx, organizationID)
	require.NoError(t, err)
	plaintext, err := svc.applicationEncryption.Decrypt(conv.FromPGTextOrEmpty[string](stored.DirectoryScimTokenEncrypted))
	require.NoError(t, err)
	require.Equal(t, token, plaintext)
	require.NotEqual(t, token, conv.FromPGTextOrEmpty[string](stored.DirectoryScimTokenEncrypted))

	setAudit, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionDirectoryHandoffSet)
	require.NoError(t, err)
	setMetadata, err := audittest.DecodeAuditData(setAudit.Metadata)
	require.NoError(t, err)
	require.Equal(t, "directory.example.test", setMetadata["base_url_host"])
	require.Equal(t, setResult.TokenFingerprint, setMetadata["token_fingerprint"])
	require.NotContains(t, string(setAudit.Metadata), token)

	getResult, err := svc.GetOrganizationDirectoryHandoff(ctx, &gen.GetOrganizationDirectoryHandoffPayload{
		AdminSessionToken: nil,
		OrganizationID:    organizationID,
	})
	require.NoError(t, err)
	require.Equal(t, "development", getResult.WorkosEnvironment)
	require.NotNil(t, getResult.Handoff)
	require.NotNil(t, getResult.Handoff.WorkosDirectoryID)
	require.Equal(t, "directory_test_1", *getResult.Handoff.WorkosDirectoryID)
	require.NotNil(t, getResult.Handoff.WorkosDirectoryState)
	require.Equal(t, "linked", *getResult.Handoff.WorkosDirectoryState)
	stored, err = repo.New(conn).AdminGetOrganizationDirectoryHandoff(ctx, organizationID)
	require.NoError(t, err)
	require.Equal(t, "directory_test_1", conv.FromPGTextOrEmpty[string](stored.DirectoryWorkosID))

	err = svc.ClearOrganizationDirectoryHandoff(ctx, &gen.ClearOrganizationDirectoryHandoffPayload{
		AdminSessionToken: nil,
		OrganizationID:    organizationID,
	})
	require.NoError(t, err)
	_, err = repo.New(conn).AdminGetOrganizationDirectoryHandoff(ctx, organizationID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	clearAudit, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionDirectoryHandoffCleared)
	require.NoError(t, err)
	clearMetadata, err := audittest.DecodeAuditData(clearAudit.Metadata)
	require.NoError(t, err)
	require.Equal(t, setMetadata, clearMetadata)
	require.NotContains(t, string(clearAudit.Metadata), token)
}

func TestSetOrganizationDirectoryHandoffRollsBackWhenAuditFails(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	const organizationID = "org_directory_handoff_atomic"
	createDirectoryHandoffOrganization(t, conn, organizationID, "")
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID:   "admin-session",
		OIDCSubject: "operator-subject",
		Name:        "Test Operator",
		Email:       "operator@example.test",
		HD:          "example.test",
	})
	require.NoError(t, audittest.RejectAction(ctx, conn, audit.ActionDirectoryHandoffSet))

	_, err := svc.SetOrganizationDirectoryHandoff(ctx, &gen.SetOrganizationDirectoryHandoffPayload{
		AdminSessionToken: nil,
		OrganizationID:    organizationID,
		ScimBaseURL:       "https://directory.example.test/scim/v2",
		ScimToken:         "token-that-must-not-persist",
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeUnexpected)
	_, err = repo.New(conn).AdminGetOrganizationDirectoryHandoff(ctx, organizationID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestValidateDirectorySCIMBaseURLRejectsUnsafeURLs(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"http://directory.example.test/scim/v2",
		"https:///scim/v2",
		"https://user:password@directory.example.test/scim/v2",
		"https://directory.example.test/scim/v2?token=secret",
		"https://directory.example.test/scim/v2#fragment",
	} {
		_, err := validateDirectorySCIMBaseURL(value)
		require.Error(t, err, value)
	}
}

func TestSelectDirectoryForHandoffRejectsAmbiguousDirectories(t *testing.T) {
	t.Parallel()

	directories := []workos.Directory{
		{ID: "directory_1", OrganizationID: "org_1", Type: "OktaSCIMV2.0", Name: "One", State: "unlinked", CreatedAt: "", UpdatedAt: ""},
		{ID: "directory_2", OrganizationID: "org_1", Type: "OktaSCIMV2.0", Name: "Two", State: "unlinked", CreatedAt: "", UpdatedAt: ""},
	}
	require.Nil(t, selectDirectoryForHandoff(directories, ""))
	require.Equal(t, "directory_2", selectDirectoryForHandoff(directories, "directory_2").ID)
	directories[1].State = "linked"
	require.Equal(t, "directory_2", selectDirectoryForHandoff(directories, "").ID)
	directories[0].State = "linked"
	require.Nil(t, selectDirectoryForHandoff(directories, ""))
}

func createDirectoryHandoffOrganization(t *testing.T, conn *pgxpool.Pool, organizationID, workosOrganizationID string) {
	t.Helper()
	now := time.Now().UTC()
	require.NoError(t, testrepo.New(conn).CreateOrganizationMetadataFixture(t.Context(), testrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 organizationID,
		Name:               "Directory Handoff Organization",
		Slug:               organizationID,
		GramAccountType:    "enterprise",
		WorkosID:           conv.ToPGTextEmpty(workosOrganizationID),
		Whitelisted:        true,
		FreeTrialStartedAt: conv.ToPGTimestamptz(now),
		FreeTrialEndsAt:    conv.ToPGTimestamptz(now.Add(14 * 24 * time.Hour)),
		DisabledAt:         pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		CreatedAt:          pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
	}))
}
