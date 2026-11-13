package provisioner

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

// allFacts is every combination of the facts a slot pass reads that
// factsOf can produce. Hosted and Refused both come from the status column
// (needs_login is not a hosting status), so they are never both set; the
// chart's "a hosted device is never deleted" depends on that, which the
// first run of this exploration pointed out.
func allFacts() []slotFacts {
	var out []slotFacts
	for bits := range 1 << 5 {
		f := slotFacts{
			Slot: 1, NodeID: "n1",
			Retired: bits&1 != 0, Hosted: bits&2 != 0, Refused: bits&4 != 0, KeyValid: bits&8 != 0, Surplus: bits&16 != 0,
		}
		if f.Hosted && f.Refused {
			continue
		}
		out = append(out, f)
	}
	return out
}

func started[S comparable, C any](st sc.Step[S, C], name string) bool {
	for _, e := range st.Effects {
		if e.Kind == sc.EffectStartInvocation && e.Name == name {
			return true
		}
	}
	return false
}

// TestSlotReconcileChartKeepsTheProvisionerRules explores every combination
// of facts and every service outcome. A pass starts from the facts, so
// there is one exploration per combination.
func TestSlotReconcileChartKeepsTheProvisionerRules(t *testing.T) {
	t.Parallel()
	m, err := newSlotChart()
	require.NoError(t, err)

	fail := sc.Fails(stepError{kind: failureAPI, err: errors.New("tailscale api: unavailable")})
	states := []SlotState{SlotClassify, SlotPurging, SlotChecking, SlotResetting, SlotMinting, SlotHostStillLive, SlotPurged, SlotSurplus, SlotRegistered, SlotKeyHeld, SlotMinted, SlotFailed}
	reached := make(map[SlotState]bool, len(states))
	for _, facts := range allFacts() {
		report, err := m.Explore(slotPass{Facts: facts}, sc.Exploration[SlotState, slotPass]{
			Outcomes: map[string][]sc.Outcome{
				slotPurge:           {sc.Succeeds(struct{}{}), fail},
				slotCheckRegistered: {sc.Succeeds(true), sc.Succeeds(false), fail},
				slotReset:           {sc.Succeeds(struct{}{}), fail},
				slotMint:            {sc.Succeeds(struct{}{}), fail},
			},
			Key: func(p slotPass, _ time.Time) any {
				return struct {
					Registered bool `json:"registered"`
					Reset      bool `json:"reset"`
					Failed     bool `json:"failed"`
				}{p.Registered, p.Reset, p.Failure != nil}
			},
			CheckDeterminism: true,
			Invariants: []sc.Invariant[SlotState, slotPass]{
				{Name: "a failed pass says why", Holds: func(s sc.Snapshot[SlotState, slotPass]) bool {
					return !s.In(SlotFailed) || s.Context.Failure != nil
				}},
			},
			Steps: []sc.StepInvariant[SlotState, slotPass]{
				{Name: "a device a pod still hosts is never deleted", Holds: func(st sc.Step[SlotState, slotPass]) bool {
					return (!started(st, slotPurge) && !started(st, slotReset)) || !st.To.Context.Facts.Hosted
				}},
				{Name: "a retired slot never gets a key", Holds: func(st sc.Step[SlotState, slotPass]) bool {
					return !started(st, slotMint) || !st.To.Context.Facts.Retired
				}},
				{Name: "a key is minted only for an identity that has not logged in, or was just reset", Holds: func(st sc.Step[SlotState, slotPass]) bool {
					return !started(st, slotMint) || !st.To.Context.Registered
				}},
				{Name: "a refused identity is reset before a new key", Holds: func(st sc.Step[SlotState, slotPass]) bool {
					return !started(st, slotMint) || !st.To.Context.Facts.Refused || st.To.Context.Reset || !st.From.Context.Registered
				}},
				{Name: "an unexpired key is not replaced", Holds: func(st sc.Step[SlotState, slotPass]) bool {
					return !started(st, slotMint) || !st.To.Context.Facts.KeyValid
				}},
			},
		})
		require.NoError(t, err)
		for _, v := range report.Violations {
			t.Errorf("%+v: %s:\n%s", facts, v.Invariant, m.FormatTrace(v.Trace))
		}
		require.Empty(t, report.Stuck)
		require.Empty(t, report.Trapped, "every pass ends")
		require.False(t, report.Truncated)
		for _, s := range states {
			reached[s] = reached[s] || report.Reached(s)
		}
	}
	for _, s := range states {
		require.True(t, reached[s], "%s is reachable", s)
	}
}

func TestTeardownChartDeletesNothingWhilePodsHost(t *testing.T) {
	t.Parallel()
	m, err := newTeardownChart()
	require.NoError(t, err)

	fail := sc.Fails(errors.New("tailscale api: rate limited"))
	for _, creds := range []teardownPass{
		{HasCredentials: true, CredentialsOK: true},
		{HasCredentials: false},
		{HasCredentials: true, CredentialsOK: false},
	} {
		report, err := m.Explore(creds, sc.Exploration[TeardownState, teardownPass]{
			Outcomes: map[string][]sc.Outcome{
				teardownCountSlots:    {sc.Succeeds(0), sc.Succeeds(2), fail},
				teardownRetire:        {sc.Succeeds(retired{Hosted: true, HostedSlot: 1}), sc.Succeeds(retired{NodeIDs: []string{"n1"}}), fail},
				teardownDeleteDevices: {sc.Succeeds(struct{}{}), fail},
				teardownDeleteService: {sc.Succeeds(struct{}{}), fail},
				teardownPurge:         {sc.Succeeds(struct{}{}), fail},
			},
			Key: func(p teardownPass, _ time.Time) any {
				return struct {
					Devices int  `json:"devices"`
					Failed  bool `json:"failed"`
				}{len(p.NodeIDs), p.Failure != nil}
			},
			CheckDeterminism: true,
			Steps: []sc.StepInvariant[TeardownState, teardownPass]{
				{Name: "Tailscale is called only with usable credentials, after every slot was retired and no pod hosts one", Holds: func(st sc.Step[TeardownState, teardownPass]) bool {
					for _, e := range st.Effects {
						if e.Kind == sc.EffectStartInvocation && (e.Name == teardownDeleteDevices || e.Name == teardownDeleteService) {
							if !st.From.In(TeardownRetiring) && !st.From.In(TeardownDeletingDevices) {
								return false
							}
							if !e.Context.HasCredentials || !e.Context.CredentialsOK {
								return false
							}
						}
					}
					return true
				}},
			},
		})
		require.NoError(t, err)
		require.Empty(t, report.Violations)
		require.Empty(t, report.Stuck)
		require.Empty(t, report.Trapped)
		require.False(t, report.Truncated)
	}
}

func TestAuthKeyDescriptionIsAcceptedByTailscale(t *testing.T) {
	t.Parallel()
	ingressID := uuid.MustParse("1a2b3c4d-0000-4000-8000-000000000000")
	require.Equal(t, "Speakeasy gateway 1a2b3c4d slot 2", authKeyDescription(ingressID, 2))
	require.Regexp(t, `^[A-Za-z0-9 ]{1,50}$`, authKeyDescription(uuid.New(), 1000))
}
