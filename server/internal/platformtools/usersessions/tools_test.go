package usersessions

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListToolDescriptor(t *testing.T) {
	t.Parallel()

	desc := NewListUserSessionsTool(nil).Descriptor()
	require.Equal(t, "platform_list_user_sessions", desc.Name)
	require.NotEmpty(t, desc.InputSchema)
	require.NotNil(t, desc.Annotations)
}

func TestGetToolDescriptor(t *testing.T) {
	t.Parallel()

	desc := NewGetUserSessionTool(nil).Descriptor()
	require.Equal(t, "platform_get_user_session", desc.Name)
	require.NotEmpty(t, desc.InputSchema)
	require.NotNil(t, desc.Annotations)
}
