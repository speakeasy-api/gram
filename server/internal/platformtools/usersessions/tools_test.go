package usersessions

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

func TestListToolDescriptor(t *testing.T) {
	t.Parallel()

	desc := NewListUserSessionsTool(nil).Descriptor()
	require.Equal(t, "platform_list_user_sessions", desc.Name)
	require.NotEmpty(t, desc.InputSchema)
	require.NotNil(t, desc.Annotations)

	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(desc.InputSchema, &schema))
	require.Contains(t, schema.Properties, "search")
	require.Contains(t, schema.Properties, "meta_mcp_server_id")
}

func TestListToolRejectsOverlongSearch(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org-a", ProjectID: &projectID})
	payload, err := json.Marshal(map[string]string{"search": strings.Repeat("é", maxSearchLength+1)})
	require.NoError(t, err)

	err = NewListUserSessionsTool(nil).Call(ctx, toolconfig.ToolCallEnv{}, bytes.NewReader(payload), &bytes.Buffer{})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeBadRequest, shareable.Code)
}

func TestGetToolDescriptor(t *testing.T) {
	t.Parallel()

	desc := NewGetUserSessionTool(nil).Descriptor()
	require.Equal(t, "platform_get_user_session", desc.Name)
	require.NotEmpty(t, desc.InputSchema)
	require.NotNil(t, desc.Annotations)
}
