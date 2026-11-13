package environments_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/environments"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// A typed id only proves an id is well formed. That an environment belongs to
// the caller's project and organization rests on the handler loading the row
// inside them, so it is tested here on the handlers themselves: a caller
// acting in another project and organization who names this project's
// environment must not reach it.
func TestEnvironments_HandlersLoadEnvironmentsInsideTheCallersProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	env, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "tenant-bound-env",
		Description:      nil,
		Entries: []*gen.EnvironmentEntryInput{
			{Name: "KEY1", Value: new("value1"), IsSecret: new(false)},
		},
	})
	require.NoError(t, err)

	outsider := callerInAnotherProject(t, ctx)

	requireNotReached := func(err error) {
		t.Helper()
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Contains(t, []oops.Code{oops.CodeNotFound, oops.CodeForbidden}, oopsErr.Code)
	}

	_, err = ti.service.UpdateEnvironment(outsider, &gen.UpdateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             env.Slug,
		Description:      nil,
		Name:             new("renamed-by-outsider"),
		EntriesToUpdate:  []*gen.EnvironmentEntryInput{},
		EntriesToRemove:  []string{},
	})
	requireNotReached(err)

	_, err = ti.service.CloneEnvironment(outsider, &gen.CloneEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             env.Slug,
		NewName:          "cloned-by-outsider",
		CopyValues:       nil,
	})
	requireNotReached(err)

	err = ti.service.DeleteEnvironment(outsider, &gen.DeleteEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             env.Slug,
	})
	require.NoError(t, err, "deleting a slug the caller's project does not have is a no-op")

	listed, err := ti.service.ListEnvironments(ctx, &gen.ListEnvironmentsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	names := make([]string, 0, len(listed.Environments))
	for _, e := range listed.Environments {
		names = append(names, e.Name)
	}
	require.Contains(t, names, "tenant-bound-env")
	require.NotContains(t, names, "renamed-by-outsider")
	require.NotContains(t, names, "cloned-by-outsider")
}

// callerInAnotherProject is the same authenticated caller acting in a
// different project of a different organization.
func callerInAnotherProject(t *testing.T, ctx context.Context) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	other := *authCtx
	otherProject := uuid.New()
	other.ProjectID = &otherProject
	other.ActiveOrganizationID = "org-" + uuid.NewString()
	return contextvalues.SetAuthContext(ctx, &other)
}
