package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func assignmentModes(assignments []*gen.PluginAssignment) map[string]string {
	out := make(map[string]string, len(assignments))
	for _, a := range assignments {
		out[a.PrincipalUrn] = a.InstallMode
	}
	return out
}

func TestPluginsService_SetPluginAssignments_StoresInstallModes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Install Modes"})
	require.NoError(t, err)
	engineering := createTestRolePrincipal(t, ctx, ti, "engineering")

	result, err := ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{
		PluginID:      plugin.ID,
		PrincipalUrns: []string{engineering, "*"},
		InstallModes:  map[string]string{engineering: "required", "*": "available"},
	})
	require.NoError(t, err)
	require.Equal(t, map[string]string{engineering: "required", "*": "available"}, assignmentModes(result.Assignments))

	fetched, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: plugin.ID})
	require.NoError(t, err)
	require.Equal(t, map[string]string{engineering: "required", "*": "available"}, assignmentModes(fetched.Assignments))
}

func TestPluginsService_SetPluginAssignments_OmittedModeKeepsCurrentOrDefaults(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Mode Preservation"})
	require.NoError(t, err)
	engineering := createTestRolePrincipal(t, ctx, ti, "engineering")
	gtm := createTestRolePrincipal(t, ctx, ti, "gtm")

	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{
		PluginID:      plugin.ID,
		PrincipalUrns: []string{engineering},
		InstallModes:  map[string]string{engineering: "required"},
	})
	require.NoError(t, err)

	// A client that doesn't send modes must not reset the existing one.
	result, err := ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{
		PluginID:      plugin.ID,
		PrincipalUrns: []string{engineering, gtm},
	})
	require.NoError(t, err)
	require.Equal(t, map[string]string{engineering: "required", gtm: "default"}, assignmentModes(result.Assignments))
}

func TestPluginsService_SetPluginAssignments_NormalizesInstallModeKeys(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Mode Key Normalization"})
	require.NoError(t, err)

	result, err := ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{
		PluginID:      plugin.ID,
		PrincipalUrns: []string{"email:dev@acme.corp"},
		InstallModes:  map[string]string{"email:Dev@Acme.Corp": "available"},
	})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"email:dev@acme.corp": "available"}, assignmentModes(result.Assignments))
}

func TestPluginsService_SetPluginAssignments_InstallModeForUnassignedPrincipalReturnsBadRequest(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Stray Mode"})
	require.NoError(t, err)

	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{
		PluginID:      plugin.ID,
		PrincipalUrns: []string{"*"},
		InstallModes:  map[string]string{"email:dev@acme.corp": "required"},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestPluginsService_SetPluginAssignments_UnknownInstallModeReturnsBadRequest(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Unknown Mode"})
	require.NoError(t, err)

	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{
		PluginID:      plugin.ID,
		PrincipalUrns: []string{"*"},
		InstallModes:  map[string]string{"*": "mandatory"},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestPluginsService_SetPluginAssignments_AuditsInstallModes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Audit Modes"})
	require.NoError(t, err)

	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{
		PluginID:      plugin.ID,
		PrincipalUrns: []string{"*"},
		InstallModes:  map[string]string{"*": "required"},
	})
	require.NoError(t, err)

	rec, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionPluginAssignmentsSet)
	require.NoError(t, err)
	meta, err := audittest.DecodeAuditData(rec.Metadata)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"*": "required"}, meta["install_modes"])
}
