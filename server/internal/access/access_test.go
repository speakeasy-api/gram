package access

import (
	"context"
	"errors"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
)

func TestRequire_allowsScopedGrant(t *testing.T) {
	t.Parallel()

	grants := &Grants{
		rows: []grantRow{{
			Scope:    ScopeBuildRead,
			Resource: "proj_123",
		}},
	}

	ctx := GrantsToContext(context.Background(), grants)

	err := Require(ctx, Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	require.NoError(t, err)
}

func TestRequire_allowsWildcardGrant(t *testing.T) {
	t.Parallel()

	grants := &Grants{
		rows: []grantRow{{
			Scope:    ScopeBuildRead,
			Resource: WildcardResource,
		}},
	}

	ctx := GrantsToContext(context.Background(), grants)

	err := Require(ctx, Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	require.NoError(t, err)
}

func TestRequire_allowsWildcardCheck(t *testing.T) {
	t.Parallel()

	grants := &Grants{
		rows: []grantRow{{
			Scope:    ScopeBuildRead,
			Resource: WildcardResource,
		}},
	}

	ctx := GrantsToContext(context.Background(), grants)

	err := Require(ctx, Check{Scope: ScopeBuildRead, ResourceID: WildcardResource})
	require.NoError(t, err)
}

func TestRequire_deniesMissingGrant(t *testing.T) {
	t.Parallel()

	grants := &Grants{
		rows: []grantRow{{
			Scope:    ScopeBuildRead,
			Resource: "proj_123",
		}},
	}

	ctx := GrantsToContext(context.Background(), grants)

	err := Require(ctx, Check{Scope: ScopeBuildRead, ResourceID: "proj_456"})
	require.Error(t, err)

	var deniedErr *AccessDeniedError
	require.True(t, errors.As(err, &deniedErr))
	require.Equal(t, ScopeBuildRead, deniedErr.Scope)
	require.Equal(t, "proj_456", deniedErr.ResourceID)

	var shareableErr *oops.ShareableError
	require.True(t, errors.As(err, &shareableErr))
	require.Equal(t, oops.CodeForbidden, shareableErr.Code)
}

func TestRequire_appliesAdditiveGrants(t *testing.T) {
	t.Parallel()

	grants := &Grants{
		rows: []grantRow{
			{
				Scope:    ScopeBuildRead,
				Resource: WildcardResource,
			},
			{
				Scope:    ScopeBuildRead,
				Resource: "proj_alpha",
			},
			{
				Scope:    ScopeMCPConnect,
				Resource: "mcp_payments",
			},
		},
	}

	ctx := GrantsToContext(context.Background(), grants)

	// Wildcard build access means project-level read is allowed for any project,
	// while the explicit MCP grant allows connecting only to the payments MCP.
	err := Require(ctx,
		Check{Scope: ScopeBuildRead, ResourceID: "proj_beta"},
		Check{Scope: ScopeMCPConnect, ResourceID: "mcp_payments"},
	)
	require.NoError(t, err)

	// The same wildcard build access still allows the project read, but MCP
	// access is denied because only mcp_payments is granted.
	err = Require(ctx,
		Check{Scope: ScopeBuildRead, ResourceID: "proj_beta"},
		Check{Scope: ScopeMCPConnect, ResourceID: "mcp_analytics"},
	)
	require.Error(t, err)

	var deniedErr *AccessDeniedError
	require.True(t, errors.As(err, &deniedErr))
	require.Equal(t, ScopeMCPConnect, deniedErr.Scope)
	require.Equal(t, "mcp_analytics", deniedErr.ResourceID)

	var shareableErr *oops.ShareableError
	require.True(t, errors.As(err, &shareableErr))
	require.Equal(t, oops.CodeForbidden, shareableErr.Code)
}

func TestRequire_requiresGrantsInContext(t *testing.T) {
	t.Parallel()

	err := Require(context.Background(), Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	require.Error(t, err)

	var shareableErr *oops.ShareableError
	require.True(t, errors.As(err, &shareableErr))
	require.Equal(t, oops.CodeUnexpected, shareableErr.Code)
}

func TestRequire_rejectsEmptyResourceID(t *testing.T) {
	t.Parallel()

	grants := &Grants{
		rows: []grantRow{{
			Scope:    ScopeBuildRead,
			Resource: WildcardResource,
		}},
	}

	ctx := GrantsToContext(context.Background(), grants)

	err := Require(ctx, Check{Scope: ScopeBuildRead, ResourceID: ""})
	require.Error(t, err)

	var invalidErr *InvalidCheckError
	require.True(t, errors.As(err, &invalidErr))
	require.Equal(t, ScopeBuildRead, invalidErr.Scope)

	var shareableErr *oops.ShareableError
	require.True(t, errors.As(err, &shareableErr))
	require.Equal(t, oops.CodeInvariantViolation, shareableErr.Code)
}
