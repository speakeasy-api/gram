package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type stubDirectExecutor struct {
	response string
	err      error
	called   bool
	gotBody  string
}

func (s *stubDirectExecutor) Call(_ context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	s.called = true
	if payload != nil {
		body, err := io.ReadAll(payload)
		if err != nil {
			return fmt.Errorf("read payload: %w", err)
		}
		s.gotBody = string(body)
	}
	if s.err != nil {
		return s.err
	}
	if _, err := io.WriteString(wr, s.response); err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	return nil
}

func TestService_ExecuteTool_RequiresProjectAuthContext(t *testing.T) {
	t.Parallel()

	svc := NewService(testenv.NewLogger(t), nil, nil, audit.NewLogger())
	projectID := uuid.New()

	_, err := svc.ExecuteTool(context.Background(), &gateway.ToolCallPlan{
		Kind: gateway.ToolKindPlatform,
		Descriptor: &gateway.ToolDescriptor{
			ProjectID: projectID.String(),
			URN:       urn.NewTool(urn.ToolKindPlatform, "logs", "search_logs"),
		},
	}, toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "project auth context")
}

func TestService_ExecuteTool_RejectsMismatchedProjectAuthContext(t *testing.T) {
	t.Parallel()

	svc := NewService(testenv.NewLogger(t), nil, nil, audit.NewLogger())
	descriptorProjectID := uuid.New()
	authProjectID := uuid.New()
	ctx := contextvalues.SetAuthContext(context.Background(), &contextvalues.AuthContext{
		ProjectID: &authProjectID,
	})

	_, err := svc.ExecuteTool(ctx, &gateway.ToolCallPlan{
		Kind: gateway.ToolKindPlatform,
		Descriptor: &gateway.ToolDescriptor{
			ProjectID: descriptorProjectID.String(),
			URN:       urn.NewTool(urn.ToolKindPlatform, "logs", "search_logs"),
		},
	}, toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "does not match project")
}

func TestService_ExecuteTool_UsesPlanExecutorOverride(t *testing.T) {
	t.Parallel()

	svc := NewService(testenv.NewLogger(t), nil, nil, audit.NewLogger())
	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(context.Background(), &contextvalues.AuthContext{
		ActiveOrganizationID:  "org-1",
		UserID:                "user-1",
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             nil,
		ProjectID:             &projectID,
		OrganizationSlug:      "org",
		Email:                 nil,
		AccountType:           "",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
		IsAdmin:               false,
	})

	exec := &stubDirectExecutor{response: `{"ok":true}`, err: nil, called: false, gotBody: ""}

	// The plan's URN deliberately points at no registered executor; the
	// override must short-circuit the registry lookup that would otherwise
	// 404.
	result, err := svc.ExecuteTool(ctx, &gateway.ToolCallPlan{
		Kind:        gateway.ToolKindPlatform,
		BillingType: "",
		Descriptor: &gateway.ToolDescriptor{
			ID:               "",
			Name:             "",
			Description:      nil,
			DeploymentID:     "",
			ProjectID:        projectID.String(),
			ProjectSlug:      "",
			OrganizationID:   "",
			OrganizationSlug: "",
			URN:              urn.NewTool(urn.ToolKindPlatform, "unregistered", "tool"),
		},
		HTTP:     nil,
		Function: nil,
		Prompt:   nil,
		Platform: &gateway.PlatformToolCallPlan{
			SourceSlug:  "unregistered",
			Managed:     false,
			OwnerKind:   "",
			OwnerID:     "",
			InputSchema: nil,
			Executor:    exec,
		},
		ExternalMCP: nil,
	}, toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}, strings.NewReader(`{"hello":"world"}`))
	require.NoError(t, err)
	require.True(t, exec.called)
	require.JSONEq(t, `{"hello":"world"}`, exec.gotBody)
	require.NotNil(t, result)
	require.JSONEq(t, `{"ok":true}`, string(result.Body))
}

func TestService_ExecuteTool_PlanExecutorOverrideSurfacesError(t *testing.T) {
	t.Parallel()

	svc := NewService(testenv.NewLogger(t), nil, nil, audit.NewLogger())
	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(context.Background(), &contextvalues.AuthContext{
		ActiveOrganizationID:  "org-1",
		UserID:                "user-1",
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             nil,
		ProjectID:             &projectID,
		OrganizationSlug:      "org",
		Email:                 nil,
		AccountType:           "",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
		IsAdmin:               false,
	})

	exec := &stubDirectExecutor{response: "", err: errors.New("boom"), called: false, gotBody: ""}

	_, err := svc.ExecuteTool(ctx, &gateway.ToolCallPlan{
		Kind:        gateway.ToolKindPlatform,
		BillingType: "",
		Descriptor: &gateway.ToolDescriptor{
			ID:               "",
			Name:             "",
			Description:      nil,
			DeploymentID:     "",
			ProjectID:        projectID.String(),
			ProjectSlug:      "",
			OrganizationID:   "",
			OrganizationSlug: "",
			URN:              urn.NewTool(urn.ToolKindPlatform, "unregistered", "tool"),
		},
		HTTP:     nil,
		Function: nil,
		Prompt:   nil,
		Platform: &gateway.PlatformToolCallPlan{
			SourceSlug:  "unregistered",
			Managed:     false,
			OwnerKind:   "",
			OwnerID:     "",
			InputSchema: nil,
			Executor:    exec,
		},
		ExternalMCP: nil,
	}, toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "boom")
	require.True(t, exec.called)
}
