package mcpaccess_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestServerPermissionDeniedRewritesForbiddenError(t *testing.T) {
	t.Parallel()

	cause := oops.C(oops.CodeForbidden)
	err := mcpaccess.ServerPermissionDenied(cause)

	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeForbidden, shareableErr.Code)
	require.Equal(t, mcpaccess.ServerPermissionDeniedMessage, shareableErr.Error())
	require.ErrorIs(t, err, cause)
}

func TestToolPermissionDeniedRewritesForbiddenError(t *testing.T) {
	t.Parallel()

	cause := oops.C(oops.CodeForbidden)
	err := mcpaccess.ToolPermissionDenied(cause)

	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeForbidden, shareableErr.Code)
	require.Equal(t, mcpaccess.ToolPermissionDeniedMessage, shareableErr.Error())
	require.ErrorIs(t, err, cause)
}

func TestPermissionDeniedPreservesNonForbiddenError(t *testing.T) {
	t.Parallel()

	cause := errors.New("temporary failure")

	require.ErrorIs(t, mcpaccess.ServerPermissionDenied(cause), cause)
	require.ErrorIs(t, mcpaccess.ToolPermissionDenied(cause), cause)
}
