package admin

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type groupIdentifyCall struct {
	groupType  string
	groupKey   string
	properties map[string]any
}

// recordingGroupIdentifier stands in for the PostHog client, which is the one
// dependency here that cannot run in a test.
type recordingGroupIdentifier struct {
	mu    sync.Mutex
	calls []groupIdentifyCall
	err   error
}

func (r *recordingGroupIdentifier) GroupIdentify(_ context.Context, groupType string, groupKey string, groupProperties map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, groupIdentifyCall{groupType: groupType, groupKey: groupKey, properties: groupProperties})
	return r.err
}

func (r *recordingGroupIdentifier) recorded() []groupIdentifyCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]groupIdentifyCall(nil), r.calls...)
}

func TestUpdateOrganization_InternalMirrorsToPostHog(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	identifier := &recordingGroupIdentifier{}
	svc.SetOrganizationGroupIdentifier(identifier)
	seedOrg(t, ctx, conn, orgFixture{id: "org_mirror", name: "Mirror Co", slug: "mirror-co"})

	for _, internal := range []bool{true, false} {
		_, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: "org_mirror", Internal: new(internal)})
		require.NoError(t, err)
	}

	require.Equal(t, []groupIdentifyCall{
		{groupType: "organization", groupKey: "mirror-co", properties: map[string]any{"is_internal": true}},
		{groupType: "organization", groupKey: "mirror-co", properties: map[string]any{"is_internal": false}},
	}, identifier.recorded(), "the group is keyed by slug and un-marking must send false explicitly")
}

func TestUpdateOrganization_InternalResendsUnchangedValue(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	identifier := &recordingGroupIdentifier{}
	svc.SetOrganizationGroupIdentifier(identifier)
	seedOrg(t, ctx, conn, orgFixture{id: "org_resend", name: "Resend Co", slug: "resend-co"})

	for range 2 {
		_, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: "org_resend", Internal: new(true)})
		require.NoError(t, err)
	}

	calls := identifier.recorded()
	require.Len(t, calls, 2, "saving again must re-send so drift can be repaired")
	require.Equal(t, map[string]any{"is_internal": true}, calls[1].properties)
}

func TestUpdateOrganization_InternalSurvivesPostHogFailure(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	identifier := &recordingGroupIdentifier{err: errors.New("posthog unreachable")}
	svc.SetOrganizationGroupIdentifier(identifier)
	seedOrg(t, ctx, conn, orgFixture{id: "org_ph_down", name: "Down Co", slug: "down-co"})

	res, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: "org_ph_down", Internal: new(true)})
	require.NoError(t, err)
	require.True(t, res.Internal)
	// The failing call has to have been made, or this proves nothing about
	// how a failure is handled.
	require.Len(t, identifier.recorded(), 1)
}

func TestUpdateOrganization_InternalWithoutPostHog(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	svc.SetOrganizationGroupIdentifier(nil)
	seedOrg(t, ctx, conn, orgFixture{id: "org_no_ph", name: "Offline Co", slug: "offline-co"})

	res, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: "org_no_ph", Internal: new(true)})
	require.NoError(t, err)
	require.True(t, res.Internal)
}

func TestUpdateOrganization_InternalUnknownOrganizationSendsNothing(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)
	identifier := &recordingGroupIdentifier{}
	svc.SetOrganizationGroupIdentifier(identifier)

	_, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: "org_missing", Internal: new(true)})
	requireOopsCode(t, err, oops.CodeNotFound)
	require.Empty(t, identifier.recorded())
}
