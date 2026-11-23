package catalogpromotion

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	authrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/catalogpromotion/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func attachPromotionAuthClient(t *testing.T, f promotionFixture) uuid.UUID {
	t.Helper()
	q := testrepo.New(f.db)
	clientID, err := q.CreateAuthClientFixture(t.Context(), testrepo.CreateAuthClientFixtureParams{OrganizationID: f.target.OrganizationID, ProjectID: uuid.NullUUID{UUID: f.target.ProjectID, Valid: true}, ClientType: "static_credential", Name: "Synthetic credential"})
	require.NoError(t, err)
	require.NoError(t, q.AssociateAuthClientFixture(t.Context(), testrepo.AssociateAuthClientFixtureParams{OrganizationID: f.target.OrganizationID, ProjectID: f.target.ProjectID, McpServerID: f.target.MCPServerID, AuthClientID: clientID}))
	return clientID
}

func TestPromotionRejectsAttachedAuthClientHeadersAndStaleEvidence(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	clientID := attachPromotionAuthClient(t, f)
	_, err := f.promote(t)
	require.ErrorIs(t, err, ErrStale, "attachment changes invalidate existing approval even without headers")
	attached, err := repo.New(f.db).GetSource(t.Context(), repo.GetSourceParams{OrganizationID: f.target.OrganizationID, ProjectID: f.target.ProjectID, McpServerID: f.target.MCPServerID})
	require.NoError(t, err)
	require.Zero(t, attached.HeaderCount)
	require.NoError(t, testrepo.New(f.db).CreateAuthClientHeaderFixture(t.Context(), testrepo.CreateAuthClientHeaderFixtureParams{AuthClientID: clientID, OrganizationID: f.target.OrganizationID, Name: "X-API-Key", IsSecret: true, Value: conv.ToPGText("synthetic-encrypted-value")}))
	refreshed, err := f.service.GetCandidate(t.Context(), f.target)
	require.NoError(t, err)
	require.False(t, refreshed.Eligible)
	require.Equal(t, "unsupported_configuration", refreshed.Reason)
	require.NotEqual(t, attached.SourceRevision, refreshed.SourceRevision)
	require.NotContains(t, refreshed.SourceRevision, "synthetic-encrypted-value")
	f.request.SourceRevision = refreshed.SourceRevision
	_, err = f.promote(t)
	require.ErrorIs(t, err, ErrIneligible)

	q := repo.New(f.db)
	params := repo.ChangePromotionAuthHeaderFixtureParams{OrganizationID: f.target.OrganizationID, ProjectID: f.target.ProjectID, AuthClientID: clientID, IsRequired: true}
	require.NoError(t, q.ChangePromotionAuthHeaderFixture(t.Context(), params))
	required, err := q.GetSource(t.Context(), repo.GetSourceParams{OrganizationID: f.target.OrganizationID, ProjectID: f.target.ProjectID, McpServerID: f.target.MCPServerID})
	require.NoError(t, err)
	require.EqualValues(t, 1, required.HeaderCount)
	require.NotEqual(t, refreshed.SourceRevision, required.SourceRevision)
	params.DeletedAt = conv.ToPGTimestamptz(time.Now())
	require.NoError(t, q.ChangePromotionAuthHeaderFixture(t.Context(), params))
	removed, err := q.GetSource(t.Context(), repo.GetSourceParams{OrganizationID: f.target.OrganizationID, ProjectID: f.target.ProjectID, McpServerID: f.target.MCPServerID})
	require.NoError(t, err)
	require.Zero(t, removed.HeaderCount)
	require.NotEqual(t, required.SourceRevision, removed.SourceRevision)
	require.True(t, removed.ChangedAt.Time.After(attached.ChangedAt.Time))
}

func TestPromotionAuthClientConfigurationAndDetachInvalidateEvidence(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	clientID := attachPromotionAuthClient(t, f)
	params := repo.GetSourceParams{OrganizationID: f.target.OrganizationID, ProjectID: f.target.ProjectID, McpServerID: f.target.MCPServerID}
	before, err := repo.New(f.db).GetSource(t.Context(), params)
	require.NoError(t, err)
	_, err = authrepo.New(f.db).UpdateAuthClient(t.Context(), authrepo.UpdateAuthClientParams{ID: clientID, OrganizationID: f.target.OrganizationID, Name: "Synthetic disabled", Enabled: false})
	require.NoError(t, err)
	disabled, err := repo.New(f.db).GetSource(t.Context(), params)
	require.NoError(t, err)
	require.NotEqual(t, before.SourceRevision, disabled.SourceRevision)
	_, err = authrepo.New(f.db).DetachAuthClientFromServer(t.Context(), authrepo.DetachAuthClientFromServerParams{AuthClientID: clientID, McpServerID: f.target.MCPServerID, OrganizationID: f.target.OrganizationID})
	require.NoError(t, err)
	detached, err := repo.New(f.db).GetSource(t.Context(), params)
	require.NoError(t, err)
	require.NotEqual(t, disabled.SourceRevision, detached.SourceRevision)
}

func TestPromotionSourceLocksFenceAuthMutations(t *testing.T) {
	t.Parallel()
	for _, mutation := range []string{"configure", "header_insert", "header_update", "detach", "attach"} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()
			f := newPromotionFixture(t)
			clientID := attachPromotionAuthClient(t, f)
			require.NoError(t, testrepo.New(f.db).CreateAuthClientHeaderFixture(t.Context(), testrepo.CreateAuthClientHeaderFixtureParams{AuthClientID: clientID, OrganizationID: f.target.OrganizationID, Name: "X-API-Key", IsSecret: true, Value: conv.ToPGText("synthetic")}))
			other, createErr := testrepo.New(f.db).CreateAuthClientFixture(t.Context(), testrepo.CreateAuthClientFixtureParams{OrganizationID: f.target.OrganizationID, ProjectID: uuid.NullUUID{UUID: f.target.ProjectID, Valid: true}, ClientType: "static_credential", Name: "Synthetic other"})
			require.NoError(t, createErr)
			tx := testenv.BeginTx(t, t.Context(), f.db)
			_, err := lockSource(t.Context(), tx, f.target)
			require.NoError(t, err)
			probe := testenv.BeginTx(t, t.Context(), f.db)
			testenv.SetLockTimeout(t, t.Context(), probe, 50*time.Millisecond)
			switch mutation {
			case "configure":
				_, err = authrepo.New(probe).UpdateAuthClient(t.Context(), authrepo.UpdateAuthClientParams{ID: clientID, OrganizationID: f.target.OrganizationID, Name: "Synthetic disabled", Enabled: false})
			case "header_insert":
				err = testrepo.New(probe).CreateAuthClientHeaderFixture(t.Context(), testrepo.CreateAuthClientHeaderFixtureParams{AuthClientID: clientID, OrganizationID: f.target.OrganizationID, Name: "X-Other", Value: conv.ToPGText("synthetic")})
			case "header_update":
				err = repo.New(probe).ChangePromotionAuthHeaderFixture(t.Context(), repo.ChangePromotionAuthHeaderFixtureParams{OrganizationID: f.target.OrganizationID, ProjectID: f.target.ProjectID, AuthClientID: clientID, IsRequired: true})
			case "detach":
				_, err = authrepo.New(probe).DetachAuthClientFromServer(t.Context(), authrepo.DetachAuthClientFromServerParams{AuthClientID: clientID, McpServerID: f.target.MCPServerID, OrganizationID: f.target.OrganizationID})
			case "attach":
				err = testrepo.New(probe).AssociateAuthClientFixture(t.Context(), testrepo.AssociateAuthClientFixtureParams{OrganizationID: f.target.OrganizationID, ProjectID: f.target.ProjectID, McpServerID: f.target.MCPServerID, AuthClientID: other})
			}
			testenv.RequireLockNotAvailable(t, err)
		})
	}
}

type timedPromotionHealth struct {
	calls     []time.Time
	targets   []telemetry.MCPServerTelemetryTarget
	windows   [][2]time.Time
	recentErr error
}

func (h *timedPromotionHealth) Outcomes(_ context.Context, target telemetry.MCPServerTelemetryTarget, from, to time.Time) (*telemetry.MCPServerOutcomes, error) {
	h.targets = append(h.targets, target)
	h.windows = append(h.windows, [2]time.Time{from, to})
	if len(h.windows) == 2 && h.recentErr != nil {
		return nil, h.recentErr
	}
	out := &telemetry.MCPServerOutcomes{Watermark: to}
	for _, call := range h.calls {
		if !call.Before(from) && !call.After(to) {
			out.Success += 10
		}
	}
	return out, nil
}

func (h *timedPromotionHealth) Series(_ context.Context, _ telemetry.MCPServerTelemetryTarget, from, to time.Time, _ time.Duration) ([]telemetry.MCPServerSeriesPoint, error) {
	var points []telemetry.MCPServerSeriesPoint
	for _, call := range h.calls {
		if !call.Before(from) && !call.After(to) {
			points = append(points, telemetry.MCPServerSeriesPoint{BucketStart: call.UTC().Truncate(24 * time.Hour), Total: 10})
		}
	}
	return points, nil
}

func TestPromotionUsesExactRecentSuccessWindow(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		age      time.Duration
		eligible bool
	}{
		{"Friday evening qualifies on Sunday evening", 46 * time.Hour, true},
		{"exact boundary", 48 * time.Hour, true},
		{"just inside", 48*time.Hour - time.Nanosecond, true},
		{"just outside", 48*time.Hour + time.Nanosecond, false},
		{"project watermark is not server freshness", 72 * time.Hour, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newPromotionFixture(t)
			now := f.health.now.UTC().Truncate(24 * time.Hour).Add(42 * time.Hour)
			h := &timedPromotionHealth{calls: []time.Time{now.Add(-96 * time.Hour), now.Add(-tc.age)}}
			f.service.health = h
			f.service.now = func() time.Time { return now }
			candidate, err := f.service.GetCandidate(t.Context(), f.target)
			require.NoError(t, err)
			require.Equal(t, tc.eligible, candidate.Eligible)
			require.Len(t, h.windows, 2)
			require.Equal(t, [2]time.Time{now.Add(-48 * time.Hour), now}, h.windows[1])
			require.Equal(t, h.targets[0], h.targets[1])
			require.Equal(t, f.target.MCPServerID.String(), h.targets[1].MCPServerID)
		})
	}
}

func TestPromotionFailsClosedWhenRecentTelemetryUnavailable(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	now := f.health.now.Add(time.Minute)
	h := &timedPromotionHealth{calls: []time.Time{now.Add(-96 * time.Hour), now.Add(-46 * time.Hour)}, recentErr: fmt.Errorf("synthetic recent query failure")}
	f.service.health = h
	f.service.now = func() time.Time { return now }
	candidate, err := f.service.GetCandidate(t.Context(), f.target)
	require.NoError(t, err)
	require.False(t, candidate.Eligible)
	require.Equal(t, "telemetry_unavailable", candidate.Reason)
	require.Len(t, h.windows, 2)
}

func TestPromotionRecentFailuresOverrideLaggingWindow(t *testing.T) {
	t.Parallel()
	points := []telemetry.MCPServerSeriesPoint{{BucketStart: time.Now().Add(-24 * time.Hour), Total: 10}, {BucketStart: time.Now(), Total: 10}}
	observed := qualify(&telemetry.MCPServerOutcomes{Success: 20}, points, &telemetry.MCPServerOutcomes{Success: 1, Failed: 1})
	require.Equal(t, "errors_observed", observed.reason)
	require.EqualValues(t, 1, observed.failures)
}

func TestPromotionHeaderDeletionRequiresFreshEvidence(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	source, err := repo.New(f.db).GetSource(t.Context(), repo.GetSourceParams{OrganizationID: f.target.OrganizationID, ProjectID: f.target.ProjectID, McpServerID: f.target.MCPServerID})
	require.NoError(t, err)
	header, err := remoterepo.New(f.db).CreateServerHeader(t.Context(), remoterepo.CreateServerHeaderParams{ProjectID: f.target.ProjectID, RemoteMcpServerID: source.RemoteMcpServerID, Name: "X-API-Key", IsRequired: true, IsSecret: true, Value: conv.ToPGText("synthetic-encrypted-value")})
	require.NoError(t, err)
	deleted, err := remoterepo.New(f.db).DeleteServerHeader(t.Context(), remoterepo.DeleteServerHeaderParams{ID: header.ID, ProjectID: f.target.ProjectID})
	require.NoError(t, err)
	require.Equal(t, header.UpdatedAt, deleted.UpdatedAt)
	now := deleted.DeletedAt.Time.Add(time.Minute)
	h := &timedPromotionHealth{calls: []time.Time{deleted.DeletedAt.Time.Add(-24 * time.Hour), deleted.DeletedAt.Time.Add(-time.Hour)}}
	f.service.health = h
	f.service.now = func() time.Time { return now }
	candidate, err := f.service.GetCandidate(t.Context(), f.target)
	require.NoError(t, err)
	require.False(t, candidate.Eligible)
	require.Equal(t, "insufficient_evidence", candidate.Reason)
	require.Len(t, h.windows, 1)
	require.Equal(t, deleted.DeletedAt.Time, h.windows[0][0])
}

func TestPromotionSourceLocksFenceRemoteHeaderDeletion(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	source, err := repo.New(f.db).GetSource(t.Context(), repo.GetSourceParams{OrganizationID: f.target.OrganizationID, ProjectID: f.target.ProjectID, McpServerID: f.target.MCPServerID})
	require.NoError(t, err)
	header, err := remoterepo.New(f.db).CreateServerHeader(t.Context(), remoterepo.CreateServerHeaderParams{ProjectID: f.target.ProjectID, RemoteMcpServerID: source.RemoteMcpServerID, Name: "X-API-Key", IsRequired: true, IsSecret: true, Value: conv.ToPGText("synthetic")})
	require.NoError(t, err)
	tx := testenv.BeginTx(t, t.Context(), f.db)
	_, err = lockSource(t.Context(), tx, f.target)
	require.NoError(t, err)
	probe := testenv.BeginTx(t, t.Context(), f.db)
	testenv.SetLockTimeout(t, t.Context(), probe, 50*time.Millisecond)
	_, err = remoterepo.New(probe).DeleteServerHeader(t.Context(), remoterepo.DeleteServerHeaderParams{ID: header.ID, ProjectID: f.target.ProjectID})
	testenv.RequireLockNotAvailable(t, err)
}
