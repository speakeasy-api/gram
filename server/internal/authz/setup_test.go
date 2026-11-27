package authz

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	pgContainer, cloneFunc, err := testenv.NewTestPostgres(ctx)
	if err != nil {
		log.Fatalf("launch test postgres: %v", err)
	}
	cloneTestDatabase = cloneFunc

	chContainer, chFactory, err := testenv.NewTestClickhouse(ctx)
	if err != nil {
		log.Fatalf("launch test clickhouse: %v", err)
	}
	newClickhouseClient = chFactory

	code := m.Run()

	if err := chContainer.Terminate(ctx); err != nil {
		log.Fatalf("terminate clickhouse container: %v", err)
	}
	if err := pgContainer.Terminate(ctx); err != nil {
		log.Fatalf("terminate postgres container: %v", err)
	}

	os.Exit(code)
}

// testProjectID is the project MCP checks name in tests whose grants do not
// bound the project. Every MCP check must carry one.
const testProjectID = "project-test"

var (
	cloneTestDatabase   testenv.PostgresDBCloneFunc
	newClickhouseClient testenv.ClickhouseClientFunc
)

func enterpriseTestCtx(ctx context.Context) context.Context {
	sessionID := "session_test"
	return contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID:  "org_test",
		UserID:                "user_test",
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             &sessionID,
		ProjectID:             nil,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "enterprise",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
	})
}

func newTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	conn, err := cloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	return conn
}

func seedOrganization(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) {
	t.Helper()

	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:       organizationID,
		Name:     "Test Org",
		Slug:     organizationID,
		WorkosID: conv.PtrToPGText(conv.PtrEmpty("workos-org-" + organizationID)),
	})
	require.NoError(t, err)
}

func seedGrant(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, principal urn.Principal, scope Scope, resource string) {
	t.Helper()

	selectors, err := NewSelector(scope, resource).MarshalJSON()
	require.NoError(t, err)

	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principal,
		Scope:          string(scope),
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

func seedGrantWithSelector(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, principal urn.Principal, scope Scope, sel Selector) {
	t.Helper()

	selectors, err := sel.MarshalJSON()
	require.NoError(t, err)

	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principal,
		Scope:          string(scope),
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

func seedConnectedUser(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, userID string, email string, displayName string, workosUserID string, workosMembershipID string) {
	t.Helper()

	_, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          userID,
		Email:       email,
		DisplayName: displayName,
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       false,
	})
	require.NoError(t, err)

	err = usersrepo.New(conn).OverwriteUserWorkosID(ctx, usersrepo.OverwriteUserWorkosIDParams{
		WorkosID: conv.ToPGText(workosUserID),
		ID:       userID,
	})
	require.NoError(t, err)

	err = orgrepo.New(conn).AttachWorkOSUserToOrg(ctx, orgrepo.AttachWorkOSUserToOrgParams{
		OrganizationID:     organizationID,
		UserID:             conv.ToPGText(userID),
		WorkosMembershipID: conv.PtrToPGText(conv.PtrEmpty(workosMembershipID)),
	})
	require.NoError(t, err)
}

// Engine tests build checks the typed refs refuse to: a check with no project
// dimension, an arbitrary scope over a server, or a tool call missing parts.
// These helpers keep those checks expressible in this package's tests only;
// code outside authz builds every check from a typed ref.

// mcpToolCallDimensions carries the parts of an MCP tool call. An empty field
// is left out of the check.
type mcpToolCallDimensions struct {
	Tool        string
	Disposition string
	ProjectID   string
}

// mcpToolCallCheck is a tool call on toolsetID with the given dimensions.
func mcpToolCallCheck(toolsetID string, dims mcpToolCallDimensions) Check {
	dimensions := map[string]string{}
	if dims.Disposition != "" {
		dimensions[SelectorKeyDisposition] = dims.Disposition
	}
	if dims.Tool != "" {
		dimensions[SelectorKeyTool] = dims.Tool
	}
	if dims.ProjectID != "" {
		dimensions[SelectorKeyProjectID] = dims.ProjectID
	}
	return Check{Scope: ScopeMCPConnect, ResourceKind: "", ResourceID: toolsetID, Dimensions: dimensions, exact: false, representedUser: "", probe: false, group: nil, operation: operationToolCall}
}

// mcpCheck is scope on resourceID, with projectID as a dimension when it is
// not empty. A connect check is a connection.
func mcpCheck(scope Scope, resourceID, projectID string) Check {
	var dimensions map[string]string
	if projectID != "" {
		dimensions = map[string]string{SelectorKeyProjectID: projectID}
	}
	check := Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: dimensions, exact: false, representedUser: "", probe: false, group: nil, operation: operationDefault}
	if isConnectScope(scope) {
		return check.asConnection()
	}
	return check
}

// assistantCheck is scope on resourceID in projectID.
func assistantCheck(scope Scope, resourceID, projectID string) Check {
	return Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: map[string]string{SelectorKeyProjectID: projectID}, exact: false, representedUser: "", probe: false, group: nil, operation: operationDefault}
}
