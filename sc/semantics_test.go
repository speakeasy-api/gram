package sc_test

import (
	"context"
	"iter"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

type (
	tick  struct{}
	ping  struct{}
	pong  struct{}
	leave struct{}
	back  struct{}
	reset struct{}
)

// counter is the context of the small charts in this file.
type counter struct {
	N int `json:"n"`
}

func increment[Ev any](c counter, _ Ev) counter {
	c.N++
	return c
}

func noop(context.Context, counter) {}

func names[C any](effects []sc.Effect[C]) []string {
	out := make([]string, 0, len(effects))
	for _, e := range effects {
		out = append(out, e.State+"/"+e.Name)
	}

	return out
}

func TestAlwaysTransitionsRunBeforeTheNextEvent(t *testing.T) {
	t.Parallel()

	m, err := sc.New("always", func(b *sc.Builder[string, counter]) {
		b.Initial("counting")
		counting := b.State("counting")
		counting.On[tick]().Assign("increment", increment[tick])
		counting.Always().
			When("reachedThree", func(c counter, _ struct{}) bool { return c.N >= 3 }).
			To("full")
		b.Final("full")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	for range 2 {
		snap, _ = m.Transition(snap, tick{}, epoch)
	}
	require.Equal(t, []string{"counting"}, snap.Active())

	snap, _ = m.Transition(snap, tick{}, epoch)

	require.Equal(t, []string{"full"}, snap.Active())
	require.True(t, snap.Done())
}

func TestAlwaysCycleIsReportedInsteadOfHanging(t *testing.T) {
	t.Parallel()

	m, err := sc.New("cycle", func(b *sc.Builder[string, counter]) {
		b.Initial("a")
		b.State("a").On[tick]().To("b")
		b.State("b").Always().To("c")
		b.State("c").Always().To("b")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)

	require.PanicsWithValue(t,
		`sc: machine "cycle" does not settle within 10000 steps: its Always transitions or raised events form a cycle, last processing sc_test.tick in states [c]`,
		func() { m.Transition(snap, tick{}, epoch) },
	)
}

func TestRaiseSignalsAnotherRegionInTheSameStep(t *testing.T) {
	t.Parallel()

	m, err := sc.New("raise", func(b *sc.Builder[string, counter]) {
		b.Initial("both")
		b.Parallel("both", func(b *sc.Builder[string, counter]) {
			b.State("left", func(b *sc.Builder[string, counter]) {
				b.Initial("left_waiting")
				b.State("left_waiting").On[ping]().
					Raise("replyPong", func(counter, ping) any { return pong{} }).
					To("left_done")
				b.State("left_done")
			})
			b.State("right", func(b *sc.Builder[string, counter]) {
				b.Initial("right_waiting")
				b.State("right_waiting").On[pong]().To("right_done")
				b.State("right_done")
			})
		})
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, _ = m.Transition(snap, ping{}, epoch)

	require.Equal(t, []string{"both", "left", "left_done", "right", "right_done"}, snap.Active())
	require.Equal(t, uint64(1), snap.Seq())
}

// regionsChart has two regions that both handle tick, and a second event
// that one region uses to leave the parallel state.
func regionsChart(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("regions", func(b *sc.Builder[string, counter]) {
		b.Initial("both")
		b.Parallel("both", func(b *sc.Builder[string, counter]) {
			b.History("both_history", sc.Deep)
			b.State("left", func(b *sc.Builder[string, counter]) {
				b.Initial("l1")
				l1 := b.State("l1")
				l1.On[tick]().Assign("leftCount", increment[tick]).To("l2")
				l1.On[leave]().Do("leftSaw", func(context.Context, counter, leave) {})
				b.State("l2")
			})
			b.State("right", func(b *sc.Builder[string, counter]) {
				b.Initial("r1")
				r1 := b.State("r1")
				r1.On[tick]().Assign("rightCount", increment[tick]).To("r2")
				r1.On[leave]().To("outside")
				b.State("r2").On[leave]().To("outside")
			})
		})
		b.State("outside").On[back]().To("both_history")
	})
	require.NoError(t, err)

	return m
}

func TestEveryRegionHandlesTheSameEvent(t *testing.T) {
	t.Parallel()

	m := regionsChart(t)
	snap, _ := m.Start(counter{}, epoch)

	snap, _ = m.Transition(snap, tick{}, epoch)

	require.Equal(t, []string{"both", "left", "l2", "right", "r2"}, snap.Active())
	require.Equal(t, 2, snap.Context.N)
}

func TestTargetlessTransitionRunsAlongsideARegionLeavingTheParallelState(t *testing.T) {
	t.Parallel()

	m := regionsChart(t)
	snap, _ := m.Start(counter{}, epoch)

	snap, effects := m.Transition(snap, leave{}, epoch)

	require.Equal(t, []string{"outside"}, snap.Active())
	require.Equal(t, []string{"l1/leftSaw"}, names(effects))
}

func TestDeepHistoryRestoresEveryRegion(t *testing.T) {
	t.Parallel()

	m := regionsChart(t)
	snap, _ := m.Start(counter{}, epoch)
	snap, _ = m.Transition(snap, tick{}, epoch)
	snap, _ = m.Transition(snap, leave{}, epoch)
	require.Equal(t, []string{"outside"}, snap.Active())

	snap, _ = m.Transition(snap, back{}, epoch)

	require.Equal(t, []string{"both", "left", "l2", "right", "r2"}, snap.Active())
}

func TestHistoryWithNothingRecordedUsesDefaultEntry(t *testing.T) {
	t.Parallel()

	m, err := sc.New("defaults", func(b *sc.Builder[string, counter]) {
		b.Initial("outside")
		outside := b.State("outside")
		outside.On[back]().To("plain_history")
		outside.On[tick]().To("default_history")
		b.State("work", func(b *sc.Builder[string, counter]) {
			b.Initial("first")
			b.History("plain_history", sc.Shallow)
			b.History("default_history", sc.Shallow).Default("second")
			b.State("first")
			b.State("second")
		})
	})
	require.NoError(t, err)

	start, _ := m.Start(counter{}, epoch)

	viaInitial, _ := m.Transition(start, back{}, epoch)
	require.Equal(t, []string{"work", "first"}, viaInitial.Active())

	viaDefault, _ := m.Transition(start, tick{}, epoch)
	require.Equal(t, []string{"work", "second"}, viaDefault.Active())
}

func TestShallowHistoryRestartsTheRememberedChild(t *testing.T) {
	t.Parallel()

	m, err := sc.New("shallow", func(b *sc.Builder[string, counter]) {
		b.Initial("work")
		b.State("work", func(b *sc.Builder[string, counter]) {
			b.Initial("draft")
			b.History("work_history", sc.Shallow)
			b.State("draft").On[tick]().To("review")
			b.State("review", func(b *sc.Builder[string, counter]) {
				b.Initial("queued")
				b.State("queued").On[tick]().To("reading")
				b.State("reading")
			})
		}).On[leave]().To("paused")
		b.State("paused").On[back]().To("work_history")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, _ = m.Transition(snap, tick{}, epoch)
	snap, _ = m.Transition(snap, tick{}, epoch)
	require.Equal(t, []string{"work", "review", "reading"}, snap.Active())

	snap, _ = m.Transition(snap, leave{}, epoch)
	snap, _ = m.Transition(snap, back{}, epoch)

	// Shallow history remembers review, not which of its children was
	// active, so review starts from its initial state.
	require.Equal(t, []string{"work", "review", "queued"}, snap.Active())
}

// reentryChart has a compound state with entry and exit actions, a timer on
// its child, and self-targeting transitions with and without Reenter.
func reentryChart(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("reentry", func(b *sc.Builder[string, counter]) {
		b.Initial("outer")
		outer := b.State("outer", func(b *sc.Builder[string, counter]) {
			b.Initial("inner")
			inner := b.State("inner").Entry("innerIn", noop).Exit("innerOut", noop)
			inner.After(time.Minute).To("other")
			inner.On[ping]().Assign("pingCount", increment[ping]).To("inner")
			inner.On[pong]().To("inner").Reenter()
			b.State("other")
		}).Entry("outerIn", noop).Exit("outerOut", noop)
		outer.On[tick]().To("other")
		outer.On[reset]().To("outer")
		outer.On[back]().To("outer").Reenter()
	})
	require.NoError(t, err)

	return m
}

func TestStartRunsEntryActionsOuterFirstAndStartsTimer(t *testing.T) {
	t.Parallel()

	_, effects := reentryChart(t).Start(counter{}, epoch)

	require.Equal(t, []string{"outer/outerIn", "inner/innerIn", "inner/"}, names(effects))
	require.Equal(t, sc.EffectStartTimer, effects[2].Kind)
}

func TestSelfTransitionDoesNotReenterByDefault(t *testing.T) {
	t.Parallel()

	m := reentryChart(t)
	snap, _ := m.Start(counter{}, epoch)

	snap, effects := m.Transition(snap, ping{}, epoch)

	require.Equal(t, 1, snap.Context.N)
	require.Equal(t, []string{"outer", "inner"}, snap.Active())
	require.Empty(t, effects)
}

func TestReenterRunsExitAndEntryAndRestartsTimer(t *testing.T) {
	t.Parallel()

	m := reentryChart(t)
	snap, _ := m.Start(counter{}, epoch)

	snap, effects := m.Transition(snap, pong{}, epoch)

	require.Equal(t, []string{"outer", "inner"}, snap.Active())
	require.Equal(t,
		[]sc.EffectKind{sc.EffectAction, sc.EffectCancelTimer, sc.EffectAction, sc.EffectStartTimer},
		kinds(effects),
	)
	require.Equal(t, []string{"inner/innerOut", "inner/", "inner/innerIn", "inner/"}, names(effects))
	require.NotEqual(t, effects[1].ID, effects[3].ID)
}

func TestTransitionToChildKeepsParentActive(t *testing.T) {
	t.Parallel()

	m := reentryChart(t)
	snap, _ := m.Start(counter{}, epoch)

	snap, effects := m.Transition(snap, tick{}, epoch)

	require.Equal(t, []string{"outer", "other"}, snap.Active())
	require.Equal(t, []string{"inner/innerOut", "inner/"}, names(effects))
}

func TestSelfTargetOnCompoundStateResetsChildrenOnly(t *testing.T) {
	t.Parallel()

	m := reentryChart(t)
	snap, _ := m.Start(counter{}, epoch)
	snap, _ = m.Transition(snap, tick{}, epoch)

	snap, effects := m.Transition(snap, reset{}, epoch)

	require.Equal(t, []string{"outer", "inner"}, snap.Active())
	require.Equal(t, []string{"inner/innerIn", "inner/"}, names(effects))
}

func TestReenterOnCompoundStateExitsAndEntersIt(t *testing.T) {
	t.Parallel()

	m := reentryChart(t)
	snap, _ := m.Start(counter{}, epoch)
	snap, _ = m.Transition(snap, tick{}, epoch)

	_, effects := m.Transition(snap, back{}, epoch)

	require.Equal(t, []string{"outer/outerOut", "outer/outerIn", "inner/innerIn", "inner/"}, names(effects))
}

func TestChildTransitionTakesPriorityOverParent(t *testing.T) {
	t.Parallel()

	m, err := sc.New("priority", func(b *sc.Builder[string, counter]) {
		b.Initial("parent")
		b.State("parent", func(b *sc.Builder[string, counter]) {
			b.Initial("child")
			b.State("child").On[tick]().To("by_child")
			b.State("by_child")
		}).On[tick]().To("by_parent")
		b.State("by_parent")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, _ = m.Transition(snap, tick{}, epoch)
	require.Equal(t, []string{"parent", "by_child"}, snap.Active())

	// by_child does not handle tick, so the parent's transition applies.
	snap, _ = m.Transition(snap, tick{}, epoch)
	require.Equal(t, []string{"by_parent"}, snap.Active())
}

func TestCompoundStateOnDoneFiresWhenItReachesAFinalChild(t *testing.T) {
	t.Parallel()

	m, err := sc.New("done", func(b *sc.Builder[string, counter]) {
		b.Initial("job")
		b.State("job", func(b *sc.Builder[string, counter]) {
			b.Initial("running")
			b.State("running").On[tick]().To("finished")
			b.Final("finished")
		}).OnDone().Assign("countDone", increment[struct{}]).To("after")
		b.State("after")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{}, epoch)
	snap, _ = m.Transition(snap, tick{}, epoch)

	require.Equal(t, []string{"after"}, snap.Active())
	require.Equal(t, 1, snap.Context.N)
	require.False(t, snap.Done())
}

func TestStreamEventsAreDeliveredOnlyWhileTheServiceRuns(t *testing.T) {
	t.Parallel()

	m, err := sc.New("stream", func(b *sc.Builder[string, counter]) {
		b.Initial("listening")
		listening := b.State("listening")
		listening.Stream("ticks", func(context.Context, counter) iter.Seq2[any, error] {
			return func(func(any, error) bool) {}
		}).OnError().To("stopped")
		listening.On[tick]().Assign("increment", increment[tick])
		listening.On[leave]().To("stopped")
		b.State("stopped").On[tick]().Assign("lateIncrement", increment[tick])
	})
	require.NoError(t, err)

	snap, effects := m.Start(counter{}, epoch)
	id := effect(t, effects, sc.EffectStartInvocation).ID

	snap, _ = m.Transition(snap, sc.Emitted{ID: id, Event: tick{}}, epoch)
	require.Equal(t, 1, snap.Context.N)

	snap, _ = m.Transition(snap, leave{}, epoch)
	snap, _ = m.Transition(snap, sc.Emitted{ID: id, Event: tick{}}, epoch)

	require.Equal(t, 1, snap.Context.N)
}
