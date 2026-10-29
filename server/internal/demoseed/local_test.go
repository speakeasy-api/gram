//go:build demoseed_safety

package demoseed

import (
	"bytes"
	"context"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidentity"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/demoseed/demoseedtest"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/users"
)

const testDeveloperEmail = "dev@example.com"

// seedLocalPostgres applies the tenant seed the way demoseed.Run does, minus
// ClickHouse: the local fixtures are a Postgres-only concern, and the shared
// ClickHouse container would make these tests trample the row counts
// TestDemoSeedSafety holds fixed.
func seedLocalPostgres(ctx context.Context, t *testing.T, db *pgxpool.Pool, spec Spec) {
	t.Helper()

	logger := testenv.NewLogger(t)
	_, err := seedPostgres(ctx, logger, db, assetstest.NewTestBlobStore(t), spec)
	require.NoError(t, err)
	require.NoError(t, migrateUpstreamAuth(ctx, logger, db, spec.OrgID))
}

// TestLocalFixturesAfterLogin covers the database a developer actually has
// when they run the seed for the first time: one where they have already
// logged in. The auth callback creates the org (un-whitelisted) and a user row
// whose id it derives from the dev-idp subject — neither of which the seed can
// predict — so a seed that keys on its own derived ids collides on
// users_email_key and leaves the org behind the BookDemo gate.
func TestLocalFixturesAfterLogin(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	spec := LocalSpec()

	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	blob := assetstest.NewTestBlobStore(t)

	subject := devidentity.WorkOSUserID(devidentity.DeterministicUserID(testDeveloperEmail))
	loginUserID := users.UserIDFromWorkOSID(subject)
	require.NotEqual(t, loginUserID, devidentity.DeterministicUserID(testDeveloperEmail).String(),
		"the two id derivations coincide; this test would be vacuous")

	require.NoError(t, demoseedtest.PlantLoginArtifacts(ctx, db, demoseedtest.LoginArtifacts{
		OrgID:        spec.OrgID,
		OrgWorkOSID:  spec.WorkOSOrgID,
		OrgName:      spec.OrgName,
		OrgSlug:      spec.OrgSlug,
		UserID:       loginUserID,
		Email:        testDeveloperEmail,
		WorkOSID:     subject,
		MembershipID: "devidp_mem_from_login",
	}))

	seedLocalPostgres(ctx, t, db, spec)
	require.NoError(t, RunLocalFixtures(ctx, logger, db, blob, nil, LocalFixturesOptions{
		DeveloperEmail: testDeveloperEmail,
		Environment:    "local",
	}))

	state, err := demoseedtest.ReadDeveloperState(ctx, db, spec.OrgID, testDeveloperEmail)
	require.NoError(t, err)
	require.Equal(t, demoseedtest.DeveloperState{
		Users:           1,
		UserID:          loginUserID,
		WorkOSID:        subject,
		Whitelisted:     true,
		OrgWorkOSID:     spec.WorkOSOrgID,
		Memberships:     1,
		RoleAssignments: 1,
	}, state)
}

// TestLocalFixturesReseed is the ordinary case — seed first, then seed again —
// which must stay idempotent now that the developer row is keyed on email.
func TestLocalFixturesReseed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	spec := LocalSpec()

	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	blob := assetstest.NewTestBlobStore(t)

	siteURL, err := url.Parse("https://public.example.test")
	require.NoError(t, err)
	opts := LocalFixturesOptions{DeveloperEmail: testDeveloperEmail, Environment: "local", SiteURL: siteURL}
	validator, err := mcpregistry.LoadValidator()
	require.NoError(t, err)
	registry := mcpregistry.New(db, validator).WithAssetBaseURL(siteURL)
	var firstID uuid.UUID
	var firstIcons mcpregistry.CatalogIcons
	codeGatewayMode := func() string {
		servers, err := metamcprepo.New(db).ListMetaMCPServers(ctx, metamcprepo.ListMetaMCPServersParams{OrganizationID: spec.OrgID, ProjectID: uuid.MustParse(spec.ProjectID())})
		require.NoError(t, err)
		for _, server := range servers {
			if server.MetaMcpServer.Name == "Python Code Gateway" {
				return server.MetaMcpServer.DiscoveryMode
			}
		}
		require.FailNow(t, "Python Code Gateway not seeded")
		return ""
	}
	for range 2 {
		seedLocalPostgres(ctx, t, db, spec)
		require.Equal(t, "progressive", codeGatewayMode(), "the shared seed has no code runtime to serve Code Mode")
		require.NoError(t, RunLocalFixtures(ctx, logger, db, blob, nil, opts))
		require.Equal(t, "code_mode", codeGatewayMode())
		entry, err := registry.GetByName(ctx, localCatalogName)
		require.NoError(t, err)
		icons, err := mcpregistry.ParseCatalogIcons(entry.Data)
		require.NoError(t, err)
		require.NotNil(t, icons.Default)
		require.Equal(t, icons.Default, icons.Dark)
		require.Contains(t, icons.Default.Src, siteURL.String()+"/rpc/assets.serveImage?id=")
		if firstID == uuid.Nil {
			firstID, firstIcons = entry.ID, icons
		} else {
			require.Equal(t, firstID, entry.ID)
			require.Equal(t, firstIcons, icons)
		}
		tp := otel.GetTracerProvider()
		images := assets.NewPlatformService(logger, tp, guardian.NewDefaultPolicy(tp), db, blob)
		duplicate, err := images.StorePlatformImage(ctx, logger, "local-fixture@example.test", bytes.NewReader(localCatalogLogo), int64(len(localCatalogLogo)), "image/svg+xml")
		require.NoError(t, err)
		require.Equal(t, assets.ServeImageURL(siteURL, uuid.MustParse(duplicate.Asset.ID)), icons.Default.Src)
		page, err := registry.List(ctx, mcpregistry.ListOptions{Query: localCatalogName})
		require.NoError(t, err)
		require.Len(t, page.Entries, 1)

	}

	seedUserID := devidentity.DeterministicUserID(testDeveloperEmail)
	state, err := demoseedtest.ReadDeveloperState(ctx, db, spec.OrgID, testDeveloperEmail)
	require.NoError(t, err)
	require.Equal(t, demoseedtest.DeveloperState{
		Users:           1,
		UserID:          seedUserID.String(),
		WorkOSID:        devidentity.WorkOSUserID(seedUserID),
		Whitelisted:     true,
		OrgWorkOSID:     spec.WorkOSOrgID,
		Memberships:     1,
		RoleAssignments: 1,
	}, state)
}
