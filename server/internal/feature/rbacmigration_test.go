package feature

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRBACMigrationVariant(t *testing.T) {
	t.Parallel()

	flags := &InMemory{}
	flags.SetFlagVariant(RBACMigrationFlag("project"), "org_shadow", RBACMigrationVariantShadow)
	flags.SetFlagVariant(RBACMigrationFlag("project"), "org_new", RBACMigrationVariantNew)
	flags.SetFlagVariant(RBACMigrationFlag("project"), "org_other", "unexpected")
	flags.SetFlagVariant(RBACMigrationFlag("Project!"), "org_new", RBACMigrationVariantNew)

	ctx := t.Context()
	require.Equal(t, RBACMigrationVariantShadow, RBACMigrationVariant(ctx, flags, "project", "org_shadow"))
	require.Equal(t, RBACMigrationVariantNew, RBACMigrationVariant(ctx, flags, "project", "org_new"))
	require.Empty(t, RBACMigrationVariant(ctx, flags, "project", "org_other"), "an unknown variant keeps the engine deciding")
	require.Empty(t, RBACMigrationVariant(ctx, flags, "project", "org_unset"))
	require.Empty(t, RBACMigrationVariant(ctx, flags, "project", ""))
	require.Empty(t, RBACMigrationVariant(ctx, nil, "project", "org_new"))
	require.Empty(t, RBACMigrationVariant(ctx, flags, "Project!", "org_new"), "an invalid unit never resolves a flag")
	require.Equal(t, Flag("rbac-migration-project"), RBACMigrationFlag("project"))
}
