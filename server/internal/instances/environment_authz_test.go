package instances

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	"github.com/speakeasy-api/gram/server/internal/oops"
	templatesrepo "github.com/speakeasy-api/gram/server/internal/templates/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type environmentAuthzFixture struct {
	svc     *Service
	authCtx *contextvalues.AuthContext
	toolURN urn.Tool
	scans   *int
}

func newEnvironmentAuthzFixture(t *testing.T) (context.Context, environmentAuthzFixture, *pgxpool.Pool) {
	t.Helper()

	ctx, svc, conn := newTestInstanceService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolURN := urn.NewTool(urn.ToolKindPrompt, "prompt", "env-authz")
	_, err := templatesrepo.New(conn).CreateTemplate(ctx, templatesrepo.CreateTemplateParams{
		ProjectID:   *authCtx.ProjectID,
		ToolUrn:     toolURN,
		Name:        "env-authz",
		Prompt:      "Summarize {{topic}}",
		Description: conv.ToPGText("Summary"),
		Arguments:   []byte(`{"type":"object","properties":{"topic":{"type":"string"}}}`),
		Engine:      conv.ToPGText("mustache"),
		Kind:        conv.ToPGText("prompt"),
		ToolsHint:   nil, ToolUrnsHint: nil,
	})
	require.NoError(t, err)

	scans := 0
	svc.scanEvaluator = mcpriskscan.NewEvaluator(scanObserverFunc(func(context.Context, mcpriskscan.Subject) {
		scans++
	}))

	return ctx, environmentAuthzFixture{svc: svc, authCtx: authCtx, toolURN: toolURN, scans: &scans}, conn
}

func (f environmentAuthzFixture) createSecretEnvironment(t *testing.T, ctx context.Context, name string) environmentsrepo.Environment {
	t.Helper()

	env, err := environmentsrepo.New(f.svc.db).CreateEnvironment(ctx, environmentsrepo.CreateEnvironmentParams{
		OrganizationID: f.authCtx.ActiveOrganizationID,
		ProjectID:      *f.authCtx.ProjectID,
		Name:           name,
		Slug:           name,
		Description:    conv.ToPGText("Holds a protected credential"),
	})
	require.NoError(t, err)

	_, err = f.svc.env.CreateEnvironmentEntries(ctx, environmentsrepo.CreateEnvironmentEntriesParams{
		EnvironmentID: env.ID,
		Names:         []string{"PROTECTED_TOKEN"},
		Values:        []string{"super-secret-value"},
		IsSecrets:     []bool{true},
	})
	require.NoError(t, err)

	return env
}

func (f environmentAuthzFixture) invokeWithSession(t *testing.T, ctx context.Context, environmentSlug string) (*httptest.ResponseRecorder, error) {
	t.Helper()

	request := f.newRequest(t, ctx, environmentSlug)
	request.Header.Set(constants.SessionHeader, *f.authCtx.SessionID)
	recorder := httptest.NewRecorder()
	return recorder, f.svc.ExecuteInstanceTool(recorder, request)
}

func (f environmentAuthzFixture) newRequest(t *testing.T, ctx context.Context, environmentSlug string) *http.Request {
	t.Helper()

	query := url.Values{}
	query.Set(toolUrnQueryParam, f.toolURN.String())
	if environmentSlug != "" {
		query.Set(environmentSlugQueryParam, environmentSlug)
	}
	request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/rpc/instances.invoke/tool?"+query.Encode(), strings.NewReader(`{"arguments":{"topic":"environments"}}`))
	request.Header.Set(constants.ProjectHeader, *f.authCtx.ProjectSlug)
	return request
}

func projectWriteGrant(projectID string) authz.Grant {
	return authz.NewGrant(authz.ScopeProjectWrite, projectID)
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

func TestExecuteInstanceTool_SelectedEnvironmentDeniedWithProjectWriteOnly(t *testing.T) {
	t.Parallel()

	baseCtx, f, _ := newEnvironmentAuthzFixture(t)
	env := f.createSecretEnvironment(t, baseCtx, "restricted-env")

	ctx := authztest.WithExactGrants(t, baseCtx, projectWriteGrant(f.authCtx.ProjectID.String()))
	_, err := f.invokeWithSession(t, ctx, env.Slug)
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Zero(t, *f.scans, "authorization must fail before the tool call is scanned or executed")
}

func TestExecuteInstanceTool_MissingEnvironmentDeniedWithProjectWriteOnly(t *testing.T) {
	t.Parallel()

	baseCtx, f, _ := newEnvironmentAuthzFixture(t)

	ctx := authztest.WithExactGrants(t, baseCtx, projectWriteGrant(f.authCtx.ProjectID.String()))
	_, err := f.invokeWithSession(t, ctx, "does-not-exist")
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Zero(t, *f.scans)
}

func TestExecuteInstanceTool_MissingEnvironmentNotFoundWithEnvironmentRead(t *testing.T) {
	t.Parallel()

	baseCtx, f, _ := newEnvironmentAuthzFixture(t)
	projectID := f.authCtx.ProjectID.String()

	ctx := authztest.WithExactGrants(t, baseCtx,
		projectWriteGrant(projectID),
		authz.NewGrantWithSelector(authz.ScopeEnvironmentRead, authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindEnvironment,
			authz.SelectorKeyResourceID:   authz.WildcardResource,
			authz.SelectorKeyProjectID:    projectID,
		}),
	)
	_, err := f.invokeWithSession(t, ctx, "does-not-exist")
	requireOopsCode(t, err, oops.CodeNotFound)
	require.Zero(t, *f.scans)
}

func TestExecuteInstanceTool_SelectedEnvironmentAllowedWithProjectWideEnvironmentRead(t *testing.T) {
	t.Parallel()

	baseCtx, f, _ := newEnvironmentAuthzFixture(t)
	env := f.createSecretEnvironment(t, baseCtx, "project-wide-env")
	projectID := f.authCtx.ProjectID.String()

	ctx := authztest.WithExactGrants(t, baseCtx,
		projectWriteGrant(projectID),
		authz.NewGrantWithSelector(authz.ScopeEnvironmentRead, authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindEnvironment,
			authz.SelectorKeyResourceID:   authz.WildcardResource,
			authz.SelectorKeyProjectID:    projectID,
		}),
	)
	recorder, err := f.invokeWithSession(t, ctx, env.Slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "Summarize environments", recorder.Body.String())
}

func TestExecuteInstanceTool_SelectedEnvironmentGrantDoesNotCoverOtherEnvironments(t *testing.T) {
	t.Parallel()

	baseCtx, f, _ := newEnvironmentAuthzFixture(t)
	granted := f.createSecretEnvironment(t, baseCtx, "granted-env")
	other := f.createSecretEnvironment(t, baseCtx, "other-env")
	projectID := f.authCtx.ProjectID.String()

	ctx := authztest.WithExactGrants(t, baseCtx,
		projectWriteGrant(projectID),
		authz.NewGrant(authz.ScopeEnvironmentRead, granted.ID.String()),
	)

	recorder, err := f.invokeWithSession(t, ctx, granted.Slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
	scansAfterGranted := *f.scans

	_, err = f.invokeWithSession(t, ctx, other.Slug)
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Equal(t, scansAfterGranted, *f.scans, "a denied environment must not reach the tool call")
}

func TestExecuteInstanceTool_SelectedEnvironmentDeniedByEnvironmentBlock(t *testing.T) {
	t.Parallel()

	baseCtx, f, _ := newEnvironmentAuthzFixture(t)
	env := f.createSecretEnvironment(t, baseCtx, "blocked-env")
	projectID := f.authCtx.ProjectID.String()

	ctx := authztest.WithExactGrants(t, baseCtx,
		projectWriteGrant(projectID),
		authz.NewGrantWithSelector(authz.ScopeEnvironmentRead, authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindEnvironment,
			authz.SelectorKeyResourceID:   authz.WildcardResource,
			authz.SelectorKeyProjectID:    projectID,
		}),
		authz.NewGrant(authz.ScopeEnvironmentBlockedRead, env.ID.String()),
	)
	_, err := f.invokeWithSession(t, ctx, env.Slug)
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Zero(t, *f.scans)
}

func TestExecuteInstanceTool_NoEnvironmentSelectedNeedsNoEnvironmentGrant(t *testing.T) {
	t.Parallel()

	baseCtx, f, _ := newEnvironmentAuthzFixture(t)

	ctx := authztest.WithExactGrants(t, baseCtx, projectWriteGrant(f.authCtx.ProjectID.String()))
	recorder, err := f.invokeWithSession(t, ctx, "")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
}

// Legacy (non-principal) API keys are not evaluated against RBAC grants: their
// authority is the project they are bound to, so they keep selecting any
// environment in that project as before.
func TestExecuteInstanceTool_LegacyAPIKeyKeepsProjectEnvironmentAccess(t *testing.T) {
	t.Parallel()

	baseCtx, f, conn := newEnvironmentAuthzFixture(t)
	env := f.createSecretEnvironment(t, baseCtx, "legacy-key-env")

	rawKey := "gram_local_" + uuid.NewString()
	keyHash, err := auth.GetAPIKeyHash(rawKey)
	require.NoError(t, err)
	_, err = keysrepo.New(conn).CreateAPIKey(baseCtx, keysrepo.CreateAPIKeyParams{
		OrganizationID:  f.authCtx.ActiveOrganizationID,
		ProjectID:       uuid.NullUUID{UUID: *f.authCtx.ProjectID, Valid: true},
		CreatedByUserID: f.authCtx.UserID,
		Name:            "instances-env-authz",
		KeyPrefix:       rawKey[:16],
		KeyHash:         keyHash,
		Scopes:          []string{"consumer"},
	})
	require.NoError(t, err)

	ctx := authztest.WithExactGrants(t, baseCtx)
	request := f.newRequest(t, ctx, env.Slug)
	request.Header.Set(constants.APIKeyHeader, rawKey)
	recorder := httptest.NewRecorder()
	require.NoError(t, f.svc.ExecuteInstanceTool(recorder, request))
	require.Equal(t, http.StatusOK, recorder.Code)
}
