package callcache

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestKeyScopesCallByAuthenticatedProject(t *testing.T) {
	t.Parallel()
	projectA := uuid.New()
	projectB := uuid.New()

	require.NotEqual(t, key(projectA, "shared-call"), key(projectB, "shared-call"))
	require.NotEqual(t, key(projectA, "first-call"), key(projectA, "second-call"))
	require.Contains(t, key(projectA, "shared-call"), projectA.String())
}
