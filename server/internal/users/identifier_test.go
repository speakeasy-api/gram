package users

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserIDFromWorkOSID_Deterministic(t *testing.T) {
	t.Parallel()

	first := UserIDFromWorkOSID("user_123")
	second := UserIDFromWorkOSID("user_123")

	require.Equal(t, first, second)
}

func TestUserIDFromWorkOSID_DifferentInputs(t *testing.T) {
	t.Parallel()

	require.NotEqual(t, UserIDFromWorkOSID("user_123"), UserIDFromWorkOSID("user_456"))
}

func TestUserIDFromWorkOSID_KnownValue(t *testing.T) {
	t.Parallel()

	require.Equal(t, "15774ba1-063c-5039-993a-52528301e681", UserIDFromWorkOSID("user_123"))
}
