package assistants

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestServiceRequiresProjectGrants(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID := newRBACService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	assistantID := uuid.NewString()
	for name, call := range map[string]func(context.Context) error{
		"list": func(ctx context.Context) error {
			_, err := svc.ListAssistants(ctx, &gen.ListAssistantsPayload{
				SessionToken:     nil,
				ProjectSlugInput: nil,
			})
			return err
		},
		"get": func(ctx context.Context) error {
			_, err := svc.GetAssistant(ctx, &gen.GetAssistantPayload{
				ID:               assistantID,
				SessionToken:     nil,
				ProjectSlugInput: nil,
			})
			return err
		},
		"create": func(ctx context.Context) error {
			_, err := svc.CreateAssistant(ctx, &gen.CreateAssistantPayload{
				SessionToken:     nil,
				ProjectSlugInput: nil,
				Name:             "Assistant",
				Model:            "openai/gpt-4o-mini",
				Instructions:     "",
				Toolsets:         nil,
				WarmTTLSeconds:   nil,
				MaxConcurrency:   nil,
				Status:           nil,
			})
			return err
		},
		"update": func(ctx context.Context) error {
			_, err := svc.UpdateAssistant(ctx, &gen.UpdateAssistantPayload{
				SessionToken:     nil,
				ProjectSlugInput: nil,
				ID:               assistantID,
				Name:             nil,
				Model:            nil,
				Instructions:     nil,
				Toolsets:         nil,
				WarmTTLSeconds:   nil,
				MaxConcurrency:   nil,
				Status:           nil,
			})
			return err
		},
		"delete": func(ctx context.Context) error {
			return svc.DeleteAssistant(ctx, &gen.DeleteAssistantPayload{
				ID:               assistantID,
				SessionToken:     nil,
				ProjectSlugInput: nil,
			})
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			requireOopsCode(t, call(ctx), oops.CodeForbidden)
		})
	}

	readCtx := authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeProjectRead,
		Selector: authz.NewSelector(authz.ScopeProjectRead, projectID.String()),
	})
	_, err := svc.ListAssistants(readCtx, &gen.ListAssistantsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}

func TestServiceCreateAssistantMapsInvalidToolsetToBadRequest(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID := newRBACService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeProjectWrite,
		Selector: authz.NewSelector(authz.ScopeProjectWrite, projectID.String()),
	})

	_, err := svc.CreateAssistant(ctx, &gen.CreateAssistantPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             "Assistant",
		Model:            "openai/gpt-4o-mini",
		Instructions:     "",
		Toolsets: []*types.AssistantToolsetRef{
			{ToolsetSlug: "missing-toolset", EnvironmentSlug: nil},
		},
		WarmTTLSeconds: nil,
		MaxConcurrency: nil,
		Status:         nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func newRBACService(t *testing.T) (*Service, context.Context, uuid.UUID) {
	t.Helper()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_rbac")
	require.NoError(t, err)

	projectID := uuid.New()
	projectSlug := "project-" + projectID.String()[:8]
	_, err = conn.Exec(t.Context(), `
INSERT INTO projects (id, name, slug, organization_id)
VALUES ($1, 'Project', $2, 'org-test')
`, projectID, projectSlug)
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	authzEngine := authz.NewEngine(logger, conn, authztest.RBACAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)
	service := &Service{
		tracer:   testenv.NewTracerProvider(t).Tracer("test"),
		logger:   logger,
		auth:     nil,
		authz:    authzEngine,
		core:     NewServiceCore(logger, testenv.NewTracerProvider(t), conn, testRuntimeBackend{backend: runtimeBackendLocal, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(logger)),
		signaler: nil,
	}

	sessionID := "session-test"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID:  "org-test",
		UserID:                "user-test",
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             &sessionID,
		ProjectID:             &projectID,
		OrganizationSlug:      "org-test",
		Email:                 nil,
		AccountType:           "enterprise",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           &projectSlug,
		APIKeyScopes:          nil,
		IsAdmin:               false,
	})

	return service, ctx, projectID
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}
