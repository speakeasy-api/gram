package environments_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/environments"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestEnvironmentsService_CloneEnvironment(t *testing.T) {
	t.Parallel()

	t.Run("clone with copy_values=false copies only names with empty placeholders", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestEnvironmentService(t)

		source, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			OrganizationID:   "",
			Name:             "clone-source-empty",
			Description:      nil,
			Entries: []*gen.EnvironmentEntryInput{
				{Name: "API_KEY", Value: "super-secret-value"},
				{Name: "DATABASE_URL", Value: "postgres://example/db"},
			},
		})
		require.NoError(t, err)

		clone, err := ti.service.CloneEnvironment(ctx, &gen.CloneEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			Slug:             source.Slug,
			NewName:          "clone-target-no-values",
			CopyValues:       nil,
		})
		require.NoError(t, err)
		require.NotEqual(t, source.ID, clone.ID)
		require.Equal(t, "clone-target-no-values", clone.Name)
		require.Equal(t, "clone-target-no-values", string(clone.Slug))
		require.Len(t, clone.Entries, 2)

		// Empty plaintext redacts to "<EMPTY>" (see redactedEnvironment in shared.go).
		for _, e := range clone.Entries {
			require.Equal(t, "<EMPTY>", e.Value, "entry %q should be the empty placeholder", e.Name)
		}

		// Source must be unchanged: still has its real (non-empty) values.
		list, err := ti.service.ListEnvironments(ctx, &gen.ListEnvironmentsPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
		})
		require.NoError(t, err)
		var sourceFound bool
		for _, env := range list.Environments {
			if env.ID == source.ID {
				sourceFound = true
				require.Len(t, env.Entries, 2)
				for _, e := range env.Entries {
					require.NotEqual(t, "<EMPTY>", e.Value, "source entry %q must not have been mutated to empty", e.Name)
				}
			}
		}
		require.True(t, sourceFound, "source environment should still exist after clone")
	})

	t.Run("clone with copy_values=true copies ciphertext verbatim", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestEnvironmentService(t)

		source, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			OrganizationID:   "",
			Name:             "clone-source-with-values",
			Description:      nil,
			Entries: []*gen.EnvironmentEntryInput{
				{Name: "API_KEY", Value: "super-secret-value"},
				{Name: "SHORT", Value: "ab"},
			},
		})
		require.NoError(t, err)

		copyValues := true
		clone, err := ti.service.CloneEnvironment(ctx, &gen.CloneEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			Slug:             source.Slug,
			NewName:          "clone-target-with-values",
			CopyValues:       &copyValues,
		})
		require.NoError(t, err)
		require.Len(t, clone.Entries, 2)

		// redactedEnvironment is deterministic on plaintext (val[:3]+"*****" or "*****"
		// for short values), so identical source/clone redactions imply identical plaintext.
		// Note: ValueHash hashes the redacted display string (not the raw plaintext) — see
		// computeValueHash in impl.go. We assert hashes match as a structural check only;
		// the redaction equality above is what proves the values were actually copied.
		sourceByName := make(map[string]*types.EnvironmentEntry, len(source.Entries))
		for _, e := range source.Entries {
			sourceByName[e.Name] = e
		}
		for _, e := range clone.Entries {
			s, ok := sourceByName[e.Name]
			require.True(t, ok, "cloned entry %q should exist in source", e.Name)
			require.Equal(t, s.Value, e.Value, "cloned entry %q redaction should equal source redaction", e.Name)
			require.Equal(t, s.ValueHash, e.ValueHash, "cloned entry %q value_hash should equal source value_hash", e.Name)
		}
	})

	t.Run("duplicate clone name returns conflict", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestEnvironmentService(t)

		source, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			OrganizationID:   "",
			Name:             "dup-source",
			Description:      nil,
			Entries:          []*gen.EnvironmentEntryInput{},
		})
		require.NoError(t, err)

		_, err = ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			OrganizationID:   "",
			Name:             "already-taken",
			Description:      nil,
			Entries:          []*gen.EnvironmentEntryInput{},
		})
		require.NoError(t, err)

		_, err = ti.service.CloneEnvironment(ctx, &gen.CloneEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			Slug:             source.Slug,
			NewName:          "already-taken",
			CopyValues:       nil,
		})
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeConflict, oopsErr.Code)
	})

	t.Run("non-existent source returns not found", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestEnvironmentService(t)

		_, err := ti.service.CloneEnvironment(ctx, &gen.CloneEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			Slug:             "does-not-exist",
			NewName:          "anything",
			CopyValues:       nil,
		})
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeNotFound, oopsErr.Code)
	})
}

func TestEnvironmentsService_CloneEnvironment_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	source, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "audit-clone-source",
		Description:      nil,
		Entries: []*gen.EnvironmentEntryInput{
			{Name: "API_KEY", Value: "abc"},
		},
	})
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionEnvironmentCreate)
	require.NoError(t, err)

	clone, err := ti.service.CloneEnvironment(ctx, &gen.CloneEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             source.Slug,
		NewName:          "audit-clone-target",
		CopyValues:       nil,
	})
	require.NoError(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionEnvironmentCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionEnvironmentCreate)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionEnvironmentCreate), record.Action)
	require.Equal(t, "environment", record.SubjectType)
	require.Equal(t, clone.Name, record.SubjectDisplay)
	require.Equal(t, string(clone.Slug), record.SubjectSlug)
	require.Nil(t, record.BeforeSnapshot)
	require.Nil(t, record.AfterSnapshot)
}

func TestEnvironments_RBAC_Clone_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	source, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "rbac-clone-no-grants-source",
		Description:      nil,
		Entries:          []*gen.EnvironmentEntryInput{},
	})
	require.NoError(t, err)

	ctx = authztest.WithExactGrants(t, ctx)

	_, err = ti.service.CloneEnvironment(ctx, &gen.CloneEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             source.Slug,
		NewName:          "rbac-clone-no-grants-target",
		CopyValues:       nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEnvironments_RBAC_Clone_DeniedWithReadOnlyGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	source, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "rbac-clone-readonly-source",
		Description:      nil,
		Entries:          []*gen.EnvironmentEntryInput{},
	})
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx,
		authz.Grant{
			Scope:    authz.ScopeProjectRead,
			Selector: authz.NewSelector(authz.ScopeProjectRead, authCtx.ProjectID.String()),
		},
	)

	_, err = ti.service.CloneEnvironment(ctx, &gen.CloneEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             source.Slug,
		NewName:          "rbac-clone-readonly-target",
		CopyValues:       nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEnvironments_RBAC_Clone_AllowedWithProjectWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	source, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "rbac-clone-pw-source",
		Description:      nil,
		Entries:          []*gen.EnvironmentEntryInput{},
	})
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	// project:write satisfies both environment:write (destination) and environment:read
	// (source), since environment:* are now project-bounded and resolve via scope expansion.
	ctx = authztest.WithExactGrants(t, ctx,
		authz.Grant{
			Scope:    authz.ScopeProjectWrite,
			Selector: authz.NewSelector(authz.ScopeProjectWrite, authCtx.ProjectID.String()),
		},
	)

	_, err = ti.service.CloneEnvironment(ctx, &gen.CloneEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             source.Slug,
		NewName:          "rbac-clone-pw-target",
		CopyValues:       nil,
	})
	require.NoError(t, err)
}

func TestEnvironments_RBAC_Clone_AllowedWithEnvironmentWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	source, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "rbac-clone-env-source",
		Description:      nil,
		Entries:          []*gen.EnvironmentEntryInput{},
	})
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	// environment:write at the project satisfies both the destination write check and,
	// via scope expansion (environment:read is satisfied by environment:write at the same
	// project_id), the source-read check.
	ctx = authztest.WithExactGrants(t, ctx,
		authz.Grant{
			Scope:    authz.ScopeEnvironmentWrite,
			Selector: authz.NewSelector(authz.ScopeEnvironmentWrite, authCtx.ProjectID.String()),
		},
	)

	_, err = ti.service.CloneEnvironment(ctx, &gen.CloneEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             source.Slug,
		NewName:          "rbac-clone-env-target",
		CopyValues:       nil,
	})
	require.NoError(t, err)
}

func TestEnvironments_RBAC_Clone_DeniedWithEnvironmentReadOnly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	source, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "rbac-clone-er-only-source",
		Description:      nil,
		Entries:          []*gen.EnvironmentEntryInput{},
	})
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	// environment:read at the project lets the user view envs but does NOT
	// satisfy the environment:write destination check, so clone must be rejected.
	ctx = authztest.WithExactGrants(t, ctx,
		authz.Grant{
			Scope:    authz.ScopeEnvironmentRead,
			Selector: authz.NewSelector(authz.ScopeEnvironmentRead, authCtx.ProjectID.String()),
		},
	)

	_, err = ti.service.CloneEnvironment(ctx, &gen.CloneEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             source.Slug,
		NewName:          "rbac-clone-er-only-target",
		CopyValues:       nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

// project:read alone must NOT grant environment:read or environment:write.
// This guards against a generic project-viewer escalating into reading or
// cloning environment values (which include secrets) — qstearns' explicit
// constraint when the environment scopes were introduced.
func TestEnvironments_RBAC_Clone_DeniedWithProjectReadOnly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	source, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "rbac-clone-pr-only-source",
		Description:      nil,
		Entries:          []*gen.EnvironmentEntryInput{},
	})
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx,
		authz.Grant{
			Scope:    authz.ScopeProjectRead,
			Selector: authz.NewSelector(authz.ScopeProjectRead, authCtx.ProjectID.String()),
		},
	)

	_, err = ti.service.CloneEnvironment(ctx, &gen.CloneEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             source.Slug,
		NewName:          "rbac-clone-pr-only-target",
		CopyValues:       nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
