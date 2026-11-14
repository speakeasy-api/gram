package sc_test

// Ports of XState's packages/core/test/final.test.ts (6.0.0-beta.2), to check
// sc's semantics of final states against XState's. Each test names the XState
// case it ports. Cases that test an XState-only API are listed at the end of
// the file with the reason they were not ported.
//
// Four differences in what sc can declare shape every port:
//
//   - XState's root is declared with Builder.Chart, and a parallel root with
//     Builder.Regions. The chart's own actions are named after the machine,
//     so XState's "exit: __root__" is "m/exit" here.
//   - sc has no output on the root. A machine's output is that of the
//     top-level final state it ended in, read with Machine.Output, and a
//     final state's output is computed from the context alone.
//   - State identifiers are unique in an sc chart, so XState's repeated
//     local names ("start", "finish") are qualified by their region.
//   - sc rejects a state nothing can reach, so a chart in which XState
//     leaves a state unreachable is declared with sc.AllowUnreachable.

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

type (
	xfinNext1         struct{}
	xfinNext2         struct{}
	xfinNext3         struct{}
	xfinNext4         struct{}
	xfinPass          struct{}
	xfinFail          struct{}
	xfinCancel        struct{}
	xfinChildCanceled struct{}
	xfinFinish        struct{ Value int }
)

// xfinSecretCtx is the context of XState's chart that reveals a secret.
type xfinSecretCtx struct {
	RevealedSecret string
}

// xfinSecret is the output of that chart's nested final state.
type xfinSecret struct {
	Secret string
}

// xfinStatus is the output of XState's passed and failed final states.
type xfinStatus struct {
	Status string
}

type xfinBuilder = sc.Builder[string, counter]

// xfinSpy is an action of a completion transition whose effect a test looks
// for.
func xfinSpy(context.Context, counter, struct{}) {}

// xfinActions returns the actions among effects, as "state/name".
func xfinActions(effects []sc.Effect[counter]) []string {
	out := make([]string, 0, len(effects))
	for _, e := range effects {
		if e.Kind == sc.EffectAction {
			out = append(out, e.State+"/"+e.Name)
		}
	}

	return out
}

// xfinEffect returns the action effect called name, as "state/name".
func xfinEffect(t *testing.T, effects []sc.Effect[counter], name string) sc.Effect[counter] {
	t.Helper()

	for _, e := range effects {
		if e.Kind == sc.EffectAction && e.State+"/"+e.Name == name {
			return e
		}
	}

	require.Failf(t, "no such action", "%s is not among %v", name, xfinActions(effects))

	return sc.Effect[counter]{}
}

// xfinCount reports how many times name is in actions.
func xfinCount(actions []string, name string) int {
	n := 0
	for _, a := range actions {
		if a == name {
			n++
		}
	}

	return n
}

// xfinSend processes events in order and returns the last snapshot with the
// actions of every step.
func xfinSend(m *sc.Machine[string, counter], snap sc.Snapshot[string, counter], events ...any) (sc.Snapshot[string, counter], []string) {
	var actions []string
	for _, ev := range events {
		var effects []sc.Effect[counter]
		snap, effects = m.Transition(snap, ev, epoch)
		actions = append(actions, xfinActions(effects)...)
	}

	return snap, actions
}

// xfinRegion declares a compound region name whose state name.start moves to
// the final state name.finish on Ev.
func xfinRegion[Ev any](b *xfinBuilder, name string) {
	b.State(name, func(b *xfinBuilder) {
		b.Initial(name + ".start")
		b.State(name + ".start").On[Ev]().To(name + ".finish")
		b.Final(name + ".finish")
	})
}

// XState: should emit the done state event when all nested states are final
func TestXFinEmitsDoneStateEventWhenAllNestedStatesAreFinal(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("foo")
		b.Parallel("foo", func(b *xfinBuilder) {
			xfinRegion[xfinNext1](b, "first")
			xfinRegion[xfinNext2](b, "second")
		}).OnDone().Do("onDoneSpy", xfinSpy).To("bar")
		b.State("bar")
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)

	snap, actions := xfinSend(m, snap, xfinNext1{})
	require.True(t, snap.In("foo"))
	require.Empty(t, actions)

	snap, actions = xfinSend(m, snap, xfinNext2{})
	require.Equal(t, []string{"bar"}, snap.Active())
	require.Equal(t, []string{"foo/onDoneSpy"}, actions)
}

// XState: should execute final child state actions first
func TestXFinExecutesFinalChildStateActionsFirst(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("foo")
		b.State("foo", func(b *xfinBuilder) {
			b.Initial("bar")
			b.State("bar", func(b *xfinBuilder) {
				b.Initial("baz")
				b.Final("baz").Entry("bazAction", noop)
			}).OnDone().To("barFinal")
			b.Final("barFinal").Entry("barAction", noop)
		}).OnDone().Do("fooAction", xfinSpy)
	})
	require.NoError(t, err)

	_, effects := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"baz/bazAction", "barFinal/barAction", "foo/fooAction"}, xfinActions(effects))
}

// XState: should call output expressions on nested final nodes
func TestXFinCallsOutputExpressionsOnNestedFinalNodes(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *sc.Builder[string, xfinSecretCtx]) {
		b.Initial("secret")
		b.State("secret", func(b *sc.Builder[string, xfinSecretCtx]) {
			b.Initial("wait")
			b.State("wait").On[xfinNext1]().To("reveal")
			b.Final("reveal").Output(func(xfinSecretCtx) xfinSecret { return xfinSecret{Secret: "the secret"} })
		}).OnDoneWith[xfinSecret]().
			Assign("reveal", func(c xfinSecretCtx, done sc.Done[xfinSecret]) xfinSecretCtx {
				c.RevealedSecret = done.Result.Secret
				return c
			}).
			To("success")
		b.Final("success")
	})
	require.NoError(t, err)

	snap, _ := m.Start(xfinSecretCtx{RevealedSecret: ""}, epoch)
	require.False(t, snap.Done())

	snap, _ = m.Transition(snap, xfinNext1{}, epoch)
	require.True(t, snap.Done())
	require.Equal(t, xfinSecretCtx{RevealedSecret: "the secret"}, snap.Context)
}

// xfinStartEndChart is the chart most of XState's output cases use: start
// moves to the top-level final state end on an event. end declares what the
// final state reports, if anything.
func xfinStartEndChart(t *testing.T, end func(end *sc.FinalBuilder[string, counter])) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("start")
		b.State("start").On[xfinFinish]().
			Assign("keep", func(c counter, ev xfinFinish) counter { c.N = ev.Value; return c }).
			To("end")
		final := b.Final("end")
		if end != nil {
			end(final)
		}
	})
	require.NoError(t, err)

	return m
}

// xfinOutput declares that a final state reports value.
func xfinOutput[R any](value R) func(*sc.FinalBuilder[string, counter]) {
	return func(end *sc.FinalBuilder[string, counter]) {
		end.Output(func(counter) R { return value })
	}
}

// XState: should only call data expression once when entering root's final state
//
// XState's data expression is the root's output. sc has no output on the
// root, so the port counts the calls of the output of the top-level final
// state, which is what Machine.Output reports.
//
// DIVERGES from XState: XState resolves the machine's output once, when the
// machine completes, and keeps it in the snapshot. sc does not resolve it
// while transitioning at all: Machine.Output computes it from the snapshot's
// context each time it is asked.
func TestXFinCallsDataExpressionWhenEnteringRootsFinalState(t *testing.T) {
	t.Parallel()

	calls := 0
	m := xfinStartEndChart(t, func(end *sc.FinalBuilder[string, counter]) {
		end.Output(func(counter) string { calls++; return "output" })
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = xfinSend(m, snap, xfinFinish{Value: 1})

	require.True(t, snap.Done())
	require.Zero(t, calls)

	_, has := m.Output(snap)
	require.True(t, has)
	require.Equal(t, 1, calls)
}

// XState: should use top-level final state output as machine output without root output
//
// XState's output reads the value of the FINISH event. An sc output sees the
// context alone, so the transition keeps the value in the context.
func TestXFinUsesTopLevelFinalStateOutputAsMachineOutput(t *testing.T) {
	t.Parallel()

	m := xfinStartEndChart(t, func(end *sc.FinalBuilder[string, counter]) {
		end.Output(func(c counter) int { return c.N * 2 })
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = xfinSend(m, snap, xfinFinish{Value: 21})

	output, has := m.Output(snap)
	require.True(t, has)
	require.Equal(t, 42, output)
}

// XState: should leave machine output undefined without final or root output
func TestXFinLeavesMachineOutputUndefinedWithoutFinalOutput(t *testing.T) {
	t.Parallel()

	m := xfinStartEndChart(t, nil)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = xfinSend(m, snap, xfinFinish{Value: 0})

	require.True(t, snap.Done())

	output, has := m.Output(snap)
	require.False(t, has)
	require.Nil(t, output)
}

// XState: should use the reached top-level final state output
func TestXFinUsesTheReachedTopLevelFinalStateOutput(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("start")
		start := b.State("start")
		start.On[xfinPass]().To("passed")
		start.On[xfinFail]().To("failed")
		b.Final("passed").Output(func(counter) xfinStatus { return xfinStatus{Status: "passed"} })
		b.Final("failed").Output(func(counter) xfinStatus { return xfinStatus{Status: "failed"} })
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = xfinSend(m, snap, xfinFail{})

	output, has := m.Output(snap)
	require.True(t, has)
	require.Equal(t, xfinStatus{Status: "failed"}, output)
}

// XState: should populate top-level final state output through pure transition
func TestXFinPopulatesTopLevelFinalStateOutputThroughPureTransition(t *testing.T) {
	t.Parallel()

	m := xfinStartEndChart(t, xfinOutput("done"))

	snap, _ := m.Start(counter{N: 0}, epoch)
	_, has := m.Output(snap)
	require.False(t, has)

	next, _ := m.Transition(snap, xfinFinish{Value: 0}, epoch)

	output, has := m.Output(next)
	require.True(t, has)
	require.Equal(t, "done", output)
}

// XState: should populate top-level final state output through pure initialTransition
func TestXFinPopulatesTopLevelFinalStateOutputThroughPureStart(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("end")
		b.Final("end").Output(func(counter) string { return "done" })
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)

	output, has := m.Output(snap)
	require.True(t, has)
	require.Equal(t, "done", output)
}

// XState: should persist and restore top-level final state output
//
// DIVERGES from XState: XState's persisted snapshot holds the output. sc's
// does not: the output is not part of a snapshot, and Machine.Output computes
// it again from the restored context. The restored machine reports the same
// output either way.
func TestXFinPersistsAndRestoresTopLevelFinalStateOutput(t *testing.T) {
	t.Parallel()

	m := xfinStartEndChart(t, xfinOutput("persisted output"))

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = xfinSend(m, snap, xfinFinish{Value: 0})

	data, err := json.Marshal(snap)
	require.NoError(t, err)
	require.NotContains(t, string(data), "persisted output")

	restored, effects, err := m.Restore(data, epoch)
	require.NoError(t, err)
	require.Empty(t, effects)
	require.True(t, restored.Done())

	output, has := m.Output(restored)
	require.True(t, has)
	require.Equal(t, "persisted output", output)
}

// XState: should resolve top-level final state output once on completion
//
// XState's root output passes the final state's output on unchanged, which
// is what Machine.Output does without one.
//
// DIVERGES from XState: XState resolves the final state's output once, on
// completion, however often the snapshot is read. sc resolves it on demand:
// not at all while transitioning, and once for each call of Machine.Output.
func TestXFinResolvesTopLevelFinalStateOutputOnDemand(t *testing.T) {
	t.Parallel()

	calls := 0
	m := xfinStartEndChart(t, func(end *sc.FinalBuilder[string, counter]) {
		end.Output(func(counter) string { calls++; return "final output" })
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = xfinSend(m, snap, xfinFinish{Value: 0})
	require.Zero(t, calls)

	_, _ = m.Output(snap)
	_, _ = m.Output(snap)
	output, has := m.Output(snap)

	require.Equal(t, 3, calls)
	require.True(t, has)
	require.Equal(t, "final output", output)
}

// XState: state output should be able to use context updated by the entry action of the reached final state
func TestXFinStateOutputUsesContextUpdatedByTheEntryOfTheReachedFinalState(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("a")
		b.State("a", func(b *xfinBuilder) {
			b.Initial("a1")
			b.State("a1").On[xfinNext1]().To("a2")
			b.Final("a2").
				AssignOnEntry("count", func(c counter) counter { c.N = 1; return c }).
				Output(func(c counter) int { return c.N })
		}).OnDoneWith[int]().Do("spy", func(context.Context, counter, sc.Done[int]) {})
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	_, effects := m.Transition(snap, xfinNext1{}, epoch)

	require.Equal(t, sc.Done[int]{ID: "a2", Result: 1}, xfinEffect(t, effects, "a/spy").Event)
}

// xfinNestedParallelChart is a parallel state whose two regions are
// themselves parallel states with two compound regions each.
func xfinNestedParallelChart(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("first")
		b.Parallel("first", func(b *xfinBuilder) {
			b.Parallel("alpha", func(b *xfinBuilder) {
				xfinRegion[xfinNext1](b, "one")
				xfinRegion[xfinNext2](b, "two")
			})
			b.Parallel("beta", func(b *xfinBuilder) {
				xfinRegion[xfinNext3](b, "third")
				xfinRegion[xfinNext4](b, "fourth")
			})
		}).OnDone().To("done")
		b.Final("done")
	})
	require.NoError(t, err)

	return m
}

// XState: should emit a done state event for a parallel state when its parallel children reach their final states
func TestXFinParallelStateCompletesWhenItsParallelChildrenReachTheirFinalStates(t *testing.T) {
	t.Parallel()

	m := xfinNestedParallelChart(t)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = xfinSend(m, snap, xfinNext1{}, xfinNext2{}, xfinNext3{}, xfinNext4{})

	require.True(t, snap.Done())
	require.Equal(t, []string{"done"}, snap.Active())
}

// xfinMixedParallelChart is a parallel state with one region that is a
// parallel state and one that is a compound state.
func xfinMixedParallelChart(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("first")
		b.Parallel("first", func(b *xfinBuilder) {
			b.Parallel("alpha", func(b *xfinBuilder) {
				xfinRegion[xfinNext1](b, "one")
				xfinRegion[xfinNext2](b, "two")
			})
			xfinRegion[xfinNext3](b, "beta")
		}).OnDone().To("done")
		b.Final("done")
	})
	require.NoError(t, err)

	return m
}

// XState: should emit a done state event for a parallel state when its compound child reaches its final state when the other parallel child region is already in its final state
func TestXFinParallelStateCompletesWhenCompoundChildFinishesAfterParallelChild(t *testing.T) {
	t.Parallel()

	m := xfinMixedParallelChart(t)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = xfinSend(m, snap, xfinNext1{}, xfinNext2{}, xfinNext3{})

	require.True(t, snap.Done())
	require.Equal(t, []string{"done"}, snap.Active())
}

// XState: should emit a done state event for a parallel state when its parallel child reaches its final state when the other compound child region is already in its final state
func TestXFinParallelStateCompletesWhenParallelChildFinishesAfterCompoundChild(t *testing.T) {
	t.Parallel()

	m := xfinMixedParallelChart(t)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = xfinSend(m, snap, xfinNext3{}, xfinNext1{}, xfinNext2{})

	require.True(t, snap.Done())
	require.Equal(t, []string{"done"}, snap.Active())
}

// xfinImmediatelyFinalChart is XState's chart of a parallel state a whose
// regions are all final from the start: a1, a parallel state of two final
// regions, and a2, a compound state starting in a final state.
func xfinImmediatelyFinalChart(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("a")
		b.Parallel("a", func(b *xfinBuilder) {
			b.Parallel("a1", func(b *xfinBuilder) {
				b.Final("a1a")
				b.Final("a1b")
			})
			b.State("a2", func(b *xfinBuilder) {
				b.Initial("a2a")
				b.Final("a2a")
			})
		}).OnDone().To("b")
		b.Final("b")
	})
	require.NoError(t, err)

	return m
}

// XState: should reach a final state when a parallel state reaches its final state and transitions to a top-level final state in response to that
func TestXFinReachesFinalStateWhenParallelStateCompletesAtStart(t *testing.T) {
	t.Parallel()

	m := xfinImmediatelyFinalChart(t)

	snap, _ := m.Start(counter{N: 0}, epoch)

	require.True(t, snap.Done())
	require.Equal(t, []string{"b"}, snap.Active())
}

// XState: should reach a final state when a parallel state nested in a parallel state reaches its final state and transitions to a top-level final state in response to that
//
// The XState case declares the same machine as the one before it.
func TestXFinReachesFinalStateWhenNestedParallelStateCompletesAtStart(t *testing.T) {
	t.Parallel()

	m := xfinImmediatelyFinalChart(t)

	snap, _ := m.Start(counter{N: 0}, epoch)

	require.True(t, snap.Done())
	require.Equal(t, []string{"b"}, snap.Active())
}

// xfinRequireDoneWithOutput checks that a chart of regions is done from the
// start and reports what its regions do, by region name, as XState's done
// event of a parallel root does.
func xfinRequireDoneWithOutput(t *testing.T, m *sc.Machine[string, counter], want map[string]any, active ...string) {
	t.Helper()

	snap, effects := m.Start(counter{N: 0}, epoch)

	require.True(t, snap.Done())
	require.Equal(t, active, snap.Active())
	require.Equal(t, []string{"m/completed"}, xfinActions(effects))

	output, has := m.Output(snap)
	require.True(t, has)
	require.Equal(t, want, output)
}

// XState: root output should be called with a "xstate.done.state.*" event of the parallel root when a direct final child of that parallel root is reached
//
// Partial port: sc has no output on the root, so there is nothing to call
// with the done event of the parallel root, and that completion carries no
// output of the regions (XState's is {a: undefined}). What is ported is that
// the chart of regions completes at the start, once, and that Machine.Output
// then reports no output. The chart's exit action stands for XState's spy.
func TestXFinParallelRootCompletesWhenADirectFinalChildIsReached(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Regions()
		b.Chart().Exit("completed", noop)
		b.Final("a")
	})
	require.NoError(t, err)

	xfinRequireDoneWithOutput(t, m, map[string]any{"a": nil}, "a")
}

// XState: root output should be called with a "xstate.done.state.*" event of the parallel root when a final child of its compound child is reached
//
// Partial port, as the case before it.
func TestXFinParallelRootCompletesWhenAFinalChildOfItsCompoundChildIsReached(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Regions()
		b.Chart().Exit("completed", noop)
		b.State("a", func(b *xfinBuilder) {
			b.Initial("b")
			b.Final("b")
		})
	})
	require.NoError(t, err)

	xfinRequireDoneWithOutput(t, m, map[string]any{"a": nil}, "a", "b")
}

// XState: root output should be called with a "xstate.done.state.*" event of the parallel root when a final descendant is reached 2 parallel levels deep
//
// Partial port, as the two cases before it.
func TestXFinParallelRootCompletesWhenAFinalDescendantIsReachedTwoParallelLevelsDeep(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Regions()
		b.Chart().Exit("completed", noop)
		b.Parallel("a", func(b *xfinBuilder) {
			b.State("b", func(b *xfinBuilder) {
				b.Initial("c")
				b.Final("c")
			})
		})
	})
	require.NoError(t, err)

	xfinRequireDoneWithOutput(t, m, map[string]any{"a": map[string]any{"b": nil}}, "a", "b", "c")
}

// XState: onDone of an outer parallel state should be called with its own "xstate.done.state.*" event when its direct parallel child completes
//
// As in XState, the done event of a parallel state carries the outputs of
// its regions, keyed by region and nested: {b: {c: undefined}} here.
func TestXFinOuterParallelOnDoneIsTakenWhenItsDirectParallelChildCompletes(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("a")
		b.Parallel("a", func(b *xfinBuilder) {
			b.Parallel("b", func(b *xfinBuilder) {
				b.State("c", func(b *xfinBuilder) {
					b.Initial("d")
					b.Final("d")
				})
			})
		}).OnDoneWith[map[string]any]().Do("spy", func(context.Context, counter, sc.Done[map[string]any]) {})
	})
	require.NoError(t, err)

	snap, effects := m.Start(counter{N: 0}, epoch)

	require.Equal(t, 1, xfinCount(xfinActions(effects), "a/spy"))
	require.Equal(t, sc.Done[map[string]any]{ID: "a", Result: map[string]any{"b": map[string]any{"c": nil}}}, xfinEffect(t, effects, "a/spy").Event)
	require.Equal(t, []string{"a", "b", "c", "d"}, snap.Active())
}

// XState: onDone should not be called when the machine reaches its final state
func TestXFinOnDoneIsNotCalledWhenTheMachineReachesItsFinalState(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Regions()
		b.Chart().OnDone().Do("spy", xfinSpy)
		b.Parallel("a", func(b *xfinBuilder) {
			b.State("b", func(b *xfinBuilder) {
				b.Initial("c")
				b.Final("c")
			}).OnDone().Do("spy", xfinSpy)
		}).OnDone().Do("spy", xfinSpy)
	})
	require.NoError(t, err)

	snap, effects := m.Start(counter{N: 0}, epoch)

	require.True(t, snap.Done())
	require.Empty(t, xfinActions(effects))
}

// XState: machine should not complete when a parallel child of a compound state completes
func TestXFinMachineDoesNotCompleteWhenAParallelChildOfACompoundStateCompletes(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("a")
		b.Parallel("a", func(b *xfinBuilder) {
			b.State("b", func(b *xfinBuilder) {
				b.Initial("c")
				b.Final("c")
			})
		})
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)

	require.False(t, snap.Done())
	require.Equal(t, []string{"a", "b", "c"}, snap.Active())
}

// XState: root output should only be called once when multiple parallel regions complete at once
//
// Partial port: sc has no output on the root. The chart's exit action stands
// for XState's spy: it too runs when the machine completes, and once.
func TestXFinParallelRootCompletesOnceWhenSeveralRegionsCompleteAtOnce(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Regions()
		b.Chart().Exit("completed", noop)
		b.Final("a")
		b.Final("b")
	})
	require.NoError(t, err)

	xfinRequireDoneWithOutput(t, m, map[string]any{"a": nil, "b": nil}, "a", "b")
}

// XState: should require root output to produce output for a parallel root
//
// Partial port: the first half, a parallel root without a root output. The
// second half needs a root output that passes on the outputs of the regions,
// which sc has neither of.
func TestXFinParallelRootWithoutRootOutputProducesNoOutput(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Regions()
		b.Final("a").Output(func(counter) string { return "a output" })
		b.Final("b").Output(func(counter) string { return "b output" })
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	require.True(t, snap.Done())

	// DIVERGES from XState: XState's parallel root has no output unless a
	// root output function builds one from the regions' outputs. An sc
	// chart of regions has no such function and always reports its
	// regions' outputs, by region name.
	output, has := m.Output(snap)
	require.True(t, has)
	require.Equal(t, map[string]any{"a": "a output", "b": "b output"}, output)
}

// XState: onDone of a parallel state should only be called once when multiple parallel regions complete at once
func TestXFinParallelOnDoneIsTakenOnceWhenSeveralRegionsCompleteAtOnce(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("a")
		b.Parallel("a", func(b *xfinBuilder) {
			b.Final("b")
			b.Final("c")
		}).OnDone().Do("spy", xfinSpy)
	})
	require.NoError(t, err)

	_, effects := m.Start(counter{N: 0}, epoch)

	require.Equal(t, 1, xfinCount(xfinActions(effects), "a/spy"))
}

// XState: should call exit actions in reversed document order when the machines reaches its final state
//
// Partial port: XState also expects "exit: b" between the entry of b and the
// exit of the root. An sc final state has no exit actions, so that one cannot
// be declared; everything else is as XState has it.
func TestXFinExitOrderWhenTheMachineReachesItsFinalState(t *testing.T) {
	t.Parallel()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Chart().Entry("enter", noop).Exit("exit", noop)
		b.Initial("a")
		b.State("a").Entry("enter", noop).Exit("exit", noop).On[xfinNext1]().To("b")
		b.Final("b").Entry("enter", noop)
	})
	require.NoError(t, err)

	snap, effects := m.Start(counter{N: 0}, epoch)
	require.Equal(t, []string{"m/enter", "a/enter"}, xfinActions(effects))

	snap, actions := xfinSend(m, snap, xfinNext1{})

	require.True(t, snap.Done())
	require.Equal(t, []string{
		// result of the transition
		"a/exit",
		"b/enter",
		// result of reaching final states
		"m/exit",
	}, actions)
}

// xfinTrackedRegionsChart is XState's parallel root with regions a and b,
// each moving from its first child to a final one. Every state and the chart
// record their entry and, where sc allows one, their exit. wire declares the
// transitions out of child_a1 and child_b1.
func xfinTrackedRegionsChart(t *testing.T, wire func(a1, b1 *sc.StateBuilder[string, counter])) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Regions()
		b.Chart().Entry("enter", noop).Exit("exit", noop)

		var a1, b1 *sc.StateBuilder[string, counter]

		b.State("a", func(b *xfinBuilder) {
			b.Initial("child_a1")
			a1 = b.State("child_a1").Entry("enter", noop).Exit("exit", noop)
			b.Final("child_a2").Entry("enter", noop)
		}).Entry("enter", noop).Exit("exit", noop)
		b.State("b", func(b *xfinBuilder) {
			b.Initial("child_b1")
			b1 = b.State("child_b1").Entry("enter", noop).Exit("exit", noop)
			b.Final("child_b2").Entry("enter", noop)
		}).Entry("enter", noop).Exit("exit", noop)

		wire(a1, b1)
	})
	require.NoError(t, err)

	return m
}

// xfinWireSeparateEvents moves region a on xfinNext2 and region b on
// xfinNext1, as XState's EV2 and EV1 do.
func xfinWireSeparateEvents(a1, b1 *sc.StateBuilder[string, counter]) {
	a1.On[xfinNext2]().To("child_a2")
	b1.On[xfinNext1]().To("child_b2")
}

// XState: should call exit actions of parallel states in reversed document order when the machines reaches its final state after earlier region transition
//
// Partial port: XState also expects "exit: b.child_b2" before "exit: b" and
// "exit: a.child_a2" before "exit: a". An sc final state has no exit actions,
// so those two cannot be declared; everything else is as XState has it.
func TestXFinExitOrderOfParallelStatesAfterEarlierRegionTransition(t *testing.T) {
	t.Parallel()

	m := xfinTrackedRegionsChart(t, xfinWireSeparateEvents)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = xfinSend(m, snap, xfinNext1{})
	snap, actions := xfinSend(m, snap, xfinNext2{})

	require.True(t, snap.Done())
	require.Equal(t, []string{
		// result of the transition
		"child_a1/exit",
		"child_a2/enter",
		// result of reaching final states
		"b/exit",
		"a/exit",
		"m/exit",
	}, actions)
}

// XState: should call exit actions of parallel states in reversed document order when the machines reaches its final state after later region transition
//
// The XState case declares the same machine and sends the same events as the
// one before it; see there for what is ported.
func TestXFinExitOrderOfParallelStatesAfterLaterRegionTransition(t *testing.T) {
	t.Parallel()

	m := xfinTrackedRegionsChart(t, xfinWireSeparateEvents)

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = xfinSend(m, snap, xfinNext1{})
	snap, actions := xfinSend(m, snap, xfinNext2{})

	require.True(t, snap.Done())
	require.Equal(t, []string{
		"child_a1/exit",
		"child_a2/enter",
		"b/exit",
		"a/exit",
		"m/exit",
	}, actions)
}

// XState: should call exit actions of parallel states in reversed document order when the machines reaches its final state after multiple regions transition
//
// Partial port, as the two cases before it.
func TestXFinExitOrderOfParallelStatesAfterMultipleRegionsTransition(t *testing.T) {
	t.Parallel()

	m := xfinTrackedRegionsChart(t, func(a1, b1 *sc.StateBuilder[string, counter]) {
		a1.On[xfinNext1]().To("child_a2")
		b1.On[xfinNext1]().To("child_b2")
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, actions := xfinSend(m, snap, xfinNext1{})

	require.True(t, snap.Done())
	require.Equal(t, []string{
		// result of the transition
		"child_b1/exit",
		"child_a1/exit",
		"child_a2/enter",
		"child_b2/enter",
		// result of reaching final states
		"b/exit",
		"a/exit",
		"m/exit",
	}, actions)
}

// xfinPartlyFinalChart is XState's parallel root with region A in its final
// state from the start and region B not. direct declares A as a final state
// directly in the root; otherwise A is a compound state starting in a final
// state. As in XState nothing reaches B2.
func xfinPartlyFinalChart(t *testing.T, direct bool) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Regions()
		if direct {
			b.Final("A")
		} else {
			b.State("A", func(b *xfinBuilder) {
				b.Initial("A1")
				b.Final("A1")
			})
		}
		b.State("B", func(b *xfinBuilder) {
			b.Initial("B1")
			b.State("B1")
			b.Final("B2")
		})
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	return m
}

// XState: should not complete a parallel root immediately when only some of its regions are in their final states (final state reached in a compound region)
func TestXFinParallelStateDoesNotCompleteWhenOnlySomeRegionsAreFinalCompoundRegion(t *testing.T) {
	t.Parallel()

	m := xfinPartlyFinalChart(t, false)

	snap, _ := m.Start(counter{N: 0}, epoch)

	require.False(t, snap.Done())
	require.Equal(t, []string{"A", "A1", "B", "B1"}, snap.Active())
}

// XState: should not complete a parallel root immediately when only some of its regions are in their final states (a direct final child state reached)
func TestXFinParallelStateDoesNotCompleteWhenOnlySomeRegionsAreFinalDirectFinalChild(t *testing.T) {
	t.Parallel()

	m := xfinPartlyFinalChart(t, true)

	snap, _ := m.Start(counter{N: 0}, epoch)

	require.False(t, snap.Done())
	require.Equal(t, []string{"A", "B", "B1"}, snap.Active())
}

// XState: should not resolve output of a final state if its parent is a parallel state
func TestXFinDoesNotResolveOutputOfAFinalStateWhoseParentIsAParallelState(t *testing.T) {
	t.Parallel()

	calls := 0
	m, err := sc.New("m", func(b *xfinBuilder) {
		b.Initial("A")
		b.Parallel("A", func(b *xfinBuilder) {
			b.Final("B").Output(func(counter) string { calls++; return "B output" })
			b.State("C", func(b *xfinBuilder) {
				b.Initial("C1")
				b.State("C1")
			})
		})
	})
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)

	require.Equal(t, []string{"A", "B", "C", "C1"}, snap.Active())
	require.Zero(t, calls)
}

// xfinCancelChild runs XState's parent chart with child as the machine its
// start state invokes, has the child cancelled, and returns the states the
// parent is in afterwards. It must be called inside a synctest bubble.
//
// XState sends CANCEL to the child's actor directly, where the sc parent
// forwards it.
func xfinCancelChild(t *testing.T, child *sc.Machine[string, counter]) []string {
	t.Helper()

	parent, err := sc.New("parent", func(b *xfinBuilder) {
		b.Initial("start")
		start := b.State("start")
		invoked := start.InvokeMachine("child", child,
			func(c counter) counter { return c },
			func(sc.Snapshot[string, counter]) (struct{}, error) { return struct{}{}, nil },
		)
		invoked.OnDone().To("completed")
		invoked.OnError().To("failed")
		start.On[xfinChildCanceled]().To("canceled")
		start.On[xfinCancel]().SendTo("forward", "child", func(_ counter, ev xfinCancel) any { return ev })
		b.State("canceled")
		b.State("completed")
		b.State("failed")
	})
	require.NoError(t, err)

	inst, err := parent.Instance(t.Context(), counter{N: 0})
	require.NoError(t, err)
	defer inst.Close()

	synctest.Wait()

	_, err = inst.Send(t.Context(), xfinCancel{})
	require.NoError(t, err)

	synctest.Wait()

	return inst.Snapshot().Active()
}

// xfinNotifyParent is what a child tells its parent as it is cancelled.
func xfinNotifyParent(counter) xfinChildCanceled { return xfinChildCanceled{} }

// XState: should only call exit actions once when a child machine reaches its final state and sends an event to its parent that ends up stopping that child
func TestXFinCallsExitActionsOnceWhenAChildMachineFinishesAndIsStoppedByItsParent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var exits atomic.Int32

		child, err := sc.New("child", func(b *xfinBuilder) {
			b.Chart().Exit("spy", func(context.Context, counter) { exits.Add(1) })
			b.Initial("start")
			b.State("start").On[xfinCancel]().To("canceled")
			b.Final("canceled").EmitOnEntry("notify", xfinNotifyParent)
		})
		require.NoError(t, err)

		// Not asserted by XState: the parent took the child's event.
		require.Equal(t, []string{"canceled"}, xfinCancelChild(t, child))
		require.Equal(t, int32(1), exits.Load())
	})
}

// XState: should deliver final outgoing events (from final entry action) to the parent before delivering the `xstate.done.actor.*` event
func TestXFinDeliversFinalOutgoingEventsToTheParentBeforeTheDoneEvent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		child, err := sc.New("child", func(b *xfinBuilder) {
			b.Initial("start")
			b.State("start").On[xfinCancel]().To("canceled")
			b.Final("canceled").EmitOnEntry("notify", xfinNotifyParent)
		})
		require.NoError(t, err)

		// If the child's completion were delivered first, the parent
		// would be in completed.
		require.Equal(t, []string{"canceled"}, xfinCancelChild(t, child))
	})
}

// XState: should deliver final outgoing events (from root exit action) to the parent before delivering the `xstate.done.actor.*` event
func TestXFinDeliversEventsOfTheRootExitToTheParentBeforeTheDoneEvent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		child, err := sc.New("child", func(b *xfinBuilder) {
			b.Chart().EmitOnExit("notify", xfinNotifyParent)
			b.Initial("start")
			b.State("start").On[xfinCancel]().To("canceled")
			b.Final("canceled")
		})
		require.NoError(t, err)

		// If the child's completion were delivered first, the parent
		// would be in completed.
		require.Equal(t, []string{"canceled"}, xfinCancelChild(t, child))
	})
}

// XState: should be possible to complete with a null output (resolving with final state's output)
//
// XState's root output passes the final state's output on unchanged, which
// is what Machine.Output does without one.
func TestXFinCompletesWithANilOutputOfTheFinalState(t *testing.T) {
	t.Parallel()

	m := xfinStartEndChart(t, func(end *sc.FinalBuilder[string, counter]) {
		end.Output(func(counter) any { return nil })
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = xfinSend(m, snap, xfinFinish{Value: 0})

	output, has := m.Output(snap)
	require.True(t, has)
	require.Nil(t, output)
}

// XState: does not warn for plain final states
//
// sc has no warnings; a chart it objects to is a declaration error. The port
// declares XState's chart and checks whether sc accepts it.
//
// DIVERGES from XState: XState accepts the chart without complaint although
// nothing reaches its top-level final state "done". sc rejects a state
// nothing can reach, so the chart as XState declares it is an error unless
// it is declared with the AllowUnreachable option.
func TestXFinPlainFinalStatesAreAccepted(t *testing.T) {
	t.Parallel()

	chart := func(b *xfinBuilder) {
		b.Initial("a")
		b.State("a", func(b *xfinBuilder) {
			b.Initial("inner")
			b.Final("inner")
		})
		b.Final("done")
	}

	_, err := sc.New("m", chart)
	require.ErrorContains(t, err, `state "done": unreachable`)

	m, err := sc.New("m", chart, sc.AllowUnreachable())
	require.NoError(t, err)

	snap, _ := m.Start(counter{N: 0}, epoch)
	require.Equal(t, []string{"a", "inner"}, snap.Active())
	require.False(t, snap.Done())
}

// XState cases that are not ported, by reason.
//
// The root of an sc chart cannot itself be a final state:
//   - status of a machine with a root state being final should be done
//   - output of a machine with a root state being final should receive its state ID
//
// sc has no output on the root: nothing maps the output of the final state
// reached, and a machine has no output when that state declares none:
//   - should pass top-level final state output to root output mapper
//   - should keep root output-only behavior unchanged
//   - output mapper should receive self
//   - should be possible to complete with a null output (directly on root)
//
// An sc final state can declare only tags, an output and what it does on
// entry, so it cannot have the transitions or services these cases are
// about, and sc has no warnings:
//   - warns when a top-level final state declares invoke, on or after
//   - does not start actors invoked by a top-level final state
//   - warns when a nested or parallel-region final state declares on
//   - final regions under a parallel state take no transitions and start no actors
