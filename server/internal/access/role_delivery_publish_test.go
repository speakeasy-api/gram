package access

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
)

// recordingPublishSignaler records the projects a role change asked to
// republish directly, without the outbox.
type recordingPublishSignaler struct {
	mu       sync.Mutex
	projects []uuid.UUID
}

func (r *recordingPublishSignaler) SignalPluginPublish(_ context.Context, projectID uuid.UUID, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.projects = append(r.projects, projectID)
	return nil
}

func (r *recordingPublishSignaler) take() []uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	got := r.projects
	r.projects = nil
	return got
}

func TestService_UpdateRole_DeliveryChangeSignalsPublish(t *testing.T) {
	t.Parallel()
	ctx, f := newRoleDeliveryFixture(t)
	f.signals.take()

	f.patch(t, ctx, f.roleID, true)
	require.Equal(t, []uuid.UUID{*f.ac.ProjectID}, f.signals.take(), "a grant that delivers a server republishes its project")

	f.patch(t, ctx, f.roleID, false)
	require.Equal(t, []uuid.UUID{*f.ac.ProjectID}, f.signals.take(), "a grant revocation that removes a server republishes its project")
}

func TestService_UpdateRole_NoDeliveryChangeDoesNotSignalPublish(t *testing.T) {
	t.Parallel()
	ctx, f := newRoleDeliveryFixture(t)
	f.signals.take()

	projectID := f.ac.ProjectID.String()
	read := &gen.RoleGrant{Scope: string(authz.ScopeMCPRead), Selectors: []*gen.Selector{{ResourceKind: "mcp", ResourceID: "*", ProjectID: &projectID}}}
	_, err := f.ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{ID: f.roleID, AddGrants: []*gen.RoleGrant{read}})
	require.NoError(t, err)
	name := "Renamed delivery"
	_, err = f.ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{ID: f.roleID, Name: &name})
	require.NoError(t, err)
	require.Empty(t, f.signals.take())
}

func TestService_SetResourceAudience_DeliveryChangeSignalsPublish(t *testing.T) {
	t.Parallel()
	ctx, f := newRoleDeliveryFixture(t)
	f.signals.take()

	role := "role:organization:" + f.roleID
	f.saveAudience(t, ctx, &gen.SetResourceAudienceEntry{PrincipalUrn: role, Level: "use"})
	require.Equal(t, []uuid.UUID{*f.ac.ProjectID}, f.signals.take())

	f.saveAudience(t, ctx)
	require.Equal(t, []uuid.UUID{*f.ac.ProjectID}, f.signals.take())
}
