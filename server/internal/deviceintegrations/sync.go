package deviceintegrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/providers"
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	// authPauseThreshold is how many consecutive credential rejections
	// auto-pause a schedule. Transient failures never pause.
	authPauseThreshold = 3

	// failureBackoffBase is the first retry delay after a failed sync; the
	// delay doubles per consecutive failure, capped at the schedule's
	// interval so a flapping vendor never waits longer than a healthy one.
	failureBackoffBase = 5 * time.Minute

	// syncMaxPages bounds one inventory pull so a buggy or hostile vendor
	// pagination cursor cannot loop a sync forever. At page-size ~100 this
	// still covers six-figure fleets.
	syncMaxPages = 2000

	// vendorCallTimeout bounds each individual vendor request (one inventory
	// page, one evidence push). guardian's client has a dial timeout but no
	// HTTP timeout, so without this a connected-but-stalled vendor response
	// would consume the whole activity slot.
	vendorCallTimeout = 5 * time.Minute
)

// infraError marks a failure of our own infrastructure (the database)
// rather than the vendor. RunSync propagates these to Temporal for retry
// instead of recording them as sync state — a DB blip is not a vendor
// outage. Deterministic failures (undecryptable credentials, unreadable
// settings) are deliberately NOT infra: retrying cannot fix them, so they
// are recorded as visible sync failures instead.
type infraError struct {
	err error
}

func (e *infraError) Error() string { return e.err.Error() }
func (e *infraError) Unwrap() error { return e.err }

func asInfra(err error) error {
	if err == nil {
		return nil
	}
	return &infraError{err: err}
}

func isInfra(err error) bool {
	var ie *infraError
	return errors.As(err, &ie)
}

// isVendorDataError reports whether the database rejected the row CONTENT
// rather than failing operationally: SQLSTATE class 22 (data exception),
// e.g. jsonb refusing a Unicode NUL escape inside a vendor-supplied record.
// Retrying cannot succeed until the vendor data changes, so these must
// surface as visible, backed-off schedule failures — classifying them as
// infra would hot-loop the Temporal retry with nothing on the dashboard.
func isVendorDataError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && strings.HasPrefix(pgErr.Code, "22")
}

// SyncCandidate is one due schedule the coordinator should run.
type SyncCandidate struct {
	SyncID           uuid.UUID
	OrganizationID   string
	OrganizationSlug string
	Provider         string
	Schedule         string
}

// Syncer executes device integration syncs: inventory pulls that reconcile
// mdm_devices and evidence pushes that deliver coverage snapshots. It is the
// only component that decrypts stored credentials, and it does so inside the
// running activity — Temporal payloads carry sync ids only.
type Syncer struct {
	logger   *slog.Logger
	db       *pgxpool.Pool
	repo     *repo.Queries
	store    *Store
	guardian *guardian.Policy
	features feature.Provider
	metrics  *syncMetrics
}

func NewSyncer(
	logger *slog.Logger,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	enc *encryption.Client,
	guardianPolicy *guardian.Policy,
	features feature.Provider,
) *Syncer {
	componentLogger := logger.With(attr.SlogComponent("deviceintegrations.syncer"))
	return &Syncer{
		logger:   componentLogger,
		db:       db,
		repo:     repo.New(db),
		store:    NewStore(logger, db, enc),
		guardian: guardianPolicy,
		features: features,
		metrics:  newSyncMetrics(componentLogger, meterProvider),
	}
}

func (s *Syncer) deviceLevelCoverage(ctx context.Context, orgID string) bool {
	return deviceLevelCoverage(ctx, s.logger, s.db, s.features, orgID)
}

// ListCandidates returns due, runnable syncs. Due-ness is evaluated on the
// database clock inside the query, so scheduler time never mixes clock
// domains.
func (s *Syncer) ListCandidates(ctx context.Context, limit int32, excludeSyncIDs []uuid.UUID) ([]SyncCandidate, error) {
	if excludeSyncIDs == nil {
		excludeSyncIDs = []uuid.UUID{}
	}
	rows, err := s.repo.ListSyncCandidates(ctx, repo.ListSyncCandidatesParams{
		LimitCount:     limit,
		ExcludeSyncIds: excludeSyncIDs,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list device integration sync candidates")
	}
	candidates := make([]SyncCandidate, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, SyncCandidate{
			SyncID:           row.SyncID,
			OrganizationID:   row.OrganizationID,
			OrganizationSlug: row.OrganizationSlug,
			Provider:         row.Provider,
			Schedule:         row.Schedule,
		})
	}
	return candidates, nil
}

// RunSync executes one sync end to end and records its outcome on the sync
// row. Business failures (vendor errors, bad credentials) are recorded as
// sync state and return nil — Temporal retries are reserved for
// infrastructure errors (database unavailable), so a vendor outage cannot
// double-record failures through the activity retry policy.
func (s *Syncer) RunSync(ctx context.Context, syncID uuid.UUID) error {
	target, err := s.repo.GetSyncTarget(ctx, syncID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The config was deleted between candidate selection and now.
			return nil
		}
		return oops.E(oops.CodeUnexpected, err, "load device integration sync target")
	}
	logger := s.logger.With(
		attr.SlogOrganizationID(target.OrganizationID),
		attr.SlogComponent("deviceintegrations.sync."+target.Schedule),
	)

	// Re-check runnability: the coordinator's candidate read races user
	// actions, and a skipped run is cheaper than a surprising one.
	if target.Deleted || !target.Enabled || target.DisabledAt.Valid || target.AutoPausedAt.Valid {
		return nil
	}

	desc, ok := providers.Lookup(target.Provider)
	if !ok {
		// A provider can disappear across deploys; surface it on the
		// schedule rather than erroring the workflow forever.
		return s.recordFailure(ctx, target, fmt.Sprintf("provider %q is not registered", target.Provider), false)
	}

	// Decrypt/decode failures are deterministic — retrying cannot help — so
	// they are recorded as sync failures (with backoff and eventual pause)
	// rather than propagated: a silently hot-looping schedule helps nobody.
	creds, err := s.store.decryptCredentials(target.CredentialsEncrypted)
	if err != nil {
		return s.recordFailure(ctx, target, "stored credentials can no longer be decrypted; re-enter them to reconnect", true)
	}
	settings := providers.Settings{}
	if len(target.Settings) > 0 {
		if err := json.Unmarshal(target.Settings, &settings); err != nil {
			// Deterministic but not a credential rejection: retried hourly
			// with a visible error, never auto-paused.
			return s.recordFailure(ctx, target, "stored settings are unreadable; save the integration again", false)
		}
	}

	deps := providers.Deps{Client: boundedProviderClient(s.guardian)}
	// The mark-missing cutoff must live on the same clock that stamps
	// mdm_devices.last_seen_at (Postgres), or host clock skew mis-marks
	// devices observed by this very pull.
	startedRow, err := s.repo.DBNow(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "read database clock")
	}
	started := startedRow.Time.UTC()

	// Dispatch on the FIRED schedule's declared capability, never on the
	// provider's capability set: a leftover schedule row (registry drift) or
	// a multi-capability provider must run exactly the pipeline its schedule
	// names.
	var spec *providers.ScheduleSpec
	for i := range desc.Schedules {
		if desc.Schedules[i].Schedule == target.Schedule {
			spec = &desc.Schedules[i]
			break
		}
	}
	if spec == nil {
		return s.recordFailure(ctx, target, fmt.Sprintf("schedule %q is no longer declared by provider %q", target.Schedule, desc.ID), false)
	}

	var runErr error
	switch {
	case spec.Capability == providers.CapabilityInventorySource && desc.NewInventorySource != nil:
		runErr = s.runInventorySync(ctx, target, desc.NewInventorySource(deps), creds, settings, started)
	case spec.Capability == providers.CapabilityEvidenceSink && desc.NewEvidenceSink != nil:
		runErr = s.runEvidencePush(ctx, target, desc.NewEvidenceSink(deps), creds, settings, started)
	default:
		runErr = fmt.Errorf("provider %q cannot run schedule %q (capability %q)", desc.ID, spec.Schedule, spec.Capability)
	}
	if runErr != nil {
		if errors.Is(runErr, errStaleSync) {
			// The config was saved while this sync ran; its work is obsolete
			// and the reset schedule takes over. Nothing to record.
			return nil
		}
		if isInfra(runErr) {
			// Our own infrastructure failed; let Temporal retry rather than
			// recording a vendor failure that never happened.
			return runErr
		}
		message := sanitizeSyncError(runErr.Error(), creds)
		logger.WarnContext(ctx, "device integration sync failed", attr.SlogError(errors.New(message)))
		return s.recordFailure(ctx, target, message, providers.IsAuthError(runErr))
	}
	// Reaching here means the switch returned nil: a genuine sync success
	// (including an evidence push short-circuited by an unchanged digest). The
	// no-op and stale paths above all return before this point.
	s.metrics.recordOutcome(ctx, target.Provider, o11y.OutcomeSuccess)
	return nil
}

// errStaleSync marks an outcome write rejected by the config updated_at
// guard: the config was saved while this sync ran, so every side effect of
// the finalize transaction must roll back with it.
var errStaleSync = errors.New("sync outcome superseded by a config save")

// runInventorySync pulls the vendor's full device inventory, upserting each
// page, then — in one transaction — marks unvisited devices missing and
// records the completed snapshot. INVARIANT: a pull that fails mid-way has
// updated the devices it saw but never runs the mark-missing step, so a
// transient vendor error cannot report half the fleet as missing.
func (s *Syncer) runInventorySync(ctx context.Context, target repo.GetSyncTargetRow, source providers.InventorySource, creds providers.Credentials, settings providers.Settings, started time.Time) error {
	cursor := ""
	memberCache := map[string]pgtype.Text{}
	for page := 0; ; page++ {
		if page >= syncMaxPages {
			return fmt.Errorf("inventory listing exceeded %d pages without completing", syncMaxPages)
		}
		pageCtx, cancelPage := context.WithTimeout(ctx, vendorCallTimeout)
		devicePage, err := source.ListDevices(pageCtx, creds, settings, cursor)
		cancelPage()
		if err != nil {
			return fmt.Errorf("list devices: %w", err)
		}
		for _, device := range devicePage.Devices {
			if device.ExternalID == "" {
				continue
			}
			userID, err := s.resolveMember(ctx, memberCache, target.OrganizationID, device.UserEmail)
			if err != nil {
				if isVendorDataError(err) {
					return fmt.Errorf("resolve member for device %s: %w", device.ExternalID, err)
				}
				return asInfra(err)
			}
			raw := device.Raw
			if len(raw) == 0 {
				raw = []byte("{}")
			}
			var checkIn pgtype.Timestamptz
			if !device.LastCheckInAt.IsZero() {
				checkIn = conv.ToPGTimestamptz(device.LastCheckInAt)
			}
			rows, err := s.repo.UpsertMdmDevice(ctx, repo.UpsertMdmDeviceParams{
				DeviceIntegrationConfigID: target.ConfigID,
				OrganizationID:            target.OrganizationID,
				ExternalID:                device.ExternalID,
				SerialNumber:              conv.ToPGTextEmpty(device.SerialNumber),
				Hostname:                  conv.ToPGTextEmpty(device.Hostname),
				OsName:                    conv.ToPGTextEmpty(device.OSName),
				OsVersion:                 conv.ToPGTextEmpty(device.OSVersion),
				UserEmail:                 conv.ToPGTextEmpty(device.UserEmail),
				UserID:                    userID,
				MdmLastCheckInAt:          checkIn,
				Raw:                       raw,
				ConfigUpdatedAt:           target.ConfigUpdatedAt,
			})
			if err != nil {
				if isVendorDataError(err) {
					return fmt.Errorf("upsert mdm device %s: %w", device.ExternalID, err)
				}
				return asInfra(oops.E(oops.CodeUnexpected, err, "upsert mdm device"))
			}
			if rows == 0 {
				// The config was saved mid-pull: this run's inventory came
				// from the pre-save credentials/settings and must not merge
				// into the new config. Abort; the reset schedule re-runs
				// promptly with the new configuration.
				return errStaleSync
			}
		}
		cursor = devicePage.NextCursor
		if cursor == "" {
			break
		}
	}

	// The snapshot completed: mark the stragglers missing and record success
	// atomically, so coverage never observes a half-marked fleet.
	if err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if err := q.MarkDevicesMissing(ctx, repo.MarkDevicesMissingParams{
			DeviceIntegrationConfigID: target.ConfigID,
			SyncStartedAt:             conv.ToPGTimestamptz(started),
		}); err != nil {
			return fmt.Errorf("mark missing devices: %w", err)
		}
		rows, err := q.RecordSyncSuccess(ctx, repo.RecordSyncSuccessParams{
			SyncID:          target.SyncID,
			NextInSeconds:   scheduleIntervalSeconds(target),
			PollWatermarkAt: conv.ToPGTimestamptz(started),
			LastPushDigest:  pgtype.Text{String: "", Valid: false},
			ConfigUpdatedAt: target.ConfigUpdatedAt,
		})
		if err != nil {
			return fmt.Errorf("record sync success: %w", err)
		}
		if rows == 0 {
			// The config was saved mid-run. Returning an error rolls back the
			// whole transaction INCLUDING MarkDevicesMissing — a stale
			// snapshot must not mark devices the post-save world may still
			// have.
			return errStaleSync
		}
		return nil
	}); err != nil {
		if errors.Is(err, errStaleSync) {
			return errStaleSync
		}
		return asInfra(oops.E(oops.CodeUnexpected, err, "finalize inventory snapshot"))
	}
	return nil
}

// runEvidencePush builds the org's coverage snapshot and delivers it to the
// sink, unless the snapshot digest matches the last successful push — an
// unchanged fleet is a free no-op.
func (s *Syncer) runEvidencePush(ctx context.Context, target repo.GetSyncTargetRow, sink providers.EvidenceSink, creds providers.Credentials, settings providers.Settings, started time.Time) error {
	snapshot, err := s.buildCoverageSnapshot(ctx, target.OrganizationID, started)
	if err != nil {
		return asInfra(err)
	}
	digest := coverageSnapshotDigest(snapshot)

	// The watermark is an inventory concept (the mark-missing cutoff), so
	// sink schedules never advance it: pass NULL to keep the stored value.
	noWatermark := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}

	if target.LastPushDigest.Valid && target.LastPushDigest.String == digest {
		if _, err := s.repo.RecordSyncSuccess(ctx, repo.RecordSyncSuccessParams{
			SyncID:          target.SyncID,
			NextInSeconds:   scheduleIntervalSeconds(target),
			PollWatermarkAt: noWatermark,
			LastPushDigest:  pgtype.Text{String: "", Valid: false},
			ConfigUpdatedAt: target.ConfigUpdatedAt,
		}); err != nil {
			return asInfra(fmt.Errorf("record sync success: %w", err))
		}
		return nil
	}

	// PushCoverage is contractually idempotent (snapshot-replace), so a
	// Temporal retry after a post-push bookkeeping failure re-delivers the
	// same snapshot harmlessly rather than duplicating records.
	pushCtx, cancelPush := context.WithTimeout(ctx, vendorCallTimeout)
	err = sink.PushCoverage(pushCtx, creds, settings, snapshot)
	cancelPush()
	if err != nil {
		return fmt.Errorf("push coverage: %w", err)
	}

	if _, err := s.repo.RecordSyncSuccess(ctx, repo.RecordSyncSuccessParams{
		SyncID:          target.SyncID,
		NextInSeconds:   scheduleIntervalSeconds(target),
		PollWatermarkAt: noWatermark,
		LastPushDigest:  conv.ToPGText(digest),
		ConfigUpdatedAt: target.ConfigUpdatedAt,
	}); err != nil {
		return asInfra(fmt.Errorf("record sync success: %w", err))
	}
	return nil
}

// buildCoverageSnapshot assembles the evidence set for one org. Each device
// carries its own attestation strength: a serial match proves that machine
// ran the agent, an email match proves only that its assigned user did
// somewhere, and the two can coexist in one snapshot.
func (s *Syncer) buildCoverageSnapshot(ctx context.Context, orgID string, generatedAt time.Time) (providers.CoverageSnapshot, error) {
	return s.buildCoverageSnapshotWithMode(ctx, orgID, generatedAt, s.deviceLevelCoverage(ctx, orgID))
}

// buildCoverageSnapshotWithMode takes the matching mode as an argument so the
// flag resolution stays a single call in buildCoverageSnapshot and tests can
// exercise both modes without a feature provider.
func (s *Syncer) buildCoverageSnapshotWithMode(ctx context.Context, orgID string, generatedAt time.Time, deviceLevel bool) (providers.CoverageSnapshot, error) {
	rows, err := s.repo.ListCoverageSnapshotDevices(ctx, repo.ListCoverageSnapshotDevicesParams{
		DeviceLevel:    deviceLevel,
		OrganizationID: orgID,
	})
	if err != nil {
		return providers.CoverageSnapshot{}, oops.E(oops.CodeUnexpected, err, "list coverage snapshot devices")
	}
	cutoff := generatedAt.Add(-activeWindow)
	devices := make([]providers.CoverageDevice, 0, len(rows))
	for _, row := range rows {
		var lastSeen time.Time
		if row.AgentLastSeenAt.Valid {
			lastSeen = row.AgentLastSeenAt.Time.UTC()
		}
		attestation := providers.AttestationUser
		// pgtype.Bool: the expression cannot actually be NULL (an AND with a
		// non-null boolean parameter), but an invalid value must read as the
		// weaker claim, never the stronger one.
		if row.DeviceAttested.Valid && row.DeviceAttested.Bool {
			attestation = providers.AttestationDevice
		}
		devices = append(devices, providers.CoverageDevice{
			ExternalID:   row.ExternalID,
			SerialNumber: row.SerialNumber.String,
			Hostname:     row.Hostname.String,
			UserEmail:    row.UserEmail.String,
			// Inclusive at the cutoff so pushed evidence agrees with the
			// dashboard coverage query (last_seen_at >= cutoff).
			AgentActive:      row.AgentLastSeenAt.Valid && !row.AgentLastSeenAt.Time.Before(cutoff),
			AgentAttestation: attestation,
			AgentLastSeenAt:  lastSeen,
		})
	}
	return providers.CoverageSnapshot{
		OrganizationID: orgID,
		GeneratedAt:    generatedAt,
		Devices:        devices,
	}, nil
}

// coverageSnapshotDigest hashes the push-relevant content of a snapshot.
// GeneratedAt and heartbeat timestamps are deliberately excluded — the digest
// answers "did the evidence change", and a heartbeat that merely got newer
// while staying active is not a change worth pushing.
func coverageSnapshotDigest(snapshot providers.CoverageSnapshot) string {
	type digestDevice struct {
		ExternalID   string `json:"external_id"`
		SerialNumber string `json:"serial_number"`
		Hostname     string `json:"hostname"`
		UserEmail    string `json:"user_email"`
		Active       bool   `json:"active"`
		// Attestation is digested: a device moving from user- to
		// device-attested changes the meaning of the evidence even when
		// Active did not, so the push must not be short-circuited.
		Attestation string `json:"attestation"`
	}
	devices := make([]digestDevice, 0, len(snapshot.Devices))
	for _, d := range snapshot.Devices {
		devices = append(devices, digestDevice{
			ExternalID:   d.ExternalID,
			SerialNumber: d.SerialNumber,
			Hostname:     d.Hostname,
			UserEmail:    d.UserEmail,
			Active:       d.AgentActive,
			Attestation:  string(d.AgentAttestation),
		})
	}
	doc, err := json.Marshal(devices)
	if err != nil {
		// Marshaling a slice of plain structs cannot fail; guard anyway so a
		// future field addition that can fail degrades to always-push.
		return ""
	}
	sum := sha256.Sum256(doc)
	return hex.EncodeToString(sum[:])
}

func (s *Syncer) resolveMember(ctx context.Context, cache map[string]pgtype.Text, orgID string, email string) (pgtype.Text, error) {
	if email == "" {
		return pgtype.Text{String: "", Valid: false}, nil
	}
	if cached, ok := cache[email]; ok {
		return cached, nil
	}
	id, err := s.repo.ResolveOrgMemberByEmail(ctx, repo.ResolveOrgMemberByEmailParams{
		OrganizationID: orgID,
		Email:          email,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			unresolved := pgtype.Text{String: "", Valid: false}
			cache[email] = unresolved
			return unresolved, nil
		}
		return pgtype.Text{String: "", Valid: false}, oops.E(oops.CodeUnexpected, err, "resolve org member by email")
	}
	resolved := conv.ToPGText(id)
	cache[email] = resolved
	return resolved, nil
}

// scheduleInterval returns the fired schedule's declared cadence; unknown
// schedules (registry drift) fall back to an hour.
func scheduleInterval(target repo.GetSyncTargetRow) time.Duration {
	if desc, ok := providers.Lookup(target.Provider); ok {
		for _, spec := range desc.Schedules {
			if spec.Schedule == target.Schedule {
				return spec.Interval
			}
		}
	}
	return time.Hour
}

// scheduleIntervalSeconds feeds the SQL-side next_poll_after arithmetic —
// the database clock owns all scheduler time, so Go passes durations, never
// timestamps.
func scheduleIntervalSeconds(target repo.GetSyncTargetRow) int32 {
	return int32(scheduleInterval(target) / time.Second) //nolint:gosec // schedule intervals are minutes-to-hours scale
}

// recordFailure books a failed run: backoff doubling per consecutive
// failure (capped at the schedule interval), the sanitized error for the
// dashboard, and the auto-pause threshold for credential rejections.
func (s *Syncer) recordFailure(ctx context.Context, target repo.GetSyncTargetRow, message string, authRejection bool) error {
	backoff := failureBackoffBase << uint(min(target.ConsecutiveFailures, 6)) //nolint:gosec // bounded by min above
	if interval := scheduleInterval(target); backoff > interval {
		backoff = interval
	}
	paused, err := s.repo.RecordSyncFailure(ctx, repo.RecordSyncFailureParams{
		SyncID:          target.SyncID,
		NextInSeconds:   int32(backoff / time.Second), //nolint:gosec // backoff is bounded by the schedule interval
		LastPollError:   conv.ToPGText(message),
		AuthRejection:   authRejection,
		PauseAfter:      authPauseThreshold,
		ConfigUpdatedAt: target.ConfigUpdatedAt,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// A config save moved updated_at while this sync ran: nothing was
			// booked, so there is no outcome to record. The reset schedule
			// re-runs under the new config.
			return nil
		}
		return oops.E(oops.CodeUnexpected, err, "record device integration sync failure")
	}
	s.metrics.recordOutcome(ctx, target.Provider, o11y.OutcomeFailure)
	if paused {
		s.metrics.recordAutoPause(ctx, target.Provider)
	}
	return nil
}
