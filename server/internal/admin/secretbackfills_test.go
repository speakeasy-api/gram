package admin

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	assetsrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins/marketplacetokens"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestSecretBackfillsPreviewThenApplyBoth(t *testing.T) {
	t.Parallel()

	ctx, svc, db := newTestAdminService(t)
	ctx = withSecretBackfillOperator(ctx)
	platform := withSecretBackfillPlatformKey(t, svc, db)
	orgID := "org_" + uuid.NewString()
	seedOrg(t, ctx, db, orgFixture{id: orgID, name: "secret-backfills", slug: "secret-backfills-" + uuid.NewString()[:8], accountType: "enterprise"})
	projectID := seedSecretBackfillProject(t, ctx, db, orgID)
	connectionID := seedLegacyMarketplaceToken(t, ctx, db, projectID, "legacy-marketplace-token")
	runnerKeyID := seedRunnerKeyWithoutOrganization(t, ctx, db, orgID, projectID)

	preview, err := svc.GetSecretBackfills(ctx, &gen.GetSecretBackfillsPayload{})
	require.NoError(t, err)
	require.Equal(t, &gen.AdminMarketplaceTokenBackfill{Attributed: 0, Moved: 0, Skipped: 0, NeedsOrganizationKey: 0, Unattributed: 1, Legacy: 1, Hashed: 0}, preview.MarketplaceTokens)
	require.Equal(t, &gen.AdminRunnerKeyOrganizationBackfill{Filled: 0, Pending: 1, MissingOrganization: 0}, preview.RunnerKeyOrganizations)
	require.NotNil(t, preview.PlatformKeyFingerprint)
	require.Equal(t, platform.Fingerprint(), *preview.PlatformKeyFingerprint)

	stored, err := pluginsrepo.New(db).GetGitHubConnectionRowFixture(ctx, connectionID)
	require.NoError(t, err)
	require.Equal(t, "legacy-marketplace-token", stored.MarketplaceToken.String, "a preview writes nothing")

	tokens, err := svc.RunMarketplaceTokenBackfill(ctx, &gen.RunMarketplaceTokenBackfillPayload{PlatformKeyFingerprint: platform.Fingerprint()})
	require.NoError(t, err)
	require.Equal(t, &gen.AdminMarketplaceTokenBackfill{Attributed: 1, Moved: 1, Skipped: 0, NeedsOrganizationKey: 0, Unattributed: 0, Legacy: 0, Hashed: 1}, tokens)

	stored, err = pluginsrepo.New(db).GetGitHubConnectionRowFixture(ctx, connectionID)
	require.NoError(t, err)
	require.False(t, stored.MarketplaceToken.Valid, "the plaintext token is cleared")
	require.Equal(t, conv.ToPGText(orgID), stored.OrganizationID)
	require.Equal(t, marketplacetokens.Hash("legacy-marketplace-token"), stored.MarketplaceTokenHash.String)
	sealed, err := platform.Decrypt(stored.MarketplaceTokenEncrypted.String)
	require.NoError(t, err)
	require.Equal(t, "legacy-marketplace-token", sealed, "the copy is sealed with the platform key the page shows")

	runnerKeys, err := svc.RunRunnerKeyOrganizationBackfill(ctx, &gen.RunRunnerKeyOrganizationBackfillPayload{})
	require.NoError(t, err)
	require.Equal(t, &gen.AdminRunnerKeyOrganizationBackfill{Filled: 1, Pending: 0, MissingOrganization: 0}, runnerKeys)
	runnerKey, err := testrepo.New(db).GetFunctionsAccessOrganizationFixture(ctx, runnerKeyID)
	require.NoError(t, err)
	require.Equal(t, conv.ToPGText(orgID), runnerKey.OrganizationID)

	tokens, err = svc.RunMarketplaceTokenBackfill(ctx, &gen.RunMarketplaceTokenBackfillPayload{PlatformKeyFingerprint: platform.Fingerprint()})
	require.NoError(t, err)
	require.Equal(t, &gen.AdminMarketplaceTokenBackfill{Attributed: 0, Moved: 0, Skipped: 0, NeedsOrganizationKey: 0, Unattributed: 0, Legacy: 0, Hashed: 1}, tokens, "running again changes nothing")
	runnerKeys, err = svc.RunRunnerKeyOrganizationBackfill(ctx, &gen.RunRunnerKeyOrganizationBackfillPayload{})
	require.NoError(t, err)
	require.Equal(t, &gen.AdminRunnerKeyOrganizationBackfill{Filled: 0, Pending: 0, MissingOrganization: 0}, runnerKeys, "running again changes nothing")
}

func TestMarketplaceTokenBackfillIsUnavailableWithoutAPlatformKey(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)
	ctx = withSecretBackfillOperator(ctx)

	preview, err := svc.GetSecretBackfills(ctx, &gen.GetSecretBackfillsPayload{})
	require.NoError(t, err, "previews need no key")
	require.Nil(t, preview.PlatformKeyFingerprint)

	_, err = svc.RunMarketplaceTokenBackfill(ctx, &gen.RunMarketplaceTokenBackfillPayload{PlatformKeyFingerprint: "any"})
	requireSecretBackfillCode(t, err, oops.CodeUnavailable)

	_, err = svc.RunRunnerKeyOrganizationBackfill(ctx, &gen.RunRunnerKeyOrganizationBackfillPayload{})
	require.NoError(t, err, "the runner key organization backfill needs no key")
}

func TestSecretBackfillsNeedAStaffOperator(t *testing.T) {
	t.Parallel()

	ctx, svc, db := newTestAdminService(t)
	withSecretBackfillPlatformKey(t, svc, db)

	_, err := svc.RunMarketplaceTokenBackfill(ctx, &gen.RunMarketplaceTokenBackfillPayload{PlatformKeyFingerprint: "any"})
	requireSecretBackfillCode(t, err, oops.CodeUnauthorized)

	_, err = svc.RunRunnerKeyOrganizationBackfill(ctx, &gen.RunRunnerKeyOrganizationBackfillPayload{})
	requireSecretBackfillCode(t, err, oops.CodeUnauthorized)
}

func TestMarketplaceTokenBackfillRefusesAnotherKeysFingerprint(t *testing.T) {
	t.Parallel()

	ctx, svc, db := newTestAdminService(t)
	ctx = withSecretBackfillOperator(ctx)
	withSecretBackfillPlatformKey(t, svc, db)
	orgID := "org_" + uuid.NewString()
	seedOrg(t, ctx, db, orgFixture{id: orgID, name: "secret-backfills-fingerprint", slug: "secret-backfills-fingerprint-" + uuid.NewString()[:8], accountType: "enterprise"})
	projectID := seedSecretBackfillProject(t, ctx, db, orgID)
	connectionID := seedLegacyMarketplaceToken(t, ctx, db, projectID, "legacy-marketplace-token")

	// The operator checked the fingerprint of the key the process ran with
	// before it restarted with another one.
	_, err := svc.RunMarketplaceTokenBackfill(ctx, &gen.RunMarketplaceTokenBackfillPayload{PlatformKeyFingerprint: "fingerprint-of-the-previous-key"})
	requireSecretBackfillCode(t, err, oops.CodeConflict)

	stored, err := pluginsrepo.New(db).GetGitHubConnectionRowFixture(ctx, connectionID)
	require.NoError(t, err)
	require.Equal(t, "legacy-marketplace-token", stored.MarketplaceToken.String, "a refused run writes nothing")
	require.False(t, stored.MarketplaceTokenEncrypted.Valid, "a refused run seals nothing")
}

func TestSecretBackfillRefusesAnOverlappingRun(t *testing.T) {
	t.Parallel()

	ctx, svc, db := newTestAdminService(t)
	ctx = withSecretBackfillOperator(ctx)
	orgID := "org_" + uuid.NewString()
	seedOrg(t, ctx, db, orgFixture{id: orgID, name: "secret-backfills-lock", slug: "secret-backfills-lock-" + uuid.NewString()[:8], accountType: "enterprise"})
	projectID := seedSecretBackfillProject(t, ctx, db, orgID)
	runnerKeyID := seedRunnerKeyWithoutOrganization(t, ctx, db, orgID, projectID)

	// Another run holds the lock until its transaction ends.
	other, err := db.Begin(ctx) //nolint:glint // notestingrawsql: transaction boundary only; the lock uses a generated SQLc method.
	require.NoError(t, err)
	locked, err := repo.New(other).AdminTryLockSecretBackfill(ctx, secretBackfillRunnerKeyOrganizations)
	require.NoError(t, err)
	require.True(t, locked)

	_, err = svc.RunRunnerKeyOrganizationBackfill(ctx, &gen.RunRunnerKeyOrganizationBackfillPayload{})
	requireSecretBackfillCode(t, err, oops.CodeConflict)
	runnerKey, err := testrepo.New(db).GetFunctionsAccessOrganizationFixture(ctx, runnerKeyID)
	require.NoError(t, err)
	require.False(t, runnerKey.OrganizationID.Valid, "a refused run writes nothing")

	require.NoError(t, other.Rollback(ctx))
	report, err := svc.RunRunnerKeyOrganizationBackfill(ctx, &gen.RunRunnerKeyOrganizationBackfillPayload{})
	require.NoError(t, err, "the lock is free once the other run ends")
	require.Equal(t, int64(1), report.Filled)
}

func withSecretBackfillOperator(ctx context.Context) context.Context {
	return contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test", Name: "Staff Test"})
}

func withSecretBackfillPlatformKey(t *testing.T, svc *Service, db *pgxpool.Pool) *encryption.Client {
	t.Helper()

	platform := testenv.NewEncryptionClient(t)
	svc.SetSecretBackfillPlatformKey(&SecretBackfillPlatformKey{
		Platform: platform,
		Tokens:   marketplacetokens.WithoutOrganizationEncryption(testenv.NewLogger(t), db, platform),
	})

	return platform
}

func requireSecretBackfillCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

func seedSecretBackfillProject(t *testing.T, ctx context.Context, db *pgxpool.Pool, orgID string) uuid.UUID {
	t.Helper()

	// Project names and slugs are capped at 40 characters.
	slug := "p-" + uuid.NewString()[:8]
	project, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: slug, Slug: slug, OrganizationID: orgID})
	require.NoError(t, err)

	return project.ID
}

// seedLegacyMarketplaceToken stores a GitHub connection the way a publish did
// before tokens were hashed: no organization, the token in plaintext.
func seedLegacyMarketplaceToken(t *testing.T, ctx context.Context, db *pgxpool.Pool, projectID uuid.UUID, token string) uuid.UUID {
	t.Helper()

	q := pluginsrepo.New(db)
	conn, err := q.UpsertGitHubConnection(ctx, pluginsrepo.UpsertGitHubConnectionParams{
		ProjectID:                projectID,
		OrganizationID:           pgtype.Text{String: "", Valid: false},
		InstallationID:           1,
		RepoOwner:                "backfill-owner",
		RepoName:                 "backfill-" + uuid.NewString(),
		PublishedMcpFingerprints: nil,
		PublishedHooksVersion:    pgtype.Text{String: "", Valid: false},
		PublishedHooksConfig:     nil,
	})
	require.NoError(t, err)
	require.NoError(t, q.PlantMarketplaceTokenFixture(ctx, pluginsrepo.PlantMarketplaceTokenFixtureParams{
		OrganizationID: pgtype.Text{String: "", Valid: false},
		LegacyToken:    conv.ToPGText(token),
		TokenHash:      pgtype.Text{String: "", Valid: false},
		PlatformCopy:   pgtype.Text{String: "", Valid: false},
		OrgCopy:        pgtype.Text{String: "", Valid: false},
		ID:             conn.ID,
	}))

	return conn.ID
}

// seedRunnerKeyWithoutOrganization stores a function runner key the way a
// deploy did before functions_access had an organization_id.
func seedRunnerKeyWithoutOrganization(t *testing.T, ctx context.Context, db *pgxpool.Pool, orgID string, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	deployments := deploymentsrepo.New(db)
	deploymentID, err := deployments.InsertDeployment(ctx, deploymentsrepo.InsertDeploymentParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
		UserID:         "user-" + uuid.NewString(),
		IdempotencyKey: uuid.NewString(),
	})
	require.NoError(t, err)
	asset, err := assetsrepo.New(db).CreateAsset(ctx, assetsrepo.CreateAssetParams{
		Name: "fn.zip", Url: "file://fn.zip", ProjectID: projectID,
		OrganizationID: orgID, Sha256: "fn-" + uuid.NewString(),
		Kind: "functions", ContentType: "application/zip", ContentLength: 1,
	})
	require.NoError(t, err)
	function, err := deployments.UpsertDeploymentFunctionsAsset(ctx, deploymentsrepo.UpsertDeploymentFunctionsAssetParams{
		DeploymentID: deploymentID, AssetID: asset.ID, Name: "fn", Slug: "fn",
		Runtime: "nodejs:22", MemoryMib: pgtype.Int4{Int32: 128, Valid: true},
		Scale: pgtype.Int4{Int32: 1, Valid: true},
	})
	require.NoError(t, err)

	testq := testrepo.New(db)
	id, err := testq.CreateFunctionsAccessFixture(ctx, testrepo.CreateFunctionsAccessFixtureParams{
		ProjectID: projectID, DeploymentID: deploymentID, FunctionID: function.ID,
		EncryptionKey: conv.NewSecret([]byte("unused-runner-key")), BearerFormat: conv.ToPGText("v1"),
	})
	require.NoError(t, err)
	require.NoError(t, testq.ClearFunctionsAccessOrganizationFixture(ctx, id))

	return id
}
