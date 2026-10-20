package admin

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deviceagentversions"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

type stubHookClientsReader struct {
	sessions   []telemetry.HookClientSessionRow
	inference  []telemetry.InferenceHookTrafficRow
	err        error
	reads      *atomic.Int32
	inferReads *atomic.Int32
}

func (s stubHookClientsReader) Sessions(context.Context, int) ([]telemetry.HookClientSessionRow, error) {
	if s.reads != nil {
		s.reads.Add(1)
	}
	return s.sessions, s.err
}

func (s stubHookClientsReader) InferenceTraffic(context.Context, int) ([]telemetry.InferenceHookTrafficRow, error) {
	if s.inferReads != nil {
		s.inferReads.Add(1)
	}
	return s.inference, s.err
}

func TestGetHookClients_UnavailableWithoutReader(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)

	_, err := svc.GetHookClients(ctx, &gen.GetHookClientsPayload{AdminSessionToken: nil, WindowDays: 15, Refresh: true})

	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnavailable, shareable.Code)
}

func TestGetHookClients_LabelsOrganizationsFromBothGrids(t *testing.T) {
	t.Parallel()

	ctx, svc, db := newTestAdminService(t)
	knownID := "org_" + uuid.NewString()
	slug := "hook-clients-" + uuid.NewString()
	_, err := orgrepo.New(db).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: knownID, Name: "Known Org", Slug: slug, WorkosID: conv.ToPGText(knownID), Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)
	goneID := "org_" + uuid.NewString()

	svc.hookClients = stubHookClientsReader{
		err:        nil,
		reads:      nil,
		inferReads: nil,
		sessions: []telemetry.HookClientSessionRow{
			{OrganizationID: knownID, Day: "2026-10-18", Client: "", Machines: 3, Sessions: 5, MachinesAll: 4},
			{OrganizationID: knownID, Day: "2026-10-18", Client: "curl", Machines: 1, Sessions: 2, MachinesAll: 2},
		},
		inference: []telemetry.InferenceHookTrafficRow{
			{OrganizationID: goneID, Day: "2026-10-18", Surface: "", Users: 2, Events: 9, Sessions: 3},
		},
	}

	result, err := svc.GetHookClients(ctx, &gen.GetHookClientsPayload{AdminSessionToken: nil, WindowDays: 15, Refresh: true})
	require.NoError(t, err)
	require.Equal(t, 15, result.WindowDays)
	require.Len(t, result.Sessions, 2)
	require.Equal(t, int64(4), result.Sessions[0].MachinesAll)
	require.Len(t, result.Inference, 1)

	byID := map[string]*gen.AdminHookClientOrganization{}
	for _, organization := range result.Organizations {
		byID[organization.ID] = organization
	}
	require.Len(t, byID, 2)
	require.NotNil(t, byID[knownID].Name)
	require.Equal(t, "Known Org", *byID[knownID].Name)
	require.Equal(t, slug, *byID[knownID].Slug)
	// An organization deleted since its telemetry was written still appears,
	// unnamed, rather than dropping its rows from the grid.
	require.Nil(t, byID[goneID].Name)
	require.Nil(t, byID[goneID].Slug)
}

func TestGetHookClients_PreservesReaderErrorCode(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)
	svc.hookClients = stubHookClientsReader{
		sessions:   nil,
		inference:  nil,
		err:        oops.E(oops.CodeInvalid, nil, "bad window"),
		reads:      nil,
		inferReads: nil,
	}

	_, err := svc.GetHookClients(ctx, &gen.GetHookClientsPayload{AdminSessionToken: nil, WindowDays: 15, Refresh: true})

	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeInvalid, shareable.Code)
}

func TestGetHookClients_ServesFromCacheUntilRefresh(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)
	reads := &atomic.Int32{}
	inferReads := &atomic.Int32{}
	svc.hookClients = stubHookClientsReader{
		sessions:   []telemetry.HookClientSessionRow{},
		inference:  []telemetry.InferenceHookTrafficRow{},
		err:        nil,
		reads:      reads,
		inferReads: inferReads,
	}
	// A window no other test uses, so the shared cache starts empty for it.
	const window = 7

	first, err := svc.GetHookClients(ctx, &gen.GetHookClientsPayload{AdminSessionToken: nil, WindowDays: window, Refresh: true})
	require.NoError(t, err)
	require.False(t, first.FromCache)

	cached, err := svc.GetHookClients(ctx, &gen.GetHookClientsPayload{AdminSessionToken: nil, WindowDays: window, Refresh: false})
	require.NoError(t, err)
	require.True(t, cached.FromCache)
	require.Equal(t, first.QueriedAt, cached.QueriedAt)
	require.Equal(t, int32(1), reads.Load())
	require.Equal(t, int32(1), inferReads.Load())

	refreshed, err := svc.GetHookClients(ctx, &gen.GetHookClientsPayload{AdminSessionToken: nil, WindowDays: window, Refresh: true})
	require.NoError(t, err)
	require.False(t, refreshed.FromCache)
	require.Equal(t, int32(2), reads.Load())
	require.Equal(t, int32(2), inferReads.Load())
}

type stubDeviceAgentVersions struct {
	rows []deviceagentversions.Row
	err  error
}

func (s stubDeviceAgentVersions) Read(context.Context, int, time.Time) ([]deviceagentversions.Row, error) {
	return s.rows, s.err
}

func TestGetHookClients_IncludesDeviceAgentsAndSurvivesRedisFailure(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)
	svc.hookClients = stubHookClientsReader{
		sessions:   []telemetry.HookClientSessionRow{},
		inference:  []telemetry.InferenceHookTrafficRow{},
		err:        nil,
		reads:      nil,
		inferReads: nil,
	}
	agentOrg := "org_" + uuid.NewString()
	svc.SetDeviceAgentVersions(stubDeviceAgentVersions{
		err: nil,
		rows: []deviceagentversions.Row{
			{OrganizationID: agentOrg, Day: "2026-10-19", Version: "", Machines: 3},
			{OrganizationID: agentOrg, Day: "2026-10-19", Version: "0.3.31", Machines: 3},
		},
	})

	result, err := svc.GetHookClients(ctx, &gen.GetHookClientsPayload{AdminSessionToken: nil, WindowDays: 15, Refresh: true})
	require.NoError(t, err)
	require.Len(t, result.DeviceAgents, 2)
	require.Equal(t, "0.3.31", result.DeviceAgents[1].Version)
	require.Equal(t, int64(3), result.DeviceAgents[1].Machines)
	require.Len(t, result.Organizations, 1)
	require.Equal(t, agentOrg, result.Organizations[0].ID)

	// A Redis fault leaves the device agent grid empty instead of failing the page.
	svc.SetDeviceAgentVersions(stubDeviceAgentVersions{rows: nil, err: errors.New("redis down")})
	result, err = svc.GetHookClients(ctx, &gen.GetHookClientsPayload{AdminSessionToken: nil, WindowDays: 15, Refresh: true})
	require.NoError(t, err)
	require.Empty(t, result.DeviceAgents)
}
