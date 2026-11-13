package sc_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

func TestSendAndSettleWaitsForTheServicesTheStepStartedAndTheirFollowUps(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		slow := func(context.Context, counter) (int, error) {
			time.Sleep(time.Minute) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
			return 1, nil
		}

		m, err := sc.New("chain", func(b *sc.Builder[string, counter]) {
			b.Initial("idle")
			b.State("idle").On[tick]().To("first")
			first := b.State("first").Invoke("first", slow)
			first.OnDone().To("second")
			first.OnError().To("idle")
			second := b.State("second").Invoke("second", slow)
			second.OnDone().To("done")
			second.OnError().To("idle")
			b.State("done")
		})
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()

		_, err = inst.SendAndSettle(t.Context(), tick{})
		require.NoError(t, err)

		require.Equal(t, []string{"done"}, inst.Snapshot().Active())
	})
}

func TestCancelInterruptsAServiceButStillDeliversItsResult(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := workChart(t, func(ctx context.Context, _ counter) (int, error) {
			<-ctx.Done()
			return 0, ctx.Err()
		})

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()

		step, err := inst.Send(t.Context(), tick{})
		require.NoError(t, err)
		runs := step.To.Services()
		require.Len(t, runs, 1)

		require.True(t, inst.Cancel(runs[0].ID))

		done, err := inst.Await(t.Context(), func(s sc.Snapshot[string, counter]) bool { return s.Done() })
		require.NoError(t, err)
		require.True(t, done.Done())
		require.False(t, inst.Cancel("no such run"))
	})
}

// codedError is a service error with a code, as a rate-limit refusal is.
type codedError struct{}

func (codedError) Error() string { return "rate limited" }

func (codedError) Code() string { return "rate_limited" }

func TestAnErrorsCodeSurvivesStorage(t *testing.T) {
	t.Parallel()

	wrapped := sc.Failed{ID: "x", Err: errors.Join(errWorkFailed, codedError{})}
	require.Equal(t, "rate_limited", sc.ErrorCode(wrapped.Err))

	data, err := json.Marshal(wrapped)
	require.NoError(t, err)

	var loaded sc.Failed
	require.NoError(t, json.Unmarshal(data, &loaded))

	require.Equal(t, "rate_limited", sc.ErrorCode(loaded.Err))
	require.Equal(t, wrapped.Err.Error(), loaded.Err.Error())
	require.Empty(t, sc.ErrorCode(errWorkFailed))
}

func TestConditionalRunsAppearInTheEffectsOnlyWhenTheyRun(t *testing.T) {
	t.Parallel()

	crossesOnline := func(mv sc.Move[string]) bool {
		was, is := false, false
		for _, s := range mv.From {
			was = was || s == "online"
		}
		for _, s := range mv.To {
			is = is || s == "online"
		}

		return was != is
	}

	m, err := sc.New("wake", func(b *sc.Builder[string, counter]) {
		b.Action("wake", func(context.Context, counter, sc.Move[string]) {})
		b.Initial("offline")
		b.State("offline").OnEntryIf("wake", crossesOnline).On[connect]().To("connecting")
		connecting := b.State("connecting").OnEntryIf("wake", crossesOnline)
		connecting.On[tick]().To("online")
		connecting.On[pong]().RunIf("wake", crossesOnline)
		b.State("online").OnEntryIf("wake", crossesOnline).On[disconnect]().To("offline")
	})
	require.NoError(t, err)

	wakes := func(effects []sc.Effect[counter]) int {
		n := 0
		for _, e := range effects {
			if e.Name == "wake" {
				n++
			}
		}

		return n
	}

	snap, effects := m.Start(counter{}, epoch)
	require.Zero(t, wakes(effects))

	snap, effects = m.Transition(snap, connect{}, epoch)
	require.Zero(t, wakes(effects))

	_, effects = m.Transition(snap, pong{}, epoch)
	require.Zero(t, wakes(effects))

	snap, effects = m.Transition(snap, tick{}, epoch)
	require.Equal(t, 1, wakes(effects))

	_, effects = m.Transition(snap, disconnect{}, epoch)
	require.Equal(t, 1, wakes(effects))
}

func TestExploreFromSeveralStartingContexts(t *testing.T) {
	t.Parallel()

	m, err := sc.New("input", func(b *sc.Builder[string, counter]) {
		b.Initial("deciding")
		deciding := b.State("deciding")
		deciding.Always().When("big", func(c counter, _ struct{}) bool { return c.N > 10 }).To("big")
		deciding.Always().To("small")
		b.State("big")
		b.State("small")
	})
	require.NoError(t, err)

	report, err := m.Explore(counter{N: 1}, sc.Exploration[string, counter]{
		Starts: []counter{{N: 20}},
		Invariants: []sc.Invariant[string, counter]{{
			Name:  "never big",
			Holds: func(s sc.Snapshot[string, counter]) bool { return !s.In("big") },
		}},
	})
	require.NoError(t, err)

	require.True(t, report.Reached("small"))
	require.Len(t, report.Violations, 1)
	require.Equal(t, counter{N: 20}, report.Violations[0].Trace.Initial)
}

func TestInstanceReturnsNoInstanceWhenItsStartFails(t *testing.T) {
	t.Parallel()

	m, err := sc.New("failing start", func(b *sc.Builder[string, counter]) {
		b.RequiresAction("hello")
		b.Initial("a")
		b.State("a").OnEntry("hello")
	})
	require.NoError(t, err)

	inst, err := m.Instance(t.Context(), counter{})
	require.ErrorIs(t, err, sc.ErrNotBound)
	require.Nil(t, inst)
}
