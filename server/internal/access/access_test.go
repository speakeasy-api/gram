package access

import (
	"context"
	"testing"

	trequire "github.com/stretchr/testify/require"

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

	err := require(ctx, Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	trequire.NoError(t, err)
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

	err := require(ctx, Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	trequire.NoError(t, err)
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

	err := require(ctx, Check{Scope: ScopeBuildRead, ResourceID: "proj_456"})
	trequire.Error(t, err)

	var deniedErr *DeniedError
	trequire.ErrorAs(t, err, &deniedErr)
	trequire.ErrorIs(t, err, ErrDenied)
	trequire.Equal(t, ScopeBuildRead, deniedErr.Scope)
	trequire.Equal(t, "proj_456", deniedErr.ResourceID)
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
	err := require(ctx,
		Check{Scope: ScopeBuildRead, ResourceID: "proj_beta"},
		Check{Scope: ScopeMCPConnect, ResourceID: "mcp_payments"},
	)
	trequire.NoError(t, err)

	// The same wildcard build access still allows the project read, but MCP
	// access is denied because only mcp_payments is granted.
	err = require(ctx,
		Check{Scope: ScopeBuildRead, ResourceID: "proj_beta"},
		Check{Scope: ScopeMCPConnect, ResourceID: "mcp_analytics"},
	)
	trequire.Error(t, err)

	var deniedErr *DeniedError
	trequire.ErrorAs(t, err, &deniedErr)
	trequire.ErrorIs(t, err, ErrDenied)
	trequire.Equal(t, ScopeMCPConnect, deniedErr.Scope)
	trequire.Equal(t, "mcp_analytics", deniedErr.ResourceID)
}

func TestRequire_requiresGrantsInContext(t *testing.T) {
	t.Parallel()

	err := require(enterpriseCtx(), Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	trequire.ErrorIs(t, err, ErrMissingGrants)
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

	err := require(ctx, Check{Scope: ScopeBuildRead, ResourceID: ""})
	trequire.Error(t, err)

	var invalidErr *InvalidCheckError
	trequire.ErrorAs(t, err, &invalidErr)
	trequire.ErrorIs(t, err, ErrInvalidCheck)
	trequire.Equal(t, ScopeBuildRead, invalidErr.Scope)
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

	err := require(ctx, Check{Scope: ScopeBuildRead, ResourceID: WildcardResource})
	trequire.Error(t, err)

	var invalidErr *InvalidCheckError
	trequire.ErrorAs(t, err, &invalidErr)
	trequire.ErrorIs(t, err, ErrInvalidCheck)
	trequire.Equal(t, ScopeBuildRead, invalidErr.Scope)
	trequire.Equal(t, WildcardResource, invalidErr.ResourceID)
}

func TestRequire_requiresAtLeastOneCheck(t *testing.T) {
	t.Parallel()

	err := require(enterpriseCtx())
	trequire.ErrorIs(t, err, ErrNoChecks)
}

func TestRequire_skipsForNonEnterpriseAccount(t *testing.T) {
	t.Parallel()

	err := require(nonEnterpriseCtx(), Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	trequire.NoError(t, err)
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

	err := requireAny(ctx,
		Check{Scope: ScopeMCPConnect, ResourceID: "mcp:a"},
		Check{Scope: ScopeMCPConnect, ResourceID: "tool:b"},
	)
	trequire.NoError(t, err)
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

	err := requireAny(ctx,
		Check{Scope: ScopeMCPConnect, ResourceID: "mcp:a"},
		Check{Scope: ScopeMCPConnect, ResourceID: "tool:b"},
	)
	trequire.Error(t, err)

	var deniedErr *DeniedError
	trequire.ErrorAs(t, err, &deniedErr)
	trequire.ErrorIs(t, err, ErrDenied)
	trequire.Equal(t, ScopeMCPConnect, deniedErr.Scope)
	trequire.Equal(t, "mcp:a", deniedErr.ResourceID)
}

func TestRequireAny_requiresGrantsInContext(t *testing.T) {
	t.Parallel()

	err := requireAny(enterpriseCtx(), Check{Scope: ScopeMCPConnect, ResourceID: "tool:b"})
	trequire.ErrorIs(t, err, ErrMissingGrants)
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

	err := requireAny(ctx,
		Check{Scope: ScopeMCPConnect, ResourceID: "mcp:a"},
		Check{Scope: ScopeMCPConnect, ResourceID: ""},
	)
	trequire.Error(t, err)

	var invalidErr *InvalidCheckError
	trequire.ErrorAs(t, err, &invalidErr)
	trequire.ErrorIs(t, err, ErrInvalidCheck)
	trequire.Equal(t, ScopeMCPConnect, invalidErr.Scope)
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

	err := requireAny(ctx,
		Check{Scope: ScopeMCPConnect, ResourceID: "mcp:a"},
		Check{Scope: ScopeMCPConnect, ResourceID: WildcardResource},
	)
	trequire.Error(t, err)

	var invalidErr *InvalidCheckError
	trequire.ErrorAs(t, err, &invalidErr)
	trequire.ErrorIs(t, err, ErrInvalidCheck)
	trequire.Equal(t, ScopeMCPConnect, invalidErr.Scope)
	trequire.Equal(t, WildcardResource, invalidErr.ResourceID)
}

func TestRequireAny_requiresAtLeastOneCheck(t *testing.T) {
	t.Parallel()

	err := requireAny(enterpriseCtx())
	trequire.ErrorIs(t, err, ErrNoChecks)
}

func TestRequireAny_skipsForNonEnterpriseAccount(t *testing.T) {
	t.Parallel()

	err := requireAny(nonEnterpriseCtx(), Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	trequire.NoError(t, err)
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

	resourceIDs, err := filter(ctx, ScopeMCPConnect, []string{"toolA", "toolB", "toolC", "toolD"})
	trequire.NoError(t, err)
	trequire.Equal(t, []string{"toolA", "toolB", "toolC", "toolD"}, resourceIDs)
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

	resourceIDs, err := filter(ctx, ScopeMCPConnect, []string{"toolA", "toolB", "toolC", "toolD"})
	trequire.NoError(t, err)
	trequire.Equal(t, []string{"toolA", "toolB"}, resourceIDs)
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

	resourceIDs, err := filter(ctx, ScopeBuildRead, []string{"proj:123", "proj:456"})
	trequire.NoError(t, err)
	trequire.Equal(t, []string{"proj:123", "proj:456"}, resourceIDs)
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

	resourceIDs, err := filter(ctx, ScopeBuildRead, []string{"proj:123", "proj:456"})
	trequire.NoError(t, err)
	trequire.Equal(t, []string{"proj:123"}, resourceIDs)
}

func TestFilter_requiresGrantsInContext(t *testing.T) {
	t.Parallel()

	resourceIDs, err := filter(enterpriseCtx(), ScopeBuildRead, []string{"proj_alpha"})
	trequire.Error(t, err)
	trequire.Nil(t, resourceIDs)
	trequire.ErrorIs(t, err, ErrMissingGrants)
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

	resourceIDs, err := filter(ctx, ScopeBuildRead, []string{"proj_alpha", ""})
	trequire.Error(t, err)
	trequire.Nil(t, resourceIDs)

	var invalidErr *InvalidCheckError
	trequire.ErrorAs(t, err, &invalidErr)
	trequire.ErrorIs(t, err, ErrInvalidCheck)
	trequire.Equal(t, ScopeBuildRead, invalidErr.Scope)
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

	resourceIDs, err := filter(ctx, ScopeBuildRead, []string{"proj_alpha", WildcardResource})
	trequire.Error(t, err)
	trequire.Nil(t, resourceIDs)

	var invalidErr *InvalidCheckError
	trequire.ErrorAs(t, err, &invalidErr)
	trequire.ErrorIs(t, err, ErrInvalidCheck)
	trequire.Equal(t, ScopeBuildRead, invalidErr.Scope)
	trequire.Equal(t, WildcardResource, invalidErr.ResourceID)
}

func TestFilter_skipsForNonEnterpriseAccount(t *testing.T) {
	t.Parallel()

	resourceIDs, err := filter(nonEnterpriseCtx(), ScopeBuildRead, []string{"proj_123", "proj_456"})
	trequire.NoError(t, err)
	trequire.Equal(t, []string{"proj_123", "proj_456"}, resourceIDs)
}

func TestRequire_skipsForAPIKeyAuth(t *testing.T) {
	t.Parallel()

	err := require(enterpriseAPIKeyCtx(), Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	trequire.NoError(t, err)
}

func TestRequireAny_skipsForAPIKeyAuth(t *testing.T) {
	t.Parallel()

	err := requireAny(enterpriseAPIKeyCtx(), Check{Scope: ScopeBuildRead, ResourceID: "proj_123"})
	trequire.NoError(t, err)
}

func TestFilter_skipsForAPIKeyAuth(t *testing.T) {
	t.Parallel()

	resourceIDs, err := filter(enterpriseAPIKeyCtx(), ScopeBuildRead, []string{"proj_123", "proj_456"})
	trequire.NoError(t, err)
	trequire.Equal(t, []string{"proj_123", "proj_456"}, resourceIDs)
}
