package identityproviderreadiness

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type directoryHandoffQueriesDouble struct {
	stored         bool
	err            error
	organizationID string
}

func (d *directoryHandoffQueriesDouble) HasDirectoryHandoff(_ context.Context, organizationID string) (bool, error) {
	d.organizationID = organizationID
	return d.stored, d.err
}

func TestDatabaseDirectoryHandoffCheckerReturnsStoredState(t *testing.T) {
	t.Parallel()

	queries := &directoryHandoffQueriesDouble{stored: true, err: nil, organizationID: ""}
	checker := &DatabaseDirectoryHandoffChecker{queries: queries}

	stored, err := checker.HasDirectoryHandoff(t.Context(), "organization")
	require.NoError(t, err)
	require.True(t, stored)
	require.Equal(t, "organization", queries.organizationID)
}

func TestDatabaseDirectoryHandoffCheckerWrapsQueryErrors(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("database unavailable")
	checker := &DatabaseDirectoryHandoffChecker{queries: &directoryHandoffQueriesDouble{stored: false, err: queryErr, organizationID: ""}}

	stored, err := checker.HasDirectoryHandoff(t.Context(), "organization")
	require.False(t, stored)
	require.ErrorIs(t, err, queryErr)
	require.EqualError(t, err, "check directory handoff: database unavailable")
}
