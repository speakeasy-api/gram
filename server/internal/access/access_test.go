package access

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func enterpriseCtx() context.Context {
	return contextvalues.SetAuthContext(context.Background(), &contextvalues.AuthContext{
		AccountType: "enterprise",
	})
}

func nonEnterpriseCtx() context.Context {
	return contextvalues.SetAuthContext(context.Background(), &contextvalues.AuthContext{
		AccountType: "pro",
	})
}

func enterpriseAPIKeyCtx() context.Context {
	return contextvalues.SetAuthContext(context.Background(), &contextvalues.AuthContext{
		AccountType: "enterprise",
		APIKeyID:    "key_123",
	})
}

func TestRequire_allowsScopedGrant(t *testing.T) {
	t.Parallel()

	grants := &Grants{
		rows: []Grant{{
			Scope:    ScopeBuildRead,
			Resource: "proj_123",
		}},
	}

	ctx := GrantsToContext(enterpriseCtx(), grants)

	err := Require(ctx, Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	require.NoError(t, err)
}

func TestRequire_allowsWildcardGrant(t *testing.T) {
	t.Parallel()

	grants := &Grants{
		rows: []Grant{{
			Scope:    ScopeBuildRead,
			Resource: WildcardResource,
		}},
	}

	ctx := GrantsToContext(enterpriseCtx(), grants)

	err := Require(ctx, Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	require.NoError(t, err)
}

func TestRequire_deniesMissingGrant(t *testing.T) {
	t.Parallel()

	grants := &Grants{
		rows: []Grant{{
			Scope:    ScopeBuildRead,
			Resource: "proj_123",
		}},
	}

	ctx := GrantsToContext(enterpriseCtx(), grants)

	err := Require(ctx, Check{Scope: ScopeBuildRead, ResourceID: "proj_456"})
	require.Error(t, err)

	var deniedErr *DeniedError
	require.ErrorAs(t, err, &deniedErr)
	require.ErrorIs(t, err, ErrDenied)
	require.Equal(t, ScopeBuildRead, deniedErr.Scope)
	require.Equal(t, "proj_456", deniedErr.ResourceID)
}

func TestRequire_appliesAdditiveGrants(t *testing.T) {
	t.Parallel()

	grants := &Grants{
		rows: []Grant{
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

	ctx := GrantsToContext(enterpriseCtx(), grants)

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

	var deniedErr *DeniedError
	require.ErrorAs(t, err, &deniedErr)
	require.ErrorIs(t, err, ErrDenied)
	require.Equal(t, ScopeMCPConnect, deniedErr.Scope)
	require.Equal(t, "mcp_analytics", deniedErr.ResourceID)
}

func TestRequire_requiresGrantsInContext(t *testing.T) {
	t.Parallel()

	err := Require(enterpriseCtx(), Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	require.ErrorIs(t, err, ErrMissingGrants)
}

func TestRequire_rejectsEmptyResourceID(t *testing.T) {
	t.Parallel()

	grants := &Grants{
		rows: []Grant{{
			Scope:    ScopeBuildRead,
			Resource: WildcardResource,
		}},
	}

	ctx := GrantsToContext(enterpriseCtx(), grants)

	err := Require(ctx, Check{Scope: ScopeBuildRead, ResourceID: ""})
	require.Error(t, err)

	var invalidErr *InvalidCheckError
	require.ErrorAs(t, err, &invalidErr)
	require.ErrorIs(t, err, ErrInvalidCheck)
	require.Equal(t, ScopeBuildRead, invalidErr.Scope)
}

func TestRequire_rejectsWildcardResourceID(t *testing.T) {
	t.Parallel()

	grants := &Grants{
		rows: []Grant{{
			Scope:    ScopeBuildRead,
			Resource: WildcardResource,
		}},
	}

	ctx := GrantsToContext(enterpriseCtx(), grants)

	err := Require(ctx, Check{Scope: ScopeBuildRead, ResourceID: WildcardResource})
	require.Error(t, err)

	var invalidErr *InvalidCheckError
	require.ErrorAs(t, err, &invalidErr)
	require.ErrorIs(t, err, ErrInvalidCheck)
	require.Equal(t, ScopeBuildRead, invalidErr.Scope)
	require.Equal(t, WildcardResource, invalidErr.ResourceID)
}

func TestRequire_requiresAtLeastOneCheck(t *testing.T) {
	t.Parallel()

	err := Require(enterpriseCtx())
	require.ErrorIs(t, err, ErrNoChecks)
}

func TestRequire_skipsForNonEnterpriseAccount(t *testing.T) {
	t.Parallel()

	err := Require(nonEnterpriseCtx(), Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	require.NoError(t, err)
}

func TestRequireAny_allowsWhenAnyCheckMatches(t *testing.T) {
	t.Parallel()

	grants := &Grants{
		rows: []Grant{{
			Scope:    ScopeMCPConnect,
			Resource: "tool:b",
		}},
	}

	ctx := GrantsToContext(enterpriseCtx(), grants)

	err := RequireAny(ctx,
		Check{Scope: ScopeMCPConnect, ResourceID: "mcp:a"},
		Check{Scope: ScopeMCPConnect, ResourceID: "tool:b"},
	)
	require.NoError(t, err)
}

func TestRequireAny_deniesWhenNoCheckMatches(t *testing.T) {
	t.Parallel()

	grants := &Grants{
		rows: []Grant{{
			Scope:    ScopeMCPConnect,
			Resource: "tool:c",
		}},
	}

	ctx := GrantsToContext(enterpriseCtx(), grants)

	err := RequireAny(ctx,
		Check{Scope: ScopeMCPConnect, ResourceID: "mcp:a"},
		Check{Scope: ScopeMCPConnect, ResourceID: "tool:b"},
	)
	require.Error(t, err)

	var deniedErr *DeniedError
	require.ErrorAs(t, err, &deniedErr)
	require.ErrorIs(t, err, ErrDenied)
	require.Equal(t, ScopeMCPConnect, deniedErr.Scope)
	require.Equal(t, "mcp:a", deniedErr.ResourceID)
}

func TestRequireAny_requiresGrantsInContext(t *testing.T) {
	t.Parallel()

	err := RequireAny(enterpriseCtx(), Check{Scope: ScopeMCPConnect, ResourceID: "tool:b"})
	require.ErrorIs(t, err, ErrMissingGrants)
}

func TestRequireAny_rejectsEmptyResourceID(t *testing.T) {
	t.Parallel()

	grants := &Grants{
		rows: []Grant{{
			Scope:    ScopeMCPConnect,
			Resource: "tool:b",
		}},
	}

	ctx := GrantsToContext(enterpriseCtx(), grants)

	err := RequireAny(ctx,
		Check{Scope: ScopeMCPConnect, ResourceID: "mcp:a"},
		Check{Scope: ScopeMCPConnect, ResourceID: ""},
	)
	require.Error(t, err)

	var invalidErr *InvalidCheckError
	require.ErrorAs(t, err, &invalidErr)
	require.ErrorIs(t, err, ErrInvalidCheck)
	require.Equal(t, ScopeMCPConnect, invalidErr.Scope)
}

func TestRequireAny_rejectsWildcardResourceID(t *testing.T) {
	t.Parallel()

	grants := &Grants{
		rows: []Grant{{
			Scope:    ScopeMCPConnect,
			Resource: "tool:b",
		}},
	}

	ctx := GrantsToContext(enterpriseCtx(), grants)

	err := RequireAny(ctx,
		Check{Scope: ScopeMCPConnect, ResourceID: "mcp:a"},
		Check{Scope: ScopeMCPConnect, ResourceID: WildcardResource},
	)
	require.Error(t, err)

	var invalidErr *InvalidCheckError
	require.ErrorAs(t, err, &invalidErr)
	require.ErrorIs(t, err, ErrInvalidCheck)
	require.Equal(t, ScopeMCPConnect, invalidErr.Scope)
	require.Equal(t, WildcardResource, invalidErr.ResourceID)
}

func TestRequireAny_requiresAtLeastOneCheck(t *testing.T) {
	t.Parallel()

	err := RequireAny(enterpriseCtx())
	require.ErrorIs(t, err, ErrNoChecks)
}

func TestRequireAny_skipsForNonEnterpriseAccount(t *testing.T) {
	t.Parallel()

	err := RequireAny(nonEnterpriseCtx(), Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	require.NoError(t, err)
}

func TestFilter_returnsAllToolsForWildcardMCPGrant(t *testing.T) {
	t.Parallel()

	grants := &Grants{
		rows: []Grant{{
			Scope:    ScopeMCPConnect,
			Resource: WildcardResource,
		}},
	}

	ctx := GrantsToContext(enterpriseCtx(), grants)

	resourceIDs, err := Filter(ctx, ScopeMCPConnect, []string{"toolA", "toolB", "toolC", "toolD"})
	require.NoError(t, err)
	require.Equal(t, []string{"toolA", "toolB", "toolC", "toolD"}, resourceIDs)
}

func TestFilter_returnsGrantedToolSubsetForMCPList(t *testing.T) {
	t.Parallel()

	grants := &Grants{
		rows: []Grant{
			{
				Scope:    ScopeMCPConnect,
				Resource: "toolA",
			},
			{
				Scope:    ScopeMCPConnect,
				Resource: "toolB",
			},
		},
	}

	ctx := GrantsToContext(enterpriseCtx(), grants)

	resourceIDs, err := Filter(ctx, ScopeMCPConnect, []string{"toolA", "toolB", "toolC", "toolD"})
	require.NoError(t, err)
	require.Equal(t, []string{"toolA", "toolB"}, resourceIDs)
}

func TestFilter_returnsAllProjectsForWildcardBuildGrant(t *testing.T) {
	t.Parallel()

	grants := &Grants{
		rows: []Grant{{
			Scope:    ScopeBuildRead,
			Resource: WildcardResource,
		}},
	}

	ctx := GrantsToContext(enterpriseCtx(), grants)

	resourceIDs, err := Filter(ctx, ScopeBuildRead, []string{"proj:123", "proj:456"})
	require.NoError(t, err)
	require.Equal(t, []string{"proj:123", "proj:456"}, resourceIDs)
}

func TestFilter_returnsOnlyGrantedProjectForProjectList(t *testing.T) {
	t.Parallel()

	grants := &Grants{
		rows: []Grant{{
			Scope:    ScopeBuildRead,
			Resource: "proj:123",
		}},
	}

	ctx := GrantsToContext(enterpriseCtx(), grants)

	resourceIDs, err := Filter(ctx, ScopeBuildRead, []string{"proj:123", "proj:456"})
	require.NoError(t, err)
	require.Equal(t, []string{"proj:123"}, resourceIDs)
}

func TestFilter_requiresGrantsInContext(t *testing.T) {
	t.Parallel()

	resourceIDs, err := Filter(enterpriseCtx(), ScopeBuildRead, []string{"proj_alpha"})
	require.Error(t, err)
	require.Nil(t, resourceIDs)
	require.ErrorIs(t, err, ErrMissingGrants)
}

func TestFilter_rejectsEmptyResourceID(t *testing.T) {
	t.Parallel()

	grants := &Grants{
		rows: []Grant{{
			Scope:    ScopeBuildRead,
			Resource: WildcardResource,
		}},
	}

	ctx := GrantsToContext(enterpriseCtx(), grants)

	resourceIDs, err := Filter(ctx, ScopeBuildRead, []string{"proj_alpha", ""})
	require.Error(t, err)
	require.Nil(t, resourceIDs)

	var invalidErr *InvalidCheckError
	require.ErrorAs(t, err, &invalidErr)
	require.ErrorIs(t, err, ErrInvalidCheck)
	require.Equal(t, ScopeBuildRead, invalidErr.Scope)
}

func TestFilter_rejectsWildcardResourceID(t *testing.T) {
	t.Parallel()

	grants := &Grants{
		rows: []Grant{{
			Scope:    ScopeBuildRead,
			Resource: WildcardResource,
		}},
	}

	ctx := GrantsToContext(enterpriseCtx(), grants)

	resourceIDs, err := Filter(ctx, ScopeBuildRead, []string{"proj_alpha", WildcardResource})
	require.Error(t, err)
	require.Nil(t, resourceIDs)

	var invalidErr *InvalidCheckError
	require.ErrorAs(t, err, &invalidErr)
	require.ErrorIs(t, err, ErrInvalidCheck)
	require.Equal(t, ScopeBuildRead, invalidErr.Scope)
	require.Equal(t, WildcardResource, invalidErr.ResourceID)
}

func TestFilter_skipsForNonEnterpriseAccount(t *testing.T) {
	t.Parallel()

	resourceIDs, err := Filter(nonEnterpriseCtx(), ScopeBuildRead, []string{"proj_123", "proj_456"})
	require.NoError(t, err)
	require.Equal(t, []string{"proj_123", "proj_456"}, resourceIDs)
}

func TestRequire_skipsForAPIKeyAuth(t *testing.T) {
	t.Parallel()

	err := Require(enterpriseAPIKeyCtx(), Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	require.NoError(t, err)
}

func TestRequireAny_skipsForAPIKeyAuth(t *testing.T) {
	t.Parallel()

	err := RequireAny(enterpriseAPIKeyCtx(), Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	require.NoError(t, err)
}

func TestFilter_skipsForAPIKeyAuth(t *testing.T) {
	t.Parallel()

	resourceIDs, err := Filter(enterpriseAPIKeyCtx(), ScopeBuildRead, []string{"proj_123", "proj_456"})
	require.NoError(t, err)
	require.Equal(t, []string{"proj_123", "proj_456"}, resourceIDs)
}
