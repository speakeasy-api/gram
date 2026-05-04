package usersessions_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestServiceSmoke exercises the test harness so the unused-symbol linter is
// happy until tickets #4-#7 add per-method tests. It also confirms the
// Service struct and its dependencies wire together.
func TestServiceSmoke(t *testing.T) {
	t.Parallel()

	_, ti := newTestService(t)
	require.NotNil(t, ti.service)
	require.NotNil(t, ti.conn)
	require.NotNil(t, ti.sessionManager)
}
