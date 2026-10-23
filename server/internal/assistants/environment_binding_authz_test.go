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

	_, err := f.core.UpdateAssistant(noRead, f.projectID, f.assistant, &name, nil, nil,
		[]*types.AssistantToolsetRef{{ToolsetSlug: "billing", EnvironmentSlug: &staging}}, nil, nil, nil, nil, allowAllMCPEnable)
	require.ErrorIs(t, err, errEnvironmentBindingForbidden)

	// Resubmitting a binding the assistant already has selects nothing new.
	f.addServerRow(t, f.hosted.ID, stagingID)
	_, err = f.core.UpdateAssistant(noRead, f.projectID, f.assistant, &name, nil, nil,
		[]*types.AssistantToolsetRef{{ToolsetSlug: "billing", EnvironmentSlug: &staging}}, nil, nil, nil, nil, allowAllMCPEnable)
	require.NoError(t, err)

	// The same environment on another server is a new binding.
	_, err = f.core.UpdateAssistant(noRead, f.projectID, f.assistant, &name, nil, nil, nil,
		[]*types.AssistantMCPServerRef{{McpServerSlug: "remote", EnvironmentSlug: &staging}}, nil, nil, nil, allowAllMCPEnable)
	require.ErrorIs(t, err, errEnvironmentBindingForbidden)

	canRead := sessionContextWithGrants(t, f.projectID, authz.NewGrant(authz.ScopeEnvironmentRead, stagingID.UUID.String()))
	_, err = f.core.UpdateAssistant(canRead, f.projectID, f.assistant, &name, nil, nil, nil,
		[]*types.AssistantMCPServerRef{{McpServerSlug: "remote", EnvironmentSlug: &staging}}, nil, nil, nil, allowAllMCPEnable)
	require.NoError(t, err)
}
