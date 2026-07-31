package deviceintegrations

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/providers"
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/repo"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func newSyncTestEnv(t *testing.T) (context.Context, *pgxpool.Pool, *Store, *Syncer, string) {
	t.Helper()
	ctx, conn, store, orgID := newStoreTestDB(t)
	syncer := NewSyncer(testenv.NewLogger(t), testenv.NewMeterProvider(t), conn, testenv.NewEncryptionClient(t), guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)), nil)
	return ctx, conn, store, syncer, orgID
}

// newSyncMetricTestEnv is newSyncTestEnv wired to a manual-reader meter
// provider, so a test can assert on the counters RunSync emits.
func newSyncMetricTestEnv(t *testing.T) (context.Context, *pgxpool.Pool, *Store, *Syncer, string, *sdkmetric.ManualReader) {
	t.Helper()
	ctx, conn, store, orgID := newStoreTestDB(t)
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	syncer := NewSyncer(testenv.NewLogger(t), meterProvider, conn, testenv.NewEncryptionClient(t), guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)), nil)
	return ctx, conn, store, syncer, orgID, reader
}

// counterValue sums the int64 counter named `name` over the data points whose
// attributes match every key/value in `want`.
func counterValue(t *testing.T, ctx context.Context, reader *sdkmetric.ManualReader, name string, want map[attribute.Key]string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "metric %s is an int64 sum", name)
			for _, dp := range sum.DataPoints {
				if attributesMatch(dp.Attributes, want) {
					total += dp.Value
				}
			}
		}
	}
	return total
}

func attributesMatch(set attribute.Set, want map[attribute.Key]string) bool {
	for k, v := range want {
		got, ok := set.Value(k)
		if !ok || got.AsString() != v {
			return false
		}
	}
	return true
}

// findSyncID locates the org+provider's sync row through the same candidates
// query the coordinator uses. Call it while the sync is still runnable (before
// pausing/disabling) and keep the id.
func findSyncID(t *testing.T, ctx context.Context, store *Store, orgID string, providerID string) uuid.UUID {
	t.Helper()
	candidates, err := store.repo.ListSyncCandidates(ctx, repo.ListSyncCandidatesParams{
		LimitCount:     1000,
		ExcludeSyncIds: []uuid.UUID{},
	})
	require.NoError(t, err)
	for _, c := range candidates {
		if c.OrganizationID == orgID && c.Provider == providerID {
			return c.SyncID
		}
	}
	t.Fatalf("no sync candidate for org %s provider %s", orgID, providerID)
	return uuid.Nil
}

func listDevicesParams(orgID string) repo.ListManagedDevicesParams {
	return repo.ListManagedDevicesParams{
		DeviceLevel:    false,
		ActiveCutoff:   conv.ToPGTimestamptz(time.Now().UTC().Add(-activeWindow)),
		OrganizationID: orgID,
		Provider:       conv.PtrToPGTextEmpty(nil),
		CursorID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Bucket:         conv.PtrToPGTextEmpty(nil),
		PageLimit:      100,
	}
}

func devicesByBucket(t *testing.T, ctx context.Context, store *Store, orgID string, bucket string) []repo.ListManagedDevicesRow {
	t.Helper()
	params := listDevicesParams(orgID)
	params.Bucket = conv.PtrToPGTextEmpty(conv.PtrEmpty(bucket))
	rows, err := store.repo.ListManagedDevices(ctx, params)
	require.NoError(t, err)
	return rows
}

func scheduleStateFor(t *testing.T, ctx context.Context, store *Store, configID uuid.UUID, schedule string) repo.GetScheduleWithSyncRow {
	t.Helper()
	state, err := store.repo.GetScheduleWithSync(ctx, repo.GetScheduleWithSyncParams{
		DeviceIntegrationConfigID: configID,
		Schedule:                  schedule,
	})
	require.NoError(t, err)
	return state
}

func TestRunSyncInventoryHappyPath(t *testing.T) {
	t.Parallel()

	ctx, conn, store, syncer, orgID := newSyncTestEnv(t)
	memberID := seedUser(t, ctx, conn, "owner@example.test")
	require.NoError(t, testrepo.New(conn).CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{
		OrganizationID: orgID,
		UserID:         conv.ToPGText(memberID),
	}))

	created := mustUpsert(t, ctx, conn, store, orgID, validCreds(), providers.Settings{
		"instance_url": "https://example.test",
		"devices":      "dev-1=owner@example.test,dev-2",
	}, true)

	syncID := findSyncID(t, ctx, store, orgID, testProviderID)
	require.NoError(t, syncer.RunSync(ctx, syncID))

	rows, err := store.repo.ListManagedDevices(ctx, listDevicesParams(orgID))
	require.NoError(t, err)
	require.Len(t, rows, 2)

	byExternal := map[string]repo.ListManagedDevicesRow{}
	for _, row := range rows {
		byExternal[row.ExternalID] = row
	}
	require.Equal(t, "owner@example.test", byExternal["dev-1"].UserEmail.String)
	require.True(t, byExternal["dev-1"].UserID.Valid, "assigned email resolves to the org member")
	require.False(t, byExternal["dev-2"].UserID.Valid)

	state := scheduleStateFor(t, ctx, store, created.Config.ID, "testmdm_inventory")
	require.True(t, state.LastPollSuccessAt.Valid)
	require.False(t, state.LastPollFailedAt.Valid)
}

func TestRunSyncMarksMissingOnlyOnCompletedSnapshot(t *testing.T) {
	t.Parallel()

	ctx, conn, store, syncer, orgID := newSyncTestEnv(t)

	created := mustUpsert(t, ctx, conn, store, orgID, validCreds(), providers.Settings{
		"instance_url": "https://example.test",
		"devices":      "dev-1,dev-2",
	}, true)
	syncID := findSyncID(t, ctx, store, orgID, testProviderID)
	require.NoError(t, syncer.RunSync(ctx, syncID))

	// Shrink the fleet: dev-2 disappears from the vendor.
	mustUpsert(t, ctx, conn, store, orgID, nil, providers.Settings{"devices": "dev-1"}, true)
	require.NoError(t, syncer.RunSync(ctx, syncID))

	missing := devicesByBucket(t, ctx, store, orgID, "missing")
	require.Len(t, missing, 1)
	require.Equal(t, "dev-2", missing[0].ExternalID)

	// A failing pull must never widen the missing set: dev-1 was not visited
	// by the failed sync, but stays present.
	mustUpsert(t, ctx, conn, store, orgID, nil, providers.Settings{"devices": "dev-1", "fail": "boom"}, true)
	require.NoError(t, syncer.RunSync(ctx, syncID))

	missing = devicesByBucket(t, ctx, store, orgID, "missing")
	require.Len(t, missing, 1, "partial pulls never mark devices missing")

	state := scheduleStateFor(t, ctx, store, created.Config.ID, "testmdm_inventory")
	require.True(t, state.LastPollFailedAt.Valid, "failure recorded")
	require.Equal(t, int32(1), state.ConsecutiveFailures)
}

func TestRunSyncVendorDataErrorRecordsFailure(t *testing.T) {
	t.Parallel()

	ctx, conn, store, syncer, orgID := newSyncTestEnv(t)

	// A vendor record whose content the database itself rejects (jsonb
	// refuses the Unicode NUL escape). Deterministic vendor data, not an
	// infrastructure blip: RunSync must record a visible, backed-off
	// failure and return nil rather than hot-looping the Temporal retry.
	created := mustUpsert(t, ctx, conn, store, orgID, validCreds(), providers.Settings{
		"instance_url": "https://example.test",
		"devices":      "dev-1",
		"raw_json":     `{"name":"\u0000"}`,
	}, true)
	syncID := findSyncID(t, ctx, store, orgID, testProviderID)

	require.NoError(t, syncer.RunSync(ctx, syncID), "data rejection is a recorded failure, not an infra error")

	state := scheduleStateFor(t, ctx, store, created.Config.ID, "testmdm_inventory")
	require.True(t, state.LastPollFailedAt.Valid, "failure recorded")
	require.Equal(t, int32(1), state.ConsecutiveFailures)
	require.Contains(t, state.LastPollError.String, "upsert mdm device dev-1", "the error names the offending device")
	require.False(t, state.AutoPausedAt.Valid, "data errors are not credential rejections")
}

func TestRunSyncAuthFailuresAutoPause(t *testing.T) {
	t.Parallel()

	ctx, conn, store, syncer, orgID := newSyncTestEnv(t)

	created := mustUpsert(t, ctx, conn, store, orgID, validCreds(), providers.Settings{
		"instance_url": "https://example.test",
		"fail":         "auth",
	}, true)
	syncID := findSyncID(t, ctx, store, orgID, testProviderID)

	for range authPauseThreshold {
		require.NoError(t, syncer.RunSync(ctx, syncID))
	}

	state := scheduleStateFor(t, ctx, store, created.Config.ID, "testmdm_inventory")
	require.True(t, state.AutoPausedAt.Valid, "repeated credential rejections auto-pause the schedule")
	require.Equal(t, int32(authPauseThreshold), state.ConsecutiveFailures)
}

func TestRunSyncEmitsSuccessMetric(t *testing.T) {
	t.Parallel()

	ctx, conn, store, syncer, orgID, reader := newSyncMetricTestEnv(t)

	mustUpsert(t, ctx, conn, store, orgID, validCreds(), providers.Settings{
		"instance_url": "https://example.test",
		"devices":      "dev-1",
	}, true)
	syncID := findSyncID(t, ctx, store, orgID, testProviderID)
	require.NoError(t, syncer.RunSync(ctx, syncID))

	require.Equal(t, int64(1), counterValue(t, ctx, reader, meterSyncOutcome, map[attribute.Key]string{
		attr.ProviderKey: testProviderID,
		attr.OutcomeKey:  string(o11y.OutcomeSuccess),
	}), "a successful sync records one success outcome")
	require.Equal(t, int64(0), counterValue(t, ctx, reader, meterSyncOutcome, map[attribute.Key]string{
		attr.OutcomeKey: string(o11y.OutcomeFailure),
	}), "a successful sync records no failure")
	require.Equal(t, int64(0), counterValue(t, ctx, reader, meterSyncAutoPause, nil), "a successful sync never auto-pauses")
}

func TestRunSyncEmitsFailureAndAutoPauseMetrics(t *testing.T) {
	t.Parallel()

	ctx, conn, store, syncer, orgID, reader := newSyncMetricTestEnv(t)

	mustUpsert(t, ctx, conn, store, orgID, validCreds(), providers.Settings{
		"instance_url": "https://example.test",
		"fail":         "auth",
	}, true)
	syncID := findSyncID(t, ctx, store, orgID, testProviderID)

	// A pure auth-rejection streak: each run records a failure, and the run
	// that crosses the threshold records exactly one auto-pause. Once paused,
	// the schedule is no longer runnable, so a further run would record
	// nothing — bounding the loop at the threshold keeps the counts exact.
	for range authPauseThreshold {
		require.NoError(t, syncer.RunSync(ctx, syncID))
	}

	require.Equal(t, int64(authPauseThreshold), counterValue(t, ctx, reader, meterSyncOutcome, map[attribute.Key]string{
		attr.ProviderKey: testProviderID,
		attr.OutcomeKey:  string(o11y.OutcomeFailure),
	}), "each failed run records one failure outcome")
	require.Equal(t, int64(1), counterValue(t, ctx, reader, meterSyncAutoPause, map[attribute.Key]string{
		attr.ProviderKey: testProviderID,
	}), "only the threshold-crossing run records an auto-pause")
}

func TestRunSyncEvidencePushEmitsSuccessMetric(t *testing.T) {
	t.Parallel()

	ctx, conn, store, syncer, orgID, reader := newSyncMetricTestEnv(t)

	// A sink that accepts the push: the success outcome must be recorded for
	// the evidence-push capability too, tagged with the sink provider — not
	// only the inventory path.
	_, err := upsertProviderTx(t, ctx, conn, store, orgID, testSinkProviderID,
		providers.Credentials{"api_key": "sink-key"}, providers.Settings{}, true)
	require.NoError(t, err)
	syncID := findSyncID(t, ctx, store, orgID, testSinkProviderID)
	require.NoError(t, syncer.RunSync(ctx, syncID))

	require.Equal(t, int64(1), counterValue(t, ctx, reader, meterSyncOutcome, map[attribute.Key]string{
		attr.ProviderKey: testSinkProviderID,
		attr.OutcomeKey:  string(o11y.OutcomeSuccess),
	}), "a successful evidence push records one success outcome for the sink provider")
}

func TestRunSyncSuccessClearsFailureState(t *testing.T) {
	t.Parallel()

	ctx, conn, store, syncer, orgID := newSyncTestEnv(t)

	created := mustUpsert(t, ctx, conn, store, orgID, validCreds(), providers.Settings{
		"instance_url": "https://example.test",
		"fail":         "boom",
	}, true)
	syncID := findSyncID(t, ctx, store, orgID, testProviderID)
	require.NoError(t, syncer.RunSync(ctx, syncID))

	// Fix the vendor; an explicit empty value clears the merged fail knob.
	// The next run must clear the failure fields so scheduleStatus renders
	// recovery as success.
	mustUpsert(t, ctx, conn, store, orgID, nil, providers.Settings{"devices": "dev-1", "fail": ""}, true)
	require.NoError(t, syncer.RunSync(ctx, syncID))

	state := scheduleStateFor(t, ctx, store, created.Config.ID, "testmdm_inventory")
	require.False(t, state.LastPollFailedAt.Valid, "success clears failure state by contract")
	require.False(t, state.LastPollError.Valid)
	require.Zero(t, state.ConsecutiveFailures)
	require.Equal(t, "success", scheduleStatus(stateFromGetRow(state)))
}

func TestRunSyncSkipsDisabledSchedule(t *testing.T) {
	t.Parallel()

	ctx, conn, store, syncer, orgID := newSyncTestEnv(t)

	created := mustUpsert(t, ctx, conn, store, orgID, validCreds(), providers.Settings{
		"instance_url": "https://example.test",
		"devices":      "dev-1",
	}, true)
	syncID := findSyncID(t, ctx, store, orgID, testProviderID)
	require.NoError(t, testrepo.New(conn).DisableDeviceIntegrationSchedulesFixture(ctx, created.Config.ID))

	require.NoError(t, syncer.RunSync(ctx, syncID))

	rows, err := store.repo.ListManagedDevices(ctx, listDevicesParams(orgID))
	require.NoError(t, err)
	require.Empty(t, rows, "user-disabled schedules do not run")
}

func TestRunEvidencePushDigestSkip(t *testing.T) {
	t.Parallel()

	ctx, conn, store, syncer, orgID := newSyncTestEnv(t)

	// A sink that rejects every push: any successful run below proves the
	// push was skipped.
	created, err := upsertProviderTx(t, ctx, conn, store, orgID, testSinkProviderID,
		providers.Credentials{"api_key": "sink-key"}, providers.Settings{"fail_push": "true"}, true)
	require.NoError(t, err)
	syncID := findSyncID(t, ctx, store, orgID, testSinkProviderID)

	// First run: digest unset, push attempted, sink rejects → failure.
	require.NoError(t, syncer.RunSync(ctx, syncID))
	state := scheduleStateFor(t, ctx, store, created.Config.ID, "testsink_evidence")
	require.True(t, state.LastPollFailedAt.Valid, "sink rejection records a failure")

	// Seed the stored digest to match current coverage (empty fleet), then
	// run again: the digest short-circuit means no push is attempted, so the
	// rejecting sink still yields success.
	snapshot, err := syncer.buildCoverageSnapshot(ctx, orgID, time.Now().UTC())
	require.NoError(t, err)
	require.NoError(t, testrepo.New(conn).SetDeviceIntegrationSyncPushDigestFixture(ctx, testrepo.SetDeviceIntegrationSyncPushDigestFixtureParams{
		DeviceIntegrationConfigID: created.Config.ID,
		LastPushDigest:            conv.ToPGText(coverageSnapshotDigest(snapshot)),
	}))

	require.NoError(t, syncer.RunSync(ctx, syncID))
	state = scheduleStateFor(t, ctx, store, created.Config.ID, "testsink_evidence")
	require.True(t, state.LastPollSuccessAt.Valid)
	require.False(t, state.LastPollFailedAt.Valid, "unchanged coverage skips the push entirely")
}

func TestStaleSyncOutcomeDiscardedAfterConfigSave(t *testing.T) {
	t.Parallel()

	ctx, conn, store, syncer, orgID := newSyncTestEnv(t)
	_ = syncer

	created := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	syncID := findSyncID(t, ctx, store, orgID, testProviderID)

	// Snapshot the target the way a running sync would, then save the config
	// (bumping updated_at) before the outcome lands.
	target, err := store.repo.GetSyncTarget(ctx, syncID)
	require.NoError(t, err)
	mustUpsert(t, ctx, conn, store, orgID, nil, providers.Settings{"note": "changed"}, true)

	// The stale config_updated_at matches no row, so the guarded update books
	// nothing and the :one query surfaces that as ErrNoRows.
	_, err = store.repo.RecordSyncFailure(ctx, repo.RecordSyncFailureParams{
		SyncID:          target.SyncID,
		NextInSeconds:   60,
		LastPollError:   conv.ToPGText("stale outcome"),
		PauseAfter:      0,
		ConfigUpdatedAt: target.ConfigUpdatedAt,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "a config save moved updated_at, so the outcome matches no row")

	state := scheduleStateFor(t, ctx, store, created.Config.ID, "testmdm_inventory")
	require.False(t, state.LastPollFailedAt.Valid, "an outcome observed before a config save must not land after it")
}

func TestRunSyncRecordsUndecryptableCredentials(t *testing.T) {
	t.Parallel()

	ctx, conn, store, syncer, orgID := newSyncTestEnv(t)

	created := mustUpsert(t, ctx, conn, store, orgID, validCreds(), providers.Settings{
		"instance_url": "https://example.test",
		"devices":      "dev-1",
	}, true)
	syncID := findSyncID(t, ctx, store, orgID, testProviderID)
	require.NoError(t, testrepo.New(conn).CorruptDeviceIntegrationCredentialsFixture(ctx, created.Config.ID))

	// A permanent decrypt failure must record a visible sync failure (with
	// backoff toward auto-pause), not hot-loop as a retryable infra error.
	require.NoError(t, syncer.RunSync(ctx, syncID))

	state := scheduleStateFor(t, ctx, store, created.Config.ID, "testmdm_inventory")
	require.True(t, state.LastPollFailedAt.Valid)
	require.Contains(t, state.LastPollError.String, "decrypted")
}

func TestMidPullConfigSaveAbortsInventoryMerge(t *testing.T) {
	t.Parallel()

	ctx, conn, store, syncer, orgID := newSyncTestEnv(t)

	mustUpsert(t, ctx, conn, store, orgID, validCreds(), providers.Settings{
		"instance_url": "https://example.test",
		"devices":      "dev-1",
	}, true)
	syncID := findSyncID(t, ctx, store, orgID, testProviderID)

	// Snapshot the target as a running sync would, then save the config —
	// the guarded upsert must refuse to merge the stale pull's devices.
	target, err := store.repo.GetSyncTarget(ctx, syncID)
	require.NoError(t, err)
	mustUpsert(t, ctx, conn, store, orgID, nil, providers.Settings{"note": "changed"}, true)

	rows, err := store.repo.UpsertMdmDevice(ctx, repo.UpsertMdmDeviceParams{
		DeviceIntegrationConfigID: target.ConfigID,
		OrganizationID:            target.OrganizationID,
		ExternalID:                "stale-device",
		SerialNumber:              conv.ToPGTextEmpty(""),
		Hostname:                  conv.ToPGTextEmpty(""),
		OsName:                    conv.ToPGTextEmpty(""),
		OsVersion:                 conv.ToPGTextEmpty(""),
		UserEmail:                 conv.ToPGTextEmpty(""),
		UserID:                    conv.ToPGTextEmpty(""),
		MdmLastCheckInAt:          conv.ToPGTimestamptz(time.Now().UTC()),
		Raw:                       []byte("{}"),
		ConfigUpdatedAt:           target.ConfigUpdatedAt,
	})
	require.NoError(t, err)
	require.Zero(t, rows, "stale-generation device writes are refused")

	// A fresh run (which re-reads the target) still works end to end.
	require.NoError(t, syncer.RunSync(ctx, syncID))
	devices, err := store.repo.ListManagedDevices(ctx, listDevicesParams(orgID))
	require.NoError(t, err)
	require.Len(t, devices, 1)
	require.Equal(t, "dev-1", devices[0].ExternalID)
}

func TestAutoPauseRequiresPureAuthRejectionStreak(t *testing.T) {
	t.Parallel()

	ctx, conn, store, syncer, orgID := newSyncTestEnv(t)
	_ = syncer

	mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	syncID := findSyncID(t, ctx, store, orgID, testProviderID)
	target, err := store.repo.GetSyncTarget(ctx, syncID)
	require.NoError(t, err)

	record := func(auth bool) {
		t.Helper()
		fresh, err := store.repo.GetSyncTarget(ctx, syncID)
		require.NoError(t, err)
		_, err = store.repo.RecordSyncFailure(ctx, repo.RecordSyncFailureParams{
			SyncID:          target.SyncID,
			NextInSeconds:   60,
			LastPollError:   conv.ToPGText("failure"),
			AuthRejection:   auth,
			PauseAfter:      authPauseThreshold,
			ConfigUpdatedAt: fresh.ConfigUpdatedAt,
		})
		require.NoError(t, err)
	}

	// Two transient failures then one auth rejection: the mixed streak is 3
	// but the AUTH streak is 1, so no pause.
	record(false)
	record(false)
	record(true)
	state, err := store.repo.GetSyncTarget(ctx, syncID)
	require.NoError(t, err)
	require.False(t, state.AutoPausedAt.Valid, "mixed failures must not reach the auth pause threshold")

	// Two more auth rejections complete a pure streak of three: pause.
	record(true)
	record(true)
	state, err = store.repo.GetSyncTarget(ctx, syncID)
	require.NoError(t, err)
	require.True(t, state.AutoPausedAt.Valid, "a pure auth-rejection streak pauses the schedule")

	// A non-auth failure would have reset the streak.
}

// Regression: EnsureSync must seed next_poll_after from the DATABASE clock.
// An app-clock timestamp compared against clock_timestamp() in
// ListSyncCandidates made fresh syncs invisible under app/database clock
// skew (observed as every sync test failing after VM clock drift).
func TestFreshConfigSyncIsImmediatelyDue(t *testing.T) {
	t.Parallel()

	ctx, conn, store, _, orgID := newSyncTestEnv(t)
	_ = mustUpsert(t, ctx, conn, store, orgID, validCreds(), providers.Settings{
		"instance_url": "https://example.test",
	}, true)

	// findSyncID lists through the same candidates query the coordinator
	// uses; a fresh config's sync must already be due.
	syncID := findSyncID(t, ctx, store, orgID, testProviderID)
	require.NotEqual(t, uuid.Nil, syncID)
}

// TestCoverageSnapshotAttestationIsPerDevice pins the reason attestation is a
// field rather than a mode: one push can legitimately carry both strengths,
// because a machine whose agent cannot report a serial stays user-attested
// even for an org on device-level matching.
func TestCoverageSnapshotAttestationIsPerDevice(t *testing.T) {
	t.Parallel()

	ctx, conn, store, syncer, orgID := newSyncTestEnv(t)
	cfg := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	now := time.Now().UTC()

	const email = "dev@example.test"
	userID := seedUser(t, ctx, conn, email)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "reports-serial", email, &userID, "SERIAL-NEW", false)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "legacy-agent", email, &userID, "SERIAL-OLD", false)

	seedAgentSync(t, ctx, conn, orgID, email, now)
	// Only the first machine's agent reports hardware identity.
	seedDeviceAgentSync(t, ctx, conn, orgID, "SERIAL-NEW", email, now)

	snapshot, err := syncer.buildCoverageSnapshotWithMode(ctx, orgID, now, true)
	require.NoError(t, err)
	require.Len(t, snapshot.Devices, 2)

	byID := map[string]providers.CoverageDevice{}
	for _, d := range snapshot.Devices {
		byID[d.ExternalID] = d
	}
	require.Equal(t, providers.AttestationDevice, byID["reports-serial"].AgentAttestation)
	require.True(t, byID["reports-serial"].AgentActive)
	require.Equal(t, providers.AttestationUser, byID["legacy-agent"].AgentAttestation,
		"no serial heartbeat means the weaker claim, in the same push as the stronger one")
	require.True(t, byID["legacy-agent"].AgentActive)

	// The digest must distinguish the two, or a device moving from user- to
	// device-attested would be short-circuited as "no change".
	userOnly, err := syncer.buildCoverageSnapshotWithMode(ctx, orgID, now, false)
	require.NoError(t, err)
	require.Len(t, userOnly.Devices, 2,
		"both snapshots must carry the same devices, so the digests can only differ on attestation")
	for _, d := range userOnly.Devices {
		require.Equal(t, providers.AttestationUser, d.AgentAttestation)
		require.True(t, d.AgentActive, "user-level matching still covers both devices")
	}
	require.NotEqual(t, coverageSnapshotDigest(snapshot), coverageSnapshotDigest(userOnly),
		"attestation is push-relevant content and must be digested")
}
