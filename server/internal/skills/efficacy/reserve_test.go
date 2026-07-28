package efficacy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	hooksrepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

// featureQuery is one entitlement question the reservation asked.
type featureQuery struct {
	organizationID string
	feature        productfeatures.Feature
}

// stubFeatures answers the entitlement check with a pinned verdict and records
// what it was asked, so a test can assert the reservation asks about its own
// organization and the skills feature.
type stubFeatures struct {
	mu      sync.Mutex
	enabled bool
	err     error
	queries []featureQuery
}

func (s *stubFeatures) IsFeatureEnabled(_ context.Context, organizationID string, feature productfeatures.Feature) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.queries = append(s.queries, featureQuery{organizationID: organizationID, feature: feature})
	if s.err != nil {
		return false, s.err
	}

	return s.enabled, nil
}

func (s *stubFeatures) asked(t *testing.T) []featureQuery {
	t.Helper()

	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.queries)
}

// seedBatchSize is large enough to drain every fixture in one reconcile or
// enqueue pass, so seeding never leaves work behind that a test would then
// measure by accident.
const seedBatchSize int32 = MaxEnqueuePageSize

// capturedSkill is a skill fixture plus the raw hash its activations carry,
// which is what reconciliation resolves the version by.
type capturedSkill struct {
	name           string
	rawSha256      string
	skillVersionID uuid.UUID
}

func (f efficacyFixture) captureSkill(t *testing.T, name string) capturedSkill {
	t.Helper()

	content := "---\nname: " + name + "\ndescription: Efficacy skill " + name + ".\n---\n\nBody for " + name + "\n"
	captured, err := skills.CaptureSkillContent(t.Context(), f.db, f.projectID, content)
	require.NoError(t, err)

	hash := sha256.Sum256([]byte(content))

	return capturedSkill{name: name, rawSha256: hex.EncodeToString(hash[:]), skillVersionID: captured.SkillVersionID}
}

// seedUnits records count quiet sessions that each activated skill once, one
// minute apart starting at oldest, so the reservation order is deterministic.
func (f efficacyFixture) seedUnits(t *testing.T, skill capturedSkill, prefix string, count int, oldest time.Time) {
	t.Helper()
	ctx := t.Context()

	queries := hooksrepo.New(f.db)
	for i := range count {
		sessionID := prefix + "-" + strconv.Itoa(i)
		f.seedChat(t, sessionID, 1, 90*time.Minute)

		_, err := queries.InsertSkillObservation(ctx, hooksrepo.InsertSkillObservationParams{
			ProjectID:      f.projectID,
			IdempotencyKey: conv.ToPGText(uuid.NewString()),
			Provider:       "claude-code",
			UserID:         conv.ToPGTextEmpty(defaultActor.userID),
			UserEmail:      conv.ToPGTextEmpty(defaultActor.email),
			Hostname:       pgtype.Text{String: "", Valid: false},
			SessionID:      conv.ToPGText(sessionID),
			SkillName:      skill.name,
			Source:         pgtype.Text{String: "", Valid: false},
			SourceLevel:    conv.ToPGText("project"),
			SourcePath:     pgtype.Text{String: "", Valid: false},
			RawSha256:      conv.ToPGText(skill.rawSha256),
			SeenAt:         conv.ToPGTimestamptz(oldest.Add(time.Duration(i) * time.Minute)),
		})
		require.NoError(t, err)
	}
}

// enqueueSeeded turns every seeded activation into a pending evaluation and
// asserts the whole fixture landed, so a later count can only be the work of
// the reservation under test.
func (f efficacyFixture) enqueueSeeded(t *testing.T, units int) {
	t.Helper()
	ctx := t.Context()

	// A fixture may be wider than one reconcile or enqueue page, so both walks
	// are resumed until they report the queue drained.
	for {
		reconciled, err := skills.ReconcileSkillObservations(ctx, f.db, f.projectID, seedBatchSize)
		require.NoError(t, err)
		if !reconciled.HasMore {
			break
		}
	}

	confirmed := 0
	cursor := EnqueueCursor{SeenAt: time.Time{}, ID: uuid.Nil}
	for {
		result, err := EnqueuePage(ctx, f.db, &stubFeatures{enabled: true}, f.projectID, cursor, seedBatchSize)
		require.NoError(t, err)
		confirmed += result.Confirmed
		if result.Exhausted {
			break
		}

		cursor = result.NextCursor
	}

	require.Equal(t, units, confirmed)
	require.Len(t, f.pendingEvaluations(t), units)
}

// writeSettings replaces the organization's budget, so a test can pin the caps
// it exercises instead of leaning on the package defaults.
func (f efficacyFixture) writeSettings(t *testing.T, enabled bool, perSkillDailyCap, orgDailyCap, newVersionBurst int32) {
	t.Helper()

	stored, err := repo.New(f.db).UpsertSkillEfficacySettingsForProject(t.Context(), repo.UpsertSkillEfficacySettingsForProjectParams{
		Enabled:          enabled,
		PerSkillDailyCap: perSkillDailyCap,
		OrgDailyCap:      orgDailyCap,
		NewVersionBurst:  newVersionBurst,
		ProjectID:        f.projectID,
	})
	require.NoError(t, err)
	require.Equal(t, f.organizationID, stored.OrganizationID)
}

// reservedEvaluations reads the reserved rows back through the claim query,
// which is also the only way to see updated_at. A zero lease makes every row
// committed before the statement claimable.
// RETURNING order is unspecified, so the rows are sorted the way the claim
// selected them.
func (f efficacyFixture) reservedEvaluations(t *testing.T, batchSize int32) []repo.SkillEfficacyEvaluation {
	t.Helper()

	rows, err := repo.New(f.db).LoadReservedSkillEfficacyEvaluations(t.Context(), repo.LoadReservedSkillEfficacyEvaluationsParams{
		ProjectID:     f.projectID,
		ClaimToken:    uuid.New(),
		ClaimLease:    pgtype.Interval{Microseconds: 0, Days: 0, Months: 0, Valid: true},
		RecoveryAfter: pgtype.Interval{Microseconds: StaleReservationAfter.Microseconds(), Days: 0, Months: 0, Valid: true},
		BatchSize:     batchSize,
	})
	require.NoError(t, err)

	slices.SortFunc(rows, func(a, b repo.SkillEfficacyEvaluation) int {
		if order := b.ObservedAt.Time.Compare(a.ObservedAt.Time); order != 0 {
			return order
		}

		return bytes.Compare(b.ID[:], a.ID[:])
	})

	return rows
}

func TestReserveTakesNewestCandidatesAndStampsTheUTCDay(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_reserve_recent_first")

	skill := fixture.captureSkill(t, "efficacy-recent")
	oldest := time.Now().UTC().Add(-5 * time.Hour)
	fixture.seedUnits(t, skill, "recent-session", 3, oldest)
	fixture.enqueueSeeded(t, 3)

	reserved, _, err := Reserve(t.Context(), fixture.db, &stubFeatures{enabled: true}, fixture.projectID, PendingCursor{}, 2)
	require.NoError(t, err)
	require.Len(t, reserved, 2)
	require.Equal(t, "recent-session-2", reserved[0].SessionID, "candidates are walked recent-first")
	require.Equal(t, "recent-session-1", reserved[1].SessionID)
	require.Equal(t, StateReserved, reserved[0].State)

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	stored := fixture.reservedEvaluations(t, 10)
	require.Len(t, stored, 2)
	for _, row := range stored {
		require.Equal(t, StateReserved, row.State)
		require.True(t, row.ReservedOn.Valid)
		require.Equal(t, today, row.ReservedOn.Time.UTC(), "reserved_on is the UTC day")
	}

	// The oldest candidate was never admitted, so it stays available.
	pending := fixture.pendingEvaluations(t)
	require.Len(t, pending, 1)
	require.Equal(t, "recent-session-0", pending[0].SessionID)
}

func TestReserveBoundsOneBatchByDailyAndBurstSlots(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_reserve_burst_bound")

	// Caps are pinned below the batch ceiling so the batch is bounded by the slot
	// math rather than by MaxReservedClaimBatch.
	const perSkillDailyCap, newVersionBurst = 2, 3
	const slots = newVersionBurst
	fixture.writeSettings(t, true, perSkillDailyCap, 10, newVersionBurst)

	skill := fixture.captureSkill(t, "efficacy-burst")
	// Burst evaluations bypass the daily cap but still count toward its spend, so
	// the first batch is bounded by the version burst rather than adding both caps.
	const candidates = 8
	fixture.seedUnits(t, skill, "burst-session", candidates, time.Now().UTC().Add(-48*time.Hour))
	fixture.enqueueSeeded(t, candidates)

	reserved, _, err := Reserve(t.Context(), fixture.db, &stubFeatures{enabled: true}, fixture.projectID, PendingCursor{}, MaxReservedClaimBatch)
	require.NoError(t, err)
	require.Len(t, reserved, slots, "burst evaluations count toward the skill's daily spend")
	require.Len(t, fixture.pendingEvaluations(t), candidates-slots)

	// The spend the first batch committed is what the next one reads, so the
	// The version cannot overshoot its lifetime burst across batches, and the
	// daily cap was already consumed by that burst spend.
	second, _, err := Reserve(t.Context(), fixture.db, &stubFeatures{enabled: true}, fixture.projectID, PendingCursor{}, MaxReservedClaimBatch)
	require.NoError(t, err)
	require.Empty(t, second)
}

func TestReserveWalksPastACappedSkillToReachTheOnesBehindIt(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_reserve_no_starvation")

	// One daily slot per skill and no burst, so a skill is capped after a single
	// admission and everything else it has queued is dead weight the walk has to
	// step over.
	fixture.writeSettings(t, true, 1, 10, 0)

	// The hot skill fills a whole candidate page on its own, so the cold skill is
	// only reachable by paging past it.
	const hotUnits = int(PendingCandidatePage)
	const coldUnits = 3
	cold := fixture.captureSkill(t, "efficacy-cold")
	oldest := time.Now().UTC().Add(-72 * time.Hour)
	fixture.seedUnits(t, cold, "cold-session", coldUnits, oldest)

	hot := fixture.captureSkill(t, "efficacy-hot")
	fixture.seedUnits(t, hot, "hot-session", hotUnits, oldest.Add(time.Hour))
	fixture.enqueueSeeded(t, hotUnits+coldUnits)

	reserved, _, err := Reserve(t.Context(), fixture.db, &stubFeatures{enabled: true}, fixture.projectID, PendingCursor{}, MaxReservedClaimBatch)
	require.NoError(t, err)
	require.Len(t, reserved, 2, "each skill takes its one daily slot in the same pass")
	require.Equal(t, "hot-session-"+strconv.Itoa(hotUnits-1), reserved[0].SessionID, "admissible candidates are still walked recent-first")
	require.Equal(t, "cold-session-"+strconv.Itoa(coldUnits-1), reserved[1].SessionID, "the cold skill is reached despite a full page of capped candidates ahead of it")
	require.Len(t, fixture.pendingEvaluations(t), hotUnits+coldUnits-2)

	// Both skills are spent for the day, so paging to exhaustion admits nothing
	// rather than re-spending what the first pass took.
	second, _, err := Reserve(t.Context(), fixture.db, &stubFeatures{enabled: true}, fixture.projectID, PendingCursor{}, MaxReservedClaimBatch)
	require.NoError(t, err)
	require.Empty(t, second)
}

func TestReserveResumesFromTheCursorItIsGivenAndStartsOverAtTheEnd(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fixture := newEfficacyFixture(t, "skill_efficacy_reserve_cursor")

	skill := fixture.captureSkill(t, "efficacy-cursor")
	fixture.seedUnits(t, skill, "cursor-session", 3, time.Now().UTC().Add(-5*time.Hour))
	fixture.enqueueSeeded(t, 3)

	queued := fixture.pendingEvaluations(t)
	require.Len(t, queued, 3)
	newest := queued[0]

	// A walk that starts past the newest candidate reserves the one behind it and
	// reports only that examined row, not the unexamined tail of the fetched page.
	resumed, next, err := Reserve(ctx, fixture.db, &stubFeatures{enabled: true}, fixture.projectID,
		PendingCursor{ObservedAt: newest.ObservedAt.Time, ID: newest.ID}, 1)
	require.NoError(t, err)
	require.Len(t, resumed, 1)
	require.Equal(t, queued[1].ID, resumed[0].ID, "a resumed walk starts strictly after the cursor it is given")
	require.Equal(t, PendingCursor{ObservedAt: queued[1].ObservedAt.Time, ID: queued[1].ID}, next)

	tail, next, err := Reserve(ctx, fixture.db, &stubFeatures{enabled: true}, fixture.projectID, next, 1)
	require.NoError(t, err)
	require.Len(t, tail, 1)
	require.Equal(t, queued[2].ID, tail[0].ID, "the unexamined page tail is next")
	require.Equal(t, PendingCursor{}, next, "a fully examined short page reports the head")

	// A cursor past the tail is the case that would wedge a walk forever if it
	// were kept: nothing is left after it, so the pass reports the head and the
	// next one reaches the candidates the cursor had skipped.
	oldest := queued[len(queued)-1]
	none, next, err := Reserve(ctx, fixture.db, &stubFeatures{enabled: true}, fixture.projectID,
		PendingCursor{ObservedAt: oldest.ObservedAt.Time, ID: oldest.ID}, MaxReservedClaimBatch)
	require.NoError(t, err)
	require.Empty(t, none)
	require.Equal(t, PendingCursor{}, next)

	rest, next, err := Reserve(ctx, fixture.db, &stubFeatures{enabled: true}, fixture.projectID, next, MaxReservedClaimBatch)
	require.NoError(t, err)
	require.Len(t, rest, 1, "the walk starts over and reaches what the original cursor skipped")
	require.Equal(t, PendingCursor{}, next)
	require.Empty(t, fixture.pendingEvaluations(t), "no candidate is left behind a cursor")
}

func TestReserveCursorStopsAtLastExaminedCandidateWhenBatchFillsMidPage(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "efficacy_reserve_mid_page_cursor")

	skill := fixture.captureSkill(t, "efficacy-mid-page")
	fixture.seedUnits(t, skill, "mid-page-session", int(PendingCandidatePage), time.Now().UTC().Add(-5*time.Hour))
	fixture.enqueueSeeded(t, int(PendingCandidatePage))
	queued := fixture.pendingEvaluations(t)
	require.Len(t, queued, int(PendingCandidatePage))

	const batchSize int32 = 10
	reserved, next, err := Reserve(t.Context(), fixture.db, &stubFeatures{enabled: true}, fixture.projectID, PendingCursor{}, batchSize)
	require.NoError(t, err)
	require.Len(t, reserved, int(batchSize))
	require.Equal(t, PendingCursor{ObservedAt: queued[batchSize-1].ObservedAt.Time, ID: queued[batchSize-1].ID}, next)
}

func TestReserveConcurrentReserversCannotExceedRemainingBurst(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_reserve_concurrent")

	// Caps are pinned small so the last slot is reached by arithmetic rather than
	// by the size of the fixture, which keeps the race window identical on every
	// run.
	fixture.writeSettings(t, true, 1, 10, 3)

	skill := fixture.captureSkill(t, "efficacy-concurrent")
	const candidates = 6
	fixture.seedUnits(t, skill, "concurrent-session", candidates, time.Now().UTC().Add(-48*time.Hour))
	fixture.enqueueSeeded(t, candidates)

	// Spend the version down to a single remaining burst slot: the one daily slot
	// is gone and two of the three lifetime burst slots are spent.
	const prespend = 2
	warmup, _, err := Reserve(t.Context(), fixture.db, &stubFeatures{enabled: true}, fixture.projectID, PendingCursor{}, prespend)
	require.NoError(t, err)
	require.Len(t, warmup, prespend)

	var group sync.WaitGroup
	counts := make([]int, 2)
	errs := make([]error, 2)
	for i := range counts {
		group.Go(func() {
			reserved, _, err := Reserve(t.Context(), fixture.db, &stubFeatures{enabled: true}, fixture.projectID, PendingCursor{}, candidates)
			counts[i] = len(reserved)
			errs[i] = err
		})
	}
	group.Wait()

	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, 1, counts[0]+counts[1], "the org advisory lock serialises both reservers over the last slot")
	require.Len(t, fixture.reservedEvaluations(t, seedBatchSize), prespend+1)
}

func TestReserveStopsAtTheOrganizationDailyCap(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_reserve_org_cap")

	// Every other grain is left with room to spare, so the organization's day is
	// the only thing that can stop the batch.
	const orgDailyCap = 3
	fixture.writeSettings(t, true, 5, orgDailyCap, 5)

	const perSkill = 3
	const units = perSkill * 2
	oldest := time.Now().UTC().Add(-72 * time.Hour)
	for index, name := range []string{"efficacy-org-a", "efficacy-org-b"} {
		skill := fixture.captureSkill(t, name)
		fixture.seedUnits(t, skill, name+"-session", perSkill, oldest.Add(time.Duration(index)*time.Hour))
	}
	fixture.enqueueSeeded(t, units)

	reserved, _, err := Reserve(t.Context(), fixture.db, &stubFeatures{enabled: true}, fixture.projectID, PendingCursor{}, MaxReservedClaimBatch)
	require.NoError(t, err)
	require.Len(t, reserved, orgDailyCap)
	require.Len(t, fixture.pendingEvaluations(t), units-orgDailyCap)

	second, _, err := Reserve(t.Context(), fixture.db, &stubFeatures{enabled: true}, fixture.projectID, PendingCursor{}, MaxReservedClaimBatch)
	require.NoError(t, err)
	require.Empty(t, second, "the organization has spent its day")
}

func TestReserveHonoursDisabledOrganizationAndZeroCaps(t *testing.T) {
	t.Parallel()

	// A cap of 0 turns its grain off. The organization cap always binds, so a
	// zero there stops everything; a zero at a lower grain only removes that
	// grain's slots and leaves the other one to admit candidates.
	tests := []struct {
		name             string
		fixture          string
		enabled          bool
		perSkillDailyCap int32
		orgDailyCap      int32
		newVersionBurst  int32
		want             int
	}{
		{
			name:             "disabled organization reserves nothing",
			fixture:          "skill_efficacy_reserve_disabled",
			enabled:          false,
			perSkillDailyCap: DefaultPerSkillDailyCap,
			orgDailyCap:      DefaultOrgDailyCap,
			newVersionBurst:  DefaultNewVersionBurst,
			want:             0,
		},
		{
			name:             "zero org cap reserves nothing",
			fixture:          "skill_efficacy_reserve_zero_org_cap",
			enabled:          true,
			perSkillDailyCap: 5,
			orgDailyCap:      0,
			newVersionBurst:  5,
			want:             0,
		},
		{
			name:             "zero per skill cap leaves only burst slots",
			fixture:          "skill_efficacy_reserve_zero_skill_cap",
			enabled:          true,
			perSkillDailyCap: 0,
			orgDailyCap:      10,
			newVersionBurst:  3,
			want:             3,
		},
		{
			name:             "zero burst leaves only daily slots",
			fixture:          "skill_efficacy_reserve_zero_burst",
			enabled:          true,
			perSkillDailyCap: 2,
			orgDailyCap:      10,
			newVersionBurst:  0,
			want:             2,
		},
		{
			name:             "zero per skill cap and zero burst reserve nothing",
			fixture:          "skill_efficacy_reserve_zero_all_grains",
			enabled:          true,
			perSkillDailyCap: 0,
			orgDailyCap:      10,
			newVersionBurst:  0,
			want:             0,
		},
	}

	const candidates = 5
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fixture := newEfficacyFixture(t, tt.fixture)

			// Enqueued under the package defaults, so the settings this case pins
			// afterward are exercised as Reserve's own gate rather than tripping
			// the enqueue's identical admission check first.
			skill := fixture.captureSkill(t, "efficacy-caps")
			fixture.seedUnits(t, skill, "caps-session", candidates, time.Now().UTC().Add(-6*time.Hour))
			fixture.enqueueSeeded(t, candidates)

			fixture.writeSettings(t, tt.enabled, tt.perSkillDailyCap, tt.orgDailyCap, tt.newVersionBurst)

			reserved, _, err := Reserve(t.Context(), fixture.db, &stubFeatures{enabled: true}, fixture.projectID, PendingCursor{}, MaxReservedClaimBatch)
			require.NoError(t, err)
			require.Len(t, reserved, tt.want)
			require.Len(t, fixture.pendingEvaluations(t), candidates-tt.want, "candidates the caps refused stay pending")
		})
	}
}

func TestReserveRequiresTheSkillsProductFeature(t *testing.T) {
	t.Parallel()

	// The entitlement is a gate in front of the budget, so it decides the batch on
	// its own: caps are left wide open in every case below.
	tests := []struct {
		name     string
		fixture  string
		features *stubFeatures
		wantErr  bool
		want     int
	}{
		{
			name:     "entitled organization reserves its batch",
			fixture:  "skill_efficacy_reserve_feature_on",
			features: &stubFeatures{enabled: true},
			wantErr:  false,
			want:     3,
		},
		{
			name:     "unentitled organization reserves nothing",
			fixture:  "skill_efficacy_reserve_feature_off",
			features: &stubFeatures{enabled: false},
			wantErr:  false,
			want:     0,
		},
		{
			name:     "an unreadable entitlement reserves nothing and fails",
			fixture:  "skill_efficacy_reserve_feature_error",
			features: &stubFeatures{err: errors.New("product features unavailable")},
			wantErr:  true,
			want:     0,
		},
	}

	const candidates = 3
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fixture := newEfficacyFixture(t, tt.fixture)
			fixture.writeSettings(t, true, 10, 10, 10)

			skill := fixture.captureSkill(t, "efficacy-feature")
			fixture.seedUnits(t, skill, "feature-session", candidates, time.Now().UTC().Add(-6*time.Hour))
			fixture.enqueueSeeded(t, candidates)

			reserved, _, err := Reserve(t.Context(), fixture.db, tt.features, fixture.projectID, PendingCursor{}, MaxReservedClaimBatch)
			if tt.wantErr {
				require.Error(t, err, "an unreadable entitlement is infrastructure failure the caller retries")
			} else {
				require.NoError(t, err)
			}
			require.Len(t, reserved, tt.want)

			// A gated pass writes nothing at all: the candidates it refused are still
			// queued and no row was moved to reserved.
			require.Len(t, fixture.pendingEvaluations(t), candidates-tt.want)
			require.Len(t, fixture.reservedEvaluations(t, seedBatchSize), tt.want)

			require.Equal(t, []featureQuery{{organizationID: fixture.organizationID, feature: productfeatures.FeatureSkills}}, tt.features.asked(t))
		})
	}
}

func TestReserveOnADeletedProjectIsANoOp(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_reserve_deleted_project")

	// A coordinator can still be holding an id whose project went away between
	// passes. There is no organization to bill and no candidate to admit, so the
	// pass ends quietly rather than handing the caller a failure to retry.
	features := &stubFeatures{enabled: true}
	reserved, _, err := Reserve(t.Context(), fixture.db, features, uuid.New(), PendingCursor{}, MaxReservedClaimBatch)
	require.NoError(t, err)
	require.Empty(t, reserved)
	require.Empty(t, features.asked(t), "an unknown project names no organization to ask about")
}

// Enqueue admits only a live chat, but the queue outlives the chat: a session
// deleted after its unit was enqueued leaves a pending row whose subject is
// gone. Such a row is not a candidate, so it never spends a budget slot on a
// transcript the project asked to be rid of.
func TestReserveSkipsEvaluationsOfADeletedChat(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fixture := newEfficacyFixture(t, "skill_efficacy_reserve_deleted_chat")

	skill := fixture.captureSkill(t, "efficacy-deleted-chat")
	fixture.seedUnits(t, skill, "deleted-chat-session", 2, time.Now().UTC().Add(-5*time.Hour))
	fixture.enqueueSeeded(t, 2)

	deleted, err := chatrepo.New(fixture.db).SoftDeleteChat(ctx, chatrepo.SoftDeleteChatParams{
		ProjectID: fixture.projectID,
		ID:        chat.SessionIDToChatID("deleted-chat-session-1"),
	})
	require.NoError(t, err)
	require.True(t, deleted.Deleted)

	reserved, _, err := Reserve(ctx, fixture.db, &stubFeatures{enabled: true}, fixture.projectID, PendingCursor{}, MaxReservedClaimBatch)
	require.NoError(t, err)
	require.Len(t, reserved, 1, "only the live session is admitted")
	require.Equal(t, "deleted-chat-session-0", reserved[0].SessionID)

	require.Empty(t, fixture.pendingEvaluations(t), "the deleted chat's evaluation is no longer a candidate")
	projects, err := PendingWorkProjects(ctx, fixture.db, uuid.Nil, StaleReservationAfter, MaxSweepProjectPage)
	require.NoError(t, err)
	require.Empty(t, projects, "a deleted chat's pending row does not keep waking the project")
}

func TestRecoverStaleReservationsRejectsInvalidBounds(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_recover_invalid_bounds")

	for _, staleAfter := range []time.Duration{0, time.Nanosecond, -time.Second} {
		result, err := RecoverStaleReservations(t.Context(), fixture.db, fixture.projectID, staleAfter, 1)
		require.Error(t, err)
		require.Equal(t, RecoveryResult{}, result)
	}

	for _, batchSize := range []int32{-1, 0, MaxRecoveryBatch + 1} {
		result, err := RecoverStaleReservations(t.Context(), fixture.db, fixture.projectID, time.Hour, batchSize)
		require.Error(t, err)
		require.Equal(t, RecoveryResult{}, result)
	}
}

func TestRecoverStaleReservationsRetriesWithoutRespending(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fixture := newEfficacyFixture(t, "skill_efficacy_recover_retry")
	fixture.writeSettings(t, true, 1, 1, 0)

	skill := fixture.captureSkill(t, "efficacy-recover-retry")
	fixture.seedUnits(t, skill, "recover-retry-session", 1, time.Now().UTC().Add(-6*time.Hour))
	fixture.enqueueSeeded(t, 1)

	reserved, _, err := Reserve(ctx, fixture.db, &stubFeatures{enabled: true}, fixture.projectID, PendingCursor{}, 1)
	require.NoError(t, err)
	require.Len(t, reserved, 1)
	abandoned := reserved[0]
	require.NotEqual(t, uuid.Nil, abandoned.ClaimToken)

	var recovery RecoveryResult
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		var recoverErr error
		recovery, recoverErr = RecoverStaleReservations(ctx, fixture.db, fixture.projectID, time.Millisecond, 1)
		assert.NoError(collect, recoverErr)
		assert.Equal(collect, RecoveryResult{Recovered: 1, DeadLettered: 0}, recovery)
	}, 5*time.Second, 10*time.Millisecond)

	pending := fixture.pendingEvaluations(t)
	require.Len(t, pending, 1)
	retried := pending[0]
	require.Equal(t, abandoned.ID, retried.ID)
	require.Equal(t, StatePending, pending[0].State)
	require.Equal(t, abandoned.ReservedOn, retried.ReservedOn.Time, "recovery retains immutable spend history")
	require.Equal(t, int32(1), retried.Attempts)
	require.False(t, retried.ClaimToken.Valid)
	require.Equal(t, staleReservationFailureClass, retried.LastError.String)
	require.False(t, retried.FailedAt.Valid)

	spend, err := repo.New(fixture.db).CountSkillEfficacyOrgSpendForProject(ctx, repo.CountSkillEfficacyOrgSpendForProjectParams{
		ProjectID:  fixture.projectID,
		ReservedOn: retried.ReservedOn,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), spend)

	reservedAgain, _, err := Reserve(ctx, fixture.db, &stubFeatures{enabled: true}, fixture.projectID, PendingCursor{}, 1)
	require.NoError(t, err)
	require.Len(t, reservedAgain, 1, "a recovered row already owns its cap slot")
	require.Equal(t, abandoned.ID, reservedAgain[0].ID)
	require.Equal(t, abandoned.ReservedOn, reservedAgain[0].ReservedOn)
	require.NotEqual(t, abandoned.ClaimToken, reservedAgain[0].ClaimToken)

	spend, err = repo.New(fixture.db).CountSkillEfficacyOrgSpendForProject(ctx, repo.CountSkillEfficacyOrgSpendForProjectParams{
		ProjectID:  fixture.projectID,
		ReservedOn: retried.ReservedOn,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), spend, "re-reservation does not consume another slot")
}

func TestRecoverStaleReservationsDeadLettersAtTheAttemptCeiling(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fixture := newEfficacyFixture(t, "skill_efficacy_recover_dead_letter")

	skill := fixture.captureSkill(t, "efficacy-recover-dead-letter")
	fixture.seedUnits(t, skill, "recover-dead-letter-session", 1, time.Now().UTC().Add(-6*time.Hour))
	fixture.enqueueSeeded(t, 1)

	reserved, _, err := Reserve(ctx, fixture.db, &stubFeatures{enabled: true}, fixture.projectID, PendingCursor{}, 1)
	require.NoError(t, err)
	require.Len(t, reserved, 1)

	row := reserved[0]
	for attempt := range MaxModelAttempts - 1 {
		stored, attemptErr := repo.New(fixture.db).RecordSkillEfficacyEvaluationAttempt(ctx, repo.RecordSkillEfficacyEvaluationAttemptParams{
			LastError:   conv.ToPGText("skill efficacy model failure"),
			MaxAttempts: MaxModelAttempts,
			ProjectID:   fixture.projectID,
			ID:          row.ID,
			ClaimToken:  row.ClaimToken,
		})
		require.NoError(t, attemptErr)
		require.Equal(t, StateReserved, stored.State)
		require.Equal(t, int32(attempt+1), stored.Attempts)
		require.False(t, stored.FailedAt.Valid)
	}

	var recovery RecoveryResult
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		var recoverErr error
		recovery, recoverErr = RecoverStaleReservations(ctx, fixture.db, fixture.projectID, time.Millisecond, 1)
		assert.NoError(collect, recoverErr)
		assert.Equal(collect, RecoveryResult{Recovered: 0, DeadLettered: 1}, recovery)
	}, 5*time.Second, 10*time.Millisecond)

	require.Empty(t, fixture.pendingEvaluations(t), "terminal work does not retry")
	require.Empty(t, fixture.reservedEvaluations(t, 1), "dead-lettered work leaves reserved")
}

func TestRecoverStaleReservationsBoundsConcurrentBatches(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fixture := newEfficacyFixture(t, "skill_efficacy_recover_concurrent")

	skill := fixture.captureSkill(t, "efficacy-recover-concurrent")
	fixture.seedUnits(t, skill, "recover-concurrent-session", 3, time.Now().UTC().Add(-6*time.Hour))
	fixture.enqueueSeeded(t, 3)

	reserved, _, err := Reserve(ctx, fixture.db, &stubFeatures{enabled: true}, fixture.projectID, PendingCursor{}, 3)
	require.NoError(t, err)
	require.Len(t, reserved, 3)

	var first RecoveryResult
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		var recoverErr error
		first, recoverErr = RecoverStaleReservations(ctx, fixture.db, fixture.projectID, time.Millisecond, 2)
		assert.NoError(collect, recoverErr)
		assert.Equal(collect, RecoveryResult{Recovered: 2, DeadLettered: 0}, first)
	}, 5*time.Second, 10*time.Millisecond)
	require.Len(t, fixture.pendingEvaluations(t), 2)
	require.Len(t, fixture.reservedEvaluations(t, 3), 1, "one bounded row remains")

	var group sync.WaitGroup
	results := make([]RecoveryResult, 2)
	errs := make([]error, 2)
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		for i := range results {
			group.Go(func() {
				results[i], errs[i] = RecoverStaleReservations(ctx, fixture.db, fixture.projectID, time.Millisecond, 2)
			})
		}
		group.Wait()

		assert.NoError(collect, errs[0])
		assert.NoError(collect, errs[1])
		assert.Equal(collect, int64(1), results[0].Recovered+results[1].Recovered)
	}, 5*time.Second, 10*time.Millisecond)
	require.Len(t, fixture.pendingEvaluations(t), 3)
}

func TestRecoverStaleReservationsIsProjectScoped(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fixture := newEfficacyFixture(t, "skill_efficacy_recover_isolation")

	newProject := func(name, organizationID string) efficacyFixture {
		project, err := projectsrepo.New(fixture.db).CreateProject(ctx, projectsrepo.CreateProjectParams{
			Name:           name,
			Slug:           "efficacy-" + uuid.NewString()[:8],
			OrganizationID: organizationID,
		})
		require.NoError(t, err)
		return efficacyFixture{db: fixture.db, organizationID: organizationID, projectID: project.ID, skillVersionID: uuid.Nil}
	}

	otherOrganizationID := "skill-efficacy-recover-isolation-" + uuid.NewString()
	_, err := orgrepo.New(fixture.db).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          otherOrganizationID,
		Name:        otherOrganizationID,
		Slug:        otherOrganizationID,
		WorkosID:    pgtype.Text{String: "", Valid: false},
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	projects := []efficacyFixture{
		fixture,
		newProject("skill-efficacy-recover-same-org", fixture.organizationID),
		newProject("skill-efficacy-recover-other-org", otherOrganizationID),
	}
	for index := range projects {
		skill := projects[index].captureSkill(t, "efficacy-recover-isolation-"+strconv.Itoa(index))
		projects[index].seedUnits(t, skill, "recover-isolation-session-"+strconv.Itoa(index), 1, time.Now().UTC().Add(-6*time.Hour))
		projects[index].enqueueSeeded(t, 1)
		reserved, _, reserveErr := Reserve(ctx, projects[index].db, &stubFeatures{enabled: true}, projects[index].projectID, PendingCursor{}, 1)
		require.NoError(t, reserveErr)
		require.Len(t, reserved, 1)
	}

	var recovery RecoveryResult
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		var recoverErr error
		recovery, recoverErr = RecoverStaleReservations(ctx, fixture.db, fixture.projectID, time.Millisecond, 1)
		assert.NoError(collect, recoverErr)
		assert.Equal(collect, RecoveryResult{Recovered: 1, DeadLettered: 0}, recovery)
	}, 5*time.Second, 10*time.Millisecond)

	require.Len(t, fixture.pendingEvaluations(t), 1)
	require.Len(t, projects[1].reservedEvaluations(t, 1), 1, "same-organization project is isolated")
	require.Len(t, projects[2].reservedEvaluations(t, 1), 1, "other organization is isolated")
}

func TestLoadReservedClaimsRecentFirstAndBumpsUpdatedAt(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fixture := newEfficacyFixture(t, "skill_efficacy_reserve_load_claim")

	skill := fixture.captureSkill(t, "efficacy-claim")
	fixture.seedUnits(t, skill, "claim-session", 3, time.Now().UTC().Add(-6*time.Hour))
	fixture.enqueueSeeded(t, 3)

	reserved, _, err := Reserve(ctx, fixture.db, &stubFeatures{enabled: true}, fixture.projectID, PendingCursor{}, 3)
	require.NoError(t, err)
	require.Len(t, reserved, 3)

	// Unleased, because the reservation's own updated_at bump owns the rows for
	// the lease and this asserts ordering rather than ownership.
	claimed, err := loadReserved(ctx, fixture.db, fixture.projectID, 2, 0)
	require.NoError(t, err)
	require.Len(t, claimed, 2)
	require.Equal(t, reserved[0].ID, claimed[0].ID, "reserved rows are claimed recent-first")
	require.Equal(t, reserved[1].ID, claimed[1].ID)
	require.Equal(t, StateReserved, claimed[0].State, "the claim does not move the row out of reserved")
	require.Equal(t, reserved[0].ReservedOn, claimed[0].ReservedOn)

	// The claim is not a state change: every row is still reserved and none
	// returned to the pending queue.
	require.Len(t, fixture.reservedEvaluations(t, seedBatchSize), 3)
	require.Empty(t, fixture.pendingEvaluations(t))
}

func TestLoadReservedLeaseGivesTheRowToExactlyOneClaimer(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fixture := newEfficacyFixture(t, "skill_efficacy_reserve_lease_exclusive")

	skill := fixture.captureSkill(t, "efficacy-lease")
	fixture.seedUnits(t, skill, "lease-session", 1, time.Now().UTC().Add(-6*time.Hour))
	fixture.enqueueSeeded(t, 1)

	reserved, _, err := Reserve(ctx, fixture.db, &stubFeatures{enabled: true}, fixture.projectID, PendingCursor{}, 1)
	require.NoError(t, err)
	require.Len(t, reserved, 1)

	// The reservation's own updated_at bump starts the lease, so both claimers
	// come back empty until it ages out and then exactly one of them wins. A
	// short lease keeps the poll short; production uses ReservedClaimLease.
	const lease = time.Second
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		var group sync.WaitGroup
		counts := make([]int, 2)
		errs := make([]error, 2)
		for i := range counts {
			group.Go(func() {
				claimed, claimErr := loadReserved(ctx, fixture.db, fixture.projectID, 1, lease)
				counts[i] = len(claimed)
				errs[i] = claimErr
			})
		}
		group.Wait()

		assert.NoError(collect, errs[0])
		assert.NoError(collect, errs[1])
		assert.Equal(collect, 1, counts[0]+counts[1], "the row locks and the lease leave the row to one claimer")
	}, 30*time.Second, 100*time.Millisecond)

	// The same holds for a claim raised after the winner committed: the bump is
	// fresh, so the row is still owned.
	repeat, err := loadReserved(ctx, fixture.db, fixture.projectID, 1, lease)
	require.NoError(t, err)
	require.Empty(t, repeat, "a second claim inside the lease sees nothing")

	require.Len(t, fixture.reservedEvaluations(t, seedBatchSize), 1, "the row never left reserved")
}

func TestLoadReservedReclaimsAfterTheLeaseExpires(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fixture := newEfficacyFixture(t, "skill_efficacy_reserve_lease_expiry")

	skill := fixture.captureSkill(t, "efficacy-expiry")
	fixture.seedUnits(t, skill, "expiry-session", 1, time.Now().UTC().Add(-6*time.Hour))
	fixture.enqueueSeeded(t, 1)

	reserved, _, err := Reserve(ctx, fixture.db, &stubFeatures{enabled: true}, fixture.projectID, PendingCursor{}, 1)
	require.NoError(t, err)
	require.Len(t, reserved, 1)

	// A short injected lease stands in for an owner that crashed: nothing bumps
	// the row again, so it falls out of its lease and the next claim takes it.
	// The first poll waits out the reservation's own bump.
	const lease = time.Second
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		claimed, claimErr := loadReserved(ctx, fixture.db, fixture.projectID, 1, lease)
		assert.NoError(collect, claimErr)
		if assert.Len(collect, claimed, 1) {
			assert.Equal(collect, reserved[0].ID, claimed[0].ID)
		}
	}, 30*time.Second, 50*time.Millisecond)

	blocked, err := loadReserved(ctx, fixture.db, fixture.projectID, 1, lease)
	require.NoError(t, err)
	require.Empty(t, blocked, "the row is owned while the lease holds")

	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		reclaimed, claimErr := loadReserved(ctx, fixture.db, fixture.projectID, 1, lease)
		assert.NoError(collect, claimErr)
		assert.Len(collect, reclaimed, 1)
	}, 30*time.Second, 50*time.Millisecond)
}

func TestLoadReservedRefusesABatchWiderThanTheLeaseCovers(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_reserve_lease_ceiling")

	_, err := LoadReserved(t.Context(), fixture.db, fixture.projectID, MaxReservedClaimBatch+1)
	require.Error(t, err)
}

func TestReserveRefusesABatchWiderThanTheLeaseCovers(t *testing.T) {
	t.Parallel()
	fixture := newEfficacyFixture(t, "skill_efficacy_reserve_batch_ceiling")

	skill := fixture.captureSkill(t, "efficacy-ceiling")
	fixture.seedUnits(t, skill, "ceiling-session", 1, time.Now().UTC().Add(-6*time.Hour))
	fixture.enqueueSeeded(t, 1)

	_, _, err := Reserve(t.Context(), fixture.db, &stubFeatures{enabled: true}, fixture.projectID, PendingCursor{}, MaxReservedClaimBatch+1)
	require.Error(t, err, "a reserved batch is judged under the same lease a claim takes")
	require.Len(t, fixture.pendingEvaluations(t), 1, "the refused batch spends nothing")
}

func TestCountSkillEfficacyVersionLifetimeSpendStopsAtTheBurstCap(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fixture := newEfficacyFixture(t, "skill_efficacy_reserve_capped_spend")

	const spent = 3
	fixture.writeSettings(t, true, spent, 10, spent)

	skill := fixture.captureSkill(t, "efficacy-capped-spend")
	fixture.seedUnits(t, skill, "capped-spend-session", spent, time.Now().UTC().Add(-6*time.Hour))
	fixture.enqueueSeeded(t, spent)

	reserved, _, err := Reserve(ctx, fixture.db, &stubFeatures{enabled: true}, fixture.projectID, PendingCursor{}, spent)
	require.NoError(t, err)
	require.Len(t, reserved, spent)

	// The count is only ever subtracted from the cap, so it has to be the true
	// spend below the cap and the cap itself at or above it.
	absent := uuid.New()
	for _, tt := range []struct {
		name     string
		burstCap int32
		want     int64
	}{
		{name: "below the spend", burstCap: 2, want: 2},
		{name: "at the spend", burstCap: spent, want: spent},
		{name: "above the spend", burstCap: spent + 5, want: spent},
		{name: "zero cap counts nothing", burstCap: 0, want: 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rows, err := repo.New(fixture.db).CountSkillEfficacyVersionLifetimeSpend(t.Context(), repo.CountSkillEfficacyVersionLifetimeSpendParams{
				ProjectID:       fixture.projectID,
				SkillVersionIds: []uuid.UUID{skill.skillVersionID, absent},
				BurstCap:        tt.burstCap,
			})
			require.NoError(t, err)

			spend := make(map[uuid.UUID]int64, len(rows))
			for _, row := range rows {
				spend[row.SkillVersionID] = row.Spend
			}
			require.Equal(t, tt.want, spend[skill.skillVersionID])
			require.Contains(t, spend, absent, "every requested version comes back")
			require.Zero(t, spend[absent])
		})
	}
}
