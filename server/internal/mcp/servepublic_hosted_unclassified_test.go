package mcp_test

import (
	"context"
	"maps"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// privateHostedFixture is the selection toolset (reader is read-only; writer
// and eraser carry no annotations) made private, with a bearer for the mock
// user so per-tool RBAC is enforced on the real serve path.
func privateHostedFixture(t *testing.T, ctx context.Context, ti *testInstance) (toolsets_repo.Toolset, string) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset, issuer := seedSelectionToolset(t, ctx, ti)
	setToolsetMcpPrivate(t, ctx, ti, toolset.ID, *authCtx.ProjectID)

	subject := urn.NewUserSubject(mockidp.MockUserID)
	access, jti, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
		Subject:  subject,
		Audience: urn.NewToolset(toolset.ID).String(),
		Issuer:   "https://test.example",
		Lifetime: time.Hour,
		ClientID: "test-client",
	})
	require.NoError(t, err)

	now := time.Now()
	_, err = usersessions_repo.New(ti.conn).CreateUserSession(ctx, usersessions_repo.CreateUserSessionParams{
		UserSessionIssuerID: issuer.ID,
		UserSessionClientID: uuid.NullUUID{},
		SubjectUrn:          subject,
		Jti:                 jti,
		RefreshTokenHash:    conv.ToPGText("test-hosted-rbac-" + uuid.NewString()),
		RefreshExpiresAt:    pgtype.Timestamptz{Time: now.Add(24 * time.Hour), Valid: true},
		ExpiresAt:           pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true},
		ToolSelection:       nil,
	})
	require.NoError(t, err)

	return toolset, access
}

func seedMockUserToolsetGrant(t *testing.T, ctx context.Context, ti *testInstance, toolsetID uuid.UUID, narrowing map[string]string) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	selector := authz.NewSelector(authz.ScopeMCPConnect, toolsetID.String())
	maps.Copy(selector, narrowing)
	selectors, err := selector.MarshalJSON()
	require.NoError(t, err)

	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, mockidp.MockUserID),
		Scope:          string(authz.ScopeMCPConnect),
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

// hostedCallOutcome serves one tools/call and returns everything it said,
// whether as a response body or a returned error.
func hostedCallOutcome(t *testing.T, ti *testInstance, slug, bearer, tool string) string {
	t.Helper()

	w, err := servePublicHTTP(t, context.Background(), ti, slug, makeToolsCallBody(tool), bearer, nil)
	combined := w.Body.String()
	if err != nil {
		combined += err.Error()
	}
	return combined
}

// On a private hosted toolset a grant narrowed only by disposition reaches the
// tools carrying that annotation, and neither lists nor calls the tools that
// carry none.
func TestServePublic_PrivateHosted_DispositionGrantExcludesUnannotatedTools(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, bearer := privateHostedFixture(t, ctx, ti)
	seedMockUserToolsetGrant(t, ctx, ti, toolset.ID, map[string]string{authz.SelectorKeyDisposition: authz.DispositionReadOnly})

	w, err := servePublicHTTP(t, context.Background(), ti, toolset.McpSlug.String, makeToolsListBody(), bearer, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []string{"reader"}, toolNames(parseToolsListResponse(t, w.Body.Bytes())))

	require.Contains(t, hostedCallOutcome(t, ti, toolset.McpSlug.String, bearer, "eraser"), "permission")
	require.NotContains(t, hostedCallOutcome(t, ti, toolset.McpSlug.String, bearer, "reader"), "permission")
}

// Naming an unannotated tool still reaches it.
func TestServePublic_PrivateHosted_NamedGrantReachesUnannotatedTool(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, bearer := privateHostedFixture(t, ctx, ti)
	seedMockUserToolsetGrant(t, ctx, ti, toolset.ID, map[string]string{authz.SelectorKeyTool: "eraser"})

	w, err := servePublicHTTP(t, context.Background(), ti, toolset.McpSlug.String, makeToolsListBody(), bearer, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []string{"eraser"}, toolNames(parseToolsListResponse(t, w.Body.Bytes())))

	require.NotContains(t, hostedCallOutcome(t, ti, toolset.McpSlug.String, bearer, "eraser"), "permission")
	require.Contains(t, hostedCallOutcome(t, ti, toolset.McpSlug.String, bearer, "writer"), "permission")
}
