package managedrows_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/managedrows"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestRequireUnmanaged(t *testing.T) {
	t.Parallel()

	require.NoError(t, managedrows.RequireUnmanaged(uuid.NullUUID{UUID: uuid.Nil, Valid: false}, "key set"))

	err := managedrows.RequireUnmanaged(uuid.NullUUID{UUID: uuid.New(), Valid: true}, "key set")
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
	require.ErrorContains(t, err, "key set is managed by an identity provider connection")
}
