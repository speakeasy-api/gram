package assistants

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// sessionContextWithGrants is a dashboard session in projectID holding exactly grants.
func sessionContextWithGrants(t *testing.T, projectID uuid.UUID, grants ...authz.Grant) context.Context {
	t.Helper()
	sessionID := "session-test"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-test",
		UserID:               "user-test",
		SessionID:            &sessionID,
		ProjectID:            &projectID,
		OrganizationSlug:     "org-test",
		AccountType:          "enterprise",
	})
	return authztest.WithExactGrants(t, ctx, grants...)
}

func TestAssistantEnvironmentBindingRequiresEnvironmentRead(t *testing.T) {
	t.Parallel()

	f := newHostedAttachmentFixture(t, "assistant_env_binding_authz")
	staging := "staging"
	stagingID := f.environmentID(t, staging)
	name := "Assistant"
	noRead := sessionContextWithGrants(t, f.projectID)
	update := func(ctx context.Context, toolsets []*types.AssistantToolsetRef, servers []*types.AssistantMCPServerRef) error {
		canBindEnvironment, err := f.core.readableEnvironments(ctx, f.projectID, toolsets, servers)
		if err != nil {
			return err
		}
		_, err = f.core.UpdateAssistant(ctx, f.projectID, f.assistant, &name, nil, nil, toolsets, servers, nil, nil, nil, allowAllMCPEnable, canBindEnvironment)
		return err
	}

	err := update(noRead, []*types.AssistantToolsetRef{{ToolsetSlug: "billing", EnvironmentSlug: &staging}}, nil)
	require.ErrorIs(t, err, errEnvironmentBindingForbidden)

	// Resubmitting a binding the assistant already has selects nothing new.
	f.addServerRow(t, f.hosted.ID, stagingID)
	err = update(noRead, []*types.AssistantToolsetRef{{ToolsetSlug: "billing", EnvironmentSlug: &staging}}, nil)
	require.NoError(t, err)

	// The same environment on another server is a new binding.
	err = update(noRead, nil, []*types.AssistantMCPServerRef{{McpServerSlug: "remote", EnvironmentSlug: &staging}})
	require.ErrorIs(t, err, errEnvironmentBindingForbidden)

	// An unknown slug is indistinguishable from an unreadable one.
	missing := "missing"
	err = update(noRead, nil, []*types.AssistantMCPServerRef{{McpServerSlug: "remote", EnvironmentSlug: &missing}})
	require.ErrorIs(t, err, errEnvironmentBindingForbidden)
	err = update(sessionContextWithGrants(t, f.projectID, authz.NewGrant(authz.ScopeEnvironmentRead, authz.WildcardResource)), nil,
		[]*types.AssistantMCPServerRef{{McpServerSlug: "remote", EnvironmentSlug: &missing}})
	require.ErrorIs(t, err, errAssistantValidation)

	canRead := sessionContextWithGrants(t, f.projectID, authz.NewGrant(authz.ScopeEnvironmentRead, stagingID.UUID.String()))
	err = update(canRead, nil, []*types.AssistantMCPServerRef{{McpServerSlug: "remote", EnvironmentSlug: &staging}})
	require.NoError(t, err)
}
