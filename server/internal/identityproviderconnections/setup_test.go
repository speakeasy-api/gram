package identityproviderconnections_test

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/provisiontest"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

const testServerURL = "https://app.getgram.ai"

type testInstance struct {
	conn  *pgxpool.Pool
	orgID string
}

func newTestDB(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	return ctx, &testInstance{conn: conn, orgID: createOrganization(t, ctx, conn)}
}

func createOrganization(t *testing.T, ctx context.Context, conn *pgxpool.Pool) string {
	t.Helper()

	orgID := "org_" + uuid.NewString()
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "IdP Connections Test Org",
		Slug:        "idp-test-" + uuid.NewString(),
		WorkosID:    conv.ToPGText(orgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	return orgID
}

func createIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, projectID uuid.NullUUID, tokenEndpoint string) uuid.UUID {
	t.Helper()

	return provisiontest.CreateIssuer(t, ctx, conn, orgID, projectID, tokenEndpoint)
}

// hookedKMSClients wraps the fixture factory to count key creations, run a
// callback after each one, and record disabled versions.
type hookedKMSClients struct {
	inner *provisiontest.KMSClients

	mu       sync.Mutex
	created  []string
	disabled []string

	// afterCreate runs once the key exists and before provisioning continues.
	afterCreate func(created *gcpkms.CreatedSigningKey)
}

func (c *hookedKMSClients) Factory(ctx context.Context, tokenSource oauth2.TokenSource) (gcpkms.ProvisioningClient, error) {
	client, err := c.inner.Factory(ctx, tokenSource)
	if err != nil {
		return nil, fmt.Errorf("build fixture kms client: %w", err)
	}

	return &hookedProvisioningClient{ProvisioningClient: client, hooks: c}, nil
}

func (c *hookedKMSClients) Created() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.created...)
}

func (c *hookedKMSClients) Disabled() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.disabled...)
}

type hookedProvisioningClient struct {
	gcpkms.ProvisioningClient

	hooks *hookedKMSClients
}

func (c *hookedProvisioningClient) CreateSigningKey(ctx context.Context, params gcpkms.CreateSigningKeyParams) (*gcpkms.CreatedSigningKey, error) {
	created, err := c.ProvisioningClient.CreateSigningKey(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("create hooked signing key: %w", err)
	}

	c.hooks.mu.Lock()
	c.hooks.created = append(c.hooks.created, created.KeyVersionName)
	hook := c.hooks.afterCreate
	c.hooks.mu.Unlock()

	if hook != nil {
		hook(created)
	}

	return created, nil
}

func (c *hookedProvisioningClient) DisableKeyVersion(ctx context.Context, versionName string) error {
	c.hooks.mu.Lock()
	c.hooks.disabled = append(c.hooks.disabled, versionName)
	c.hooks.mu.Unlock()

	if err := c.ProvisioningClient.DisableKeyVersion(ctx, versionName); err != nil {
		return fmt.Errorf("disable hooked key version: %w", err)
	}

	return nil
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()

	parsed, err := url.Parse(raw)
	require.NoError(t, err)

	return parsed
}

// auditActions lists the recorded (actor_id, action) pairs for an organization, oldest first.
func auditActions(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string) [][2]string {
	t.Helper()

	rows, err := auditrepo.New(conn).ListAuditLogs(ctx, auditrepo.ListAuditLogsParams{
		OrganizationID:         orgID,
		IncludeAssistantEvents: true,
	})
	require.NoError(t, err)

	// The listing is newest first.
	out := make([][2]string, 0, len(rows))
	for _, row := range slices.Backward(rows) {
		out = append(out, [2]string{row.ActorType + ":" + row.ActorID, row.Action})
	}

	return out
}
