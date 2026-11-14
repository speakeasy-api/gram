package sc_test

// Ports of XState's persistence suites (6.0.0-beta.2), to check sc's
// semantics of restoring a stored snapshot against XState's:
//
//   - packages/core/test/rehydration.test.ts
//   - packages/core/test/restoreEventless.test.ts
//   - packages/core/test/restoreTerminal.test.ts
//   - packages/core/test/restored-effects.test.ts
//   - packages/core/test/timers.persistence.test.ts
//   - packages/core/test/tags.test.ts
//
// Each test names the XState case it ports. Cases that test an XState-only
// API are listed at the end of the file with the reason they were not
// ported. A comment starting "DIVERGES from XState" marks each place sc
// behaves differently.
//
// How XState's persistence API maps onto sc's:
//
//   - getPersistedSnapshot is json.Marshal of the snapshot, and restoreActor
//     is Machine.Restore. Starting the restored actor is Machine.InstanceFrom,
//     or Machine.Resume in the pure core.
//   - machine.resolveState({value}) is Machine.StartIn.
//   - XState keeps a child actor's snapshot under "children" in its parent's.
//     sc keeps a child machine's snapshot inside the parent's too, and a
//     test reads it with Snapshot.Child. sc has no reference to a running
//     child to send to, so a test sends to a child through a parent
//     transition's SendTo.
//   - XState's root-level entry, exit, invoke and on are declared on the
//     chart itself with Builder.Chart, and a root with type: 'parallel' with
//     Builder.Regions. In effects and in the stored snapshot the chart goes
//     by the machine's id.
//   - Where XState's machine is a root with no states, so is the sc chart,
//     and nothing is active in it.
//   - State identifiers are unique in an sc chart and a snapshot lists the
//     active states by identifier, so XState's nested state values and state
//     paths ("active.reviewing") become flat lists of names.
//   - sc rejects a state nothing can reach, so a chart in which XState
//     leaves a state unreachable is declared with sc.AllowUnreachable.

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

type (
	xrehNext       struct{}
	xrehFoo        struct{}
	xrehInc        struct{}
	xrehGo         struct{}
	xrehStop       struct{}
	xrehApprove    struct{}
	xrehPing       struct{}
	xrehPingDue    struct{}
	xrehSchedule   struct{}
	xrehBegin      struct{}
	xrehReenter    struct{}
	xrehTimer      struct{}
	xrehDeactivate struct{}
	xrehUnmatched  struct{}
	xrehValue      struct{ N int }
)

// xrehFlag is the context of the charts ported from restoreEventless.test.ts.
type xrehFlag struct {
	Go bool `json:"go"`
}

type (
	xrehBuilder = sc.Builder[string, counter]
	xrehMachine = sc.Machine[string, counter]
	xrehSnap    = sc.Snapshot[string, counter]
	xrehInst    = sc.Instance[string, counter]
)

// xrehLog records the values an action saw, in order.
type xrehLog struct {
	mu   sync.Mutex
	vals []int
}

func (l *xrehLog) add(v int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.vals = append(l.vals, v)
}

func (l *xrehLog) list() []int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([]int(nil), l.vals...)
}

func xrehNew(t *testing.T, id string, def func(b *xrehBuilder), opts ...sc.Option) *xrehMachine {
	t.Helper()

	m, err := sc.New(id, def, opts...)
	require.NoError(t, err)

	return m
}

// xrehPersist is XState's JSON.stringify(actor.getPersistedSnapshot()).
func xrehPersist[S comparable, C any](t *testing.T, snap sc.Snapshot[S, C]) []byte {
	t.Helper()

	data, err := json.Marshal(snap)
	require.NoError(t, err)

	return data
}

// xrehRaw decodes a stored snapshot into its stored form.
func xrehRaw(t *testing.T, data []byte) sc.RawSnapshot {
	t.Helper()

	var raw sc.RawSnapshot
	require.NoError(t, json.Unmarshal(data, &raw))

	return raw
}

// xrehTamper returns the stored snapshot with edit applied to it.
func xrehTamper(t *testing.T, data []byte, edit func(raw *sc.RawSnapshot)) []byte {
	t.Helper()

	raw := xrehRaw(t, data)
	edit(&raw)

	out, err := json.Marshal(raw)
	require.NoError(t, err)

	return out
}

// xrehFields returns the stored snapshot with edit applied to its top-level
// JSON fields, for changes the stored form's Go types cannot hold.
func xrehFields(t *testing.T, data []byte, edit func(fields map[string]json.RawMessage)) []byte {
	t.Helper()

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &fields))
	edit(fields)

	out, err := json.Marshal(fields)
	require.NoError(t, err)

	return out
}

// xrehRestore restores a stored snapshot that must need no reconciling.
func xrehRestore[S comparable, C any](t *testing.T, m *sc.Machine[S, C], data []byte, now time.Time) sc.Snapshot[S, C] {
	t.Helper()

	snap, effects, err := m.Restore(data, now)
	require.NoError(t, err)
	require.Empty(t, effects)

	return snap
}

// xrehChildOf is XState's snapshot.children[service].getSnapshot(): the
// snapshot of the child machine that parent's service runs.
func xrehChildOf(t *testing.T, child *xrehMachine, parent xrehSnap, service string) xrehSnap {
	t.Helper()

	snap, ok := parent.Child(child, service)
	require.True(t, ok, "service %q has no child snapshot", service)

	return snap
}

// xrehSend sends ev and waits until everything it caused has come to rest.
// It must be called inside a synctest bubble.
func xrehSend(t *testing.T, inst *xrehInst, ev any) sc.Step[string, counter] {
	t.Helper()

	step, err := inst.Send(t.Context(), ev)
	require.NoError(t, err)
	synctest.Wait()

	return step
}

// xrehRestart stops inst as a process stopping would, stores its snapshot,
// and continues a new instance from the stored form. It returns the new
// instance with the stored snapshot. It must be called inside a synctest
// bubble.
func xrehRestart(t *testing.T, m *xrehMachine, inst *xrehInst) (*xrehInst, []byte) {
	t.Helper()

	synctest.Wait()
	inst.Close()
	data := xrehPersist(t, inst.Snapshot())

	next, err := m.InstanceFrom(t.Context(), xrehRestore(t, m, data, time.Now()))
	require.NoError(t, err)
	synctest.Wait()

	return next, data
}

// xrehBlock is a service that runs until it is stopped.
func xrehBlock(ctx context.Context, _ counter) (int, error) {
	<-ctx.Done()

	return 0, fmt.Errorf("stopped: %w", context.Cause(ctx))
}

// xrehPass lets d pass on the fake clock of the synctest bubble it is
// called in, and waits until everything that woke up has come to rest.
func xrehPass(d time.Duration) {
	time.Sleep(d) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
	synctest.Wait()
}

// xrehOutput is the result of a child machine: its count.
func xrehOutput(snap xrehSnap) (int, error) { return snap.Context.N, nil }

// xrehCounting is XState's counter step logic as a child machine: a count
// that starts where its parent says and grows by step on xrehInc.
func xrehCounting(t *testing.T, id string, step int) *xrehMachine {
	t.Helper()

	return xrehNew(t, id, func(b *xrehBuilder) {
		b.Initial("counting")
		b.State("counting").On[xrehInc]().Assign("add", func(c counter, _ xrehInc) counter { c.N += step; return c })
	})
}

// xrehInvoke makes st run child as the service name, starting it at start.
func xrehInvoke(st *sc.StateBuilder[string, counter], name string, child *xrehMachine, start int) {
	inv := st.InvokeMachine(name, child, func(counter) counter { return counter{N: start} }, xrehOutput)
	inv.OnError().Stay()
}

// xrehForward makes st pass xrehInc on to its service name.
func xrehForward(st *sc.StateBuilder[string, counter], name string) {
	st.On[xrehInc]().SendTo("forward", name, func(_ counter, ev xrehInc) any { return ev })
}

// ---------------------------------------------------------------------------
// rehydration.test.ts
// ---------------------------------------------------------------------------

// rehydration.test.ts: rehydration > using persisted state > should be able
// to use `hasTag` immediately.
func TestXRehPersistedStateHasTagImmediately(t *testing.T) {
	t.Parallel()

	m := xrehNew(t, "tagged", func(b *xrehBuilder) {
		b.Initial("a")
		b.State("a").Tag("foo")
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	restored := xrehRestore(t, m, xrehPersist(t, snap), epoch)

	require.True(t, restored.HasTag("foo"))
}

// rehydration.test.ts: rehydration > using persisted state > should not call
// exit actions when machine gets stopped immediately.
func TestXRehPersistedStateStoppedImmediatelyRunsNoExitActions(t *testing.T) {
	t.Parallel()

	var exits atomic.Int32
	m := xrehNew(t, "exits", func(b *xrehBuilder) {
		b.Chart().Exit("root", func(context.Context, counter) { exits.Add(1) })
		b.Initial("a")
		b.State("a").Exit("a", func(context.Context, counter) { exits.Add(1) })
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	restored := xrehRestore(t, m, xrehPersist(t, snap), epoch)

	inst, err := m.InstanceFrom(t.Context(), restored)
	require.NoError(t, err)
	inst.Close()

	require.Zero(t, exits.Load())
}

// rehydration.test.ts: rehydration > using persisted state > should get
// correct result back from `can` immediately.
//
// As in XState, the machine has no states.
func TestXRehPersistedStateCanImmediately(t *testing.T) {
	t.Parallel()

	m := xrehNew(t, "can", func(b *xrehBuilder) {
		b.On[xrehFoo]().Do("act", func(context.Context, counter, xrehFoo) {})
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	restored := xrehRestore(t, m, xrehPersist(t, snap), epoch)

	require.True(t, m.Can(restored, xrehFoo{}, epoch))
}

// xrehActiveChart is the chart of the "using state value" cases.
func xrehActiveChart(t *testing.T, exits *atomic.Int32) *xrehMachine {
	t.Helper()

	return xrehNew(t, "value", func(b *xrehBuilder) {
		b.Chart().Exit("root", func(context.Context, counter) { exits.Add(1) })
		b.Initial("inactive")
		b.State("inactive").On[xrehNext]().To("active")
		b.State("active").Tag("foo").Exit("active", func(context.Context, counter) { exits.Add(1) })
	})
}

// rehydration.test.ts: rehydration > using state value > should be able to
// use `hasTag` immediately.
func TestXRehStateValueHasTagImmediately(t *testing.T) {
	t.Parallel()

	var exits atomic.Int32
	m := xrehActiveChart(t, &exits)

	snap, effects, err := m.StartIn(counter{N: 0}, epoch, "active")
	require.NoError(t, err)
	require.Empty(t, effects)

	require.True(t, snap.HasTag("foo"))
}

// rehydration.test.ts: rehydration > using state value > should not call
// exit actions when machine gets stopped immediately.
func TestXRehStateValueStoppedImmediatelyRunsNoExitActions(t *testing.T) {
	t.Parallel()

	var exits atomic.Int32
	m := xrehActiveChart(t, &exits)

	snap, _, err := m.StartIn(counter{N: 0}, epoch, "active")
	require.NoError(t, err)

	inst, err := m.InstanceFrom(t.Context(), snap)
	require.NoError(t, err)
	inst.Close()

	require.Zero(t, exits.Load())
}

// rehydration.test.ts: rehydration > using state value > should error on
// incompatible state value (shallow).
func TestXRehStateValueErrorsOnIncompatibleShallowValue(t *testing.T) {
	t.Parallel()

	m := xrehNew(t, "shallow", func(b *xrehBuilder) {
		b.Initial("valid")
		b.State("valid")
	})

	_, _, err := m.StartIn(counter{N: 0}, epoch, "invalid")

	require.ErrorIs(t, err, sc.ErrInvalidSnapshot)
	require.ErrorContains(t, err, "invalid")
}

// rehydration.test.ts: rehydration > using state value > should error on
// incompatible state value (deep).
//
// A state of an sc chart is named by its identifier alone, so XState's
// {parent: 'invalid'} is the leaf "invalid".
func TestXRehStateValueErrorsOnIncompatibleDeepValue(t *testing.T) {
	t.Parallel()

	m := xrehNew(t, "deep", func(b *xrehBuilder) {
		b.Initial("parent")
		b.State("parent", func(b *xrehBuilder) {
			b.Initial("valid")
			b.State("valid")
		})
	})

	_, _, err := m.StartIn(counter{N: 0}, epoch, "invalid")

	require.ErrorIs(t, err, sc.ErrInvalidSnapshot)
	require.ErrorContains(t, err, "invalid")
}

// rehydration.test.ts: rehydration > should not replay actions when starting
// from a persisted state.
//
// As in XState, the machine has no states.
func TestXRehDoesNotReplayActionsFromAPersistedState(t *testing.T) {
	t.Parallel()

	var entries atomic.Int32
	m := xrehNew(t, "entry", func(b *xrehBuilder) {
		b.Chart().Entry("spy", func(context.Context, counter) { entries.Add(1) })
	})

	inst, err := m.Instance(t.Context(), counter{N: 0})
	require.NoError(t, err)
	require.Equal(t, int32(1), entries.Load())

	inst.Close()
	restored := xrehRestore(t, m, xrehPersist(t, inst.Snapshot()), epoch)
	require.Empty(t, m.Resume(restored))

	inst, err = m.InstanceFrom(t.Context(), restored)
	require.NoError(t, err)
	defer inst.Close()

	require.Equal(t, int32(1), entries.Load())
}

// rehydration.test.ts: rehydration > should be able to stop a rehydrated
// child.
func TestXRehStopsARehydratedChild(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var started, stopped atomic.Int32
		m := xrehNew(t, "stoppable", func(b *xrehBuilder) {
			b.Initial("a")
			a := b.State("a")
			inv := a.Invoke("work", func(ctx context.Context, _ counter) (int, error) {
				started.Add(1)
				<-ctx.Done()
				stopped.Add(1)

				return 0, context.Cause(ctx)
			})
			inv.OnDone().To("b")
			inv.OnError().To("b")
			a.On[xrehNext]().To("c")
			b.State("b")
			b.State("c")
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)

		inst, _ = xrehRestart(t, m, inst)
		defer inst.Close()

		// The service runs again in the rehydrated instance.
		require.Equal(t, int32(2), started.Load())
		require.Equal(t, int32(1), stopped.Load())

		step := xrehSend(t, inst, xrehNext{})

		require.Equal(t, []string{"c"}, step.To.Active())
		require.Equal(t, int32(2), stopped.Load())
		require.Empty(t, inst.Snapshot().Services())
	})
}

// rehydration.test.ts: rehydration > a rehydrated done child should not
// re-notify the parent about its completion.
//
// XState spawns the child from the context initialiser, which sc cannot do,
// so the chart invokes the child instead; the system registration of the
// original is not ported. As in XState, the machine has no states.
func TestXRehADoneChildDoesNotRenotifyItsParent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		child := xrehNew(t, "finished", func(b *xrehBuilder) {
			b.Initial("end")
			b.Final("end")
		})

		var notified atomic.Int32
		m := xrehNew(t, "notified", func(b *xrehBuilder) {
			chart := b.Chart()
			inv := chart.InvokeMachine("foo", child, func(c counter) counter { return c }, xrehOutput)
			inv.OnDone().Do("spy", func(context.Context, counter, sc.Done[int]) { notified.Add(1) })
			inv.OnError().Do("spyError", func(context.Context, counter, sc.Failed) { notified.Add(1) })
			chart.OnAny().Do("spyAny", func(context.Context, counter, any) { notified.Add(1) })
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		synctest.Wait()

		// The child was done at once and the parent was told, once.
		require.Equal(t, int32(1), notified.Load())
		require.Empty(t, inst.Snapshot().Services())

		inst, _ = xrehRestart(t, m, inst)
		defer inst.Close()

		require.Equal(t, int32(1), notified.Load())
	})
}

// rehydration.test.ts: rehydration > should be possible to persist a
// rehydrated actor that got its children rehydrated.
//
// As in XState, the machine has no states.
func TestXRehPersistsARehydratedActorWithItsRehydratedChildren(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xrehNew(t, "children", func(b *xrehBuilder) {
			b.Chart().Invoke("foo", xrehBlock).OnError().Stay()
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)

		inst, first := xrehRestart(t, m, inst)
		defer inst.Close()

		again := xrehRaw(t, xrehPersist(t, inst.Snapshot()))

		require.Len(t, again.Invocations, 1)
		require.Equal(t, "foo", again.Invocations[0].Name)
		require.Equal(t, "children", again.Invocations[0].State)
		require.Equal(t, xrehRaw(t, first).Invocations, again.Invocations)
	})
}

// rehydration.test.ts: rehydration > should complete on a rehydrated final
// state.
func TestXRehCompletesOnARehydratedFinalState(t *testing.T) {
	t.Parallel()

	m := xrehNew(t, "final", func(b *xrehBuilder) {
		b.Initial("foo")
		b.State("foo").On[xrehNext]().To("bar")
		b.Final("bar")
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, _ = m.Transition(snap, xrehNext{}, epoch)

	restored := xrehRestore(t, m, xrehPersist(t, snap), epoch)
	require.True(t, restored.Done())

	// A driver waiting for the instance to finish is told at once.
	inst, err := m.InstanceFrom(t.Context(), restored)
	require.NoError(t, err)
	defer inst.Close()

	done, err := inst.Await(t.Context(), func(s xrehSnap) bool { return s.Done() })
	require.NoError(t, err)
	require.Equal(t, []string{"bar"}, done.Active())
}

// rehydration.test.ts: rehydration > shouldn't re-notify the parent about
// the error when rehydrating.
//
// As in XState, the machine has no states.
func TestXRehDoesNotRenotifyTheParentAboutAnErrorWhenRehydrating(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var runs, notified atomic.Int32
		m := xrehNew(t, "failing", func(b *xrehBuilder) {
			inv := b.Chart().Invoke("failure", func(context.Context, counter) (int, error) {
				runs.Add(1)

				return 0, errFlaky
			})
			inv.OnError().Do("spy", func(context.Context, counter, sc.Failed) { notified.Add(1) })
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		synctest.Wait()
		require.Equal(t, int32(1), notified.Load())

		inst, data := xrehRestart(t, m, inst)
		defer inst.Close()

		// The failed run is not in the stored snapshot, so it neither
		// runs nor reports again.
		require.Empty(t, xrehRaw(t, data).Invocations)
		require.Equal(t, int32(1), runs.Load())
		require.Equal(t, int32(1), notified.Load())
	})
}

// rehydration.test.ts: rehydration > should continue syncing snapshots.
//
// XState's observable is an sc stream service, and each value it emits is
// an event of the chart. As in XState, the first actor is never started:
// only its initial snapshot is stored. As in XState, the machine has no
// states.
func TestXRehContinuesSyncingSnapshots(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		values := make(chan int)
		seen := &xrehLog{mu: sync.Mutex{}, vals: nil}

		m := xrehNew(t, "syncing", func(b *xrehBuilder) {
			chart := b.Chart()
			chart.Stream("service", func(ctx context.Context, _ counter) iter.Seq2[any, error] {
				return func(yield func(any, error) bool) {
					for {
						select {
						case v := <-values:
							if !yield(xrehValue{N: v}, nil) {
								return
							}
						case <-ctx.Done():
							return
						}
					}
				}
			}).OnError().Stay()
			chart.On[xrehValue]().Do("spy", func(_ context.Context, _ counter, ev xrehValue) { seen.add(ev.N) })
		})

		snap, _ := m.Start(counter{N: 0}, time.Now())
		restored := xrehRestore(t, m, xrehPersist(t, snap), time.Now())

		inst, err := m.InstanceFrom(t.Context(), restored)
		require.NoError(t, err)
		defer inst.Close()

		values <- 42
		values <- 100
		synctest.Wait()

		require.Equal(t, []int{42, 100}, seen.list())
	})
}

// rehydration.test.ts: rehydration > should be able to rehydrate an actor
// deep in the tree.
//
// As in XState, none of the three machines has states.
func TestXRehRehydratesAnActorDeepInTheTree(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		grandchild := xrehCounting(t, "grandchild", 1)

		child := xrehNew(t, "child", func(b *xrehBuilder) {
			xrehInvoke(b.Chart(), "grandchild", grandchild, 0)
			xrehForward(b.Chart(), "grandchild")
		})

		m := xrehNew(t, "root", func(b *xrehBuilder) {
			xrehInvoke(b.Chart(), "child", child, 0)
			xrehForward(b.Chart(), "child")
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		synctest.Wait()

		xrehSend(t, inst, xrehInc{})
		inst.Close()

		// As in XState, the restored actor is not started: the
		// grandchild's count is read from the stored snapshot alone.
		restored := xrehRestore(t, m, xrehPersist(t, inst.Snapshot()), time.Now())
		restoredChild := xrehChildOf(t, child, restored, "child")
		restoredGrandchild := xrehChildOf(t, grandchild, restoredChild, "grandchild")

		require.Equal(t, 1, restoredGrandchild.Context.N)
	})
}

// xrehKeep is a migration that changes nothing.
func xrehKeep(old sc.RawSnapshot) (sc.RawSnapshot, error) { return old, nil }

// xrehRequireRenamed checks what restoring data onto a chart that renamed
// the state missing reports: the chart built by renamed is the same chart
// with or without a version change.
func xrehRequireRenamed(t *testing.T, data []byte, renamed func(opts ...sc.Option) *xrehMachine, missing string) {
	t.Helper()

	_, _, err := renamed().Restore(data, epoch)
	require.ErrorIs(t, err, sc.ErrChartChanged)
	require.NotErrorIs(t, err, sc.ErrInvalidSnapshot)
	require.NotContains(t, err.Error(), missing)

	_, _, err = renamed(sc.Version(1), sc.Migrate(xrehKeep)).Restore(data, epoch)
	require.ErrorIs(t, err, sc.ErrInvalidSnapshot)
	require.ErrorContains(t, err, fmt.Sprintf("%q is not a state of the chart", missing))
}

// rehydration.test.ts: rehydration > persisted state value validation >
// should error with a descriptive message when the persisted state value
// references a top-level state that no longer exists.
func TestXRehErrorsWhenATopLevelStateNoLongerExists(t *testing.T) {
	t.Parallel()

	before := xrehNew(t, "order-approval", func(b *xrehBuilder) {
		b.Initial("reviewing")
		b.State("reviewing")
		b.State("approved")
	}, sc.AllowUnreachable())

	renamed := func(opts ...sc.Option) *xrehMachine {
		return xrehNew(t, "order-approval", func(b *xrehBuilder) {
			b.Initial("awaitingApproval")
			b.State("awaitingApproval")
			b.State("approved")
		}, append(opts, sc.AllowUnreachable())...)
	}

	snap, _ := before.Start(counter{N: 0}, epoch)

	// DIVERGES from XState: XState reports "Persisted snapshot references
	// state 'reviewing' which does not exist on machine 'order-approval'".
	// sc compares the chart's fingerprint first and rejects the snapshot
	// with ErrChartChanged, which does not name the state. Only a chart
	// that raised its version and migrates reaches the check that names
	// the missing state, as ErrInvalidSnapshot.
	xrehRequireRenamed(t, xrehPersist(t, snap), renamed, "reviewing")
}

// rehydration.test.ts: rehydration > persisted state value validation >
// should error with the full state path when a nested state no longer
// exists.
func TestXRehErrorsWhenANestedStateNoLongerExists(t *testing.T) {
	t.Parallel()

	before := xrehNew(t, "order-approval", func(b *xrehBuilder) {
		b.Initial("active")
		b.State("active", func(b *xrehBuilder) {
			b.Initial("reviewing")
			b.State("reviewing")
			b.State("done")
		})
	}, sc.AllowUnreachable())

	renamed := func(opts ...sc.Option) *xrehMachine {
		return xrehNew(t, "order-approval", func(b *xrehBuilder) {
			b.Initial("active")
			b.State("active", func(b *xrehBuilder) {
				b.Initial("awaitingApproval")
				b.State("awaitingApproval")
				b.State("done")
			})
		}, append(opts, sc.AllowUnreachable())...)
	}

	snap, _ := before.Start(counter{N: 0}, epoch)

	// DIVERGES from XState: XState reports the missing state by its path,
	// 'active.reviewing'. sc rejects the snapshot with ErrChartChanged
	// before it looks at the states; once the chart's version is raised
	// and a migration is given, it reports ErrInvalidSnapshot naming the
	// state by its identifier, "reviewing", because sc states have no
	// paths.
	xrehRequireRenamed(t, xrehPersist(t, snap), renamed, "reviewing")
}

// rehydration.test.ts: rehydration > persisted state value validation >
// should error when a parent of a nested state value no longer exists.
func TestXRehErrorsWhenAParentOfANestedStateNoLongerExists(t *testing.T) {
	t.Parallel()

	nested := func(parent string) func(opts ...sc.Option) *xrehMachine {
		return func(opts ...sc.Option) *xrehMachine {
			return xrehNew(t, "order-approval", func(b *xrehBuilder) {
				b.Initial(parent)
				b.State(parent, func(b *xrehBuilder) {
					b.Initial("reviewing")
					b.State("reviewing")
				})
			}, opts...)
		}
	}

	snap, _ := nested("active")().Start(counter{N: 0}, epoch)

	// DIVERGES from XState: XState reports "Persisted snapshot references
	// state 'active' which does not exist". sc rejects the snapshot with
	// ErrChartChanged before it looks at the states; once the chart's
	// version is raised and a migration is given, it reports
	// ErrInvalidSnapshot naming "active".
	xrehRequireRenamed(t, xrehPersist(t, snap), nested("running"), "active")
}

// xrehRegionsChart is XState's parallel-rooted order-approval machine, with
// the state of its review region named review.
func xrehRegionsChart(t *testing.T, review string, opts ...sc.Option) *xrehMachine {
	t.Helper()

	return xrehNew(t, "order-approval", func(b *xrehBuilder) {
		b.Regions()
		b.State("review", func(b *xrehBuilder) {
			b.Initial(review)
			b.State(review)
		})
		b.State("payment", func(b *xrehBuilder) {
			b.Initial("pending")
			b.State("pending")
		})
	}, opts...)
}

// rehydration.test.ts: rehydration > persisted state value validation >
// should error when a region of a parallel state value no longer exists.
func TestXRehErrorsWhenARegionStateNoLongerExists(t *testing.T) {
	t.Parallel()

	snap, _ := xrehRegionsChart(t, "reviewing").Start(counter{N: 0}, epoch)

	renamed := func(opts ...sc.Option) *xrehMachine { return xrehRegionsChart(t, "awaitingApproval", opts...) }

	// DIVERGES from XState: XState reports the missing state by its path,
	// 'review.reviewing'. sc rejects the snapshot with ErrChartChanged
	// before it looks at the states; once the chart's version is raised
	// and a migration is given, it reports ErrInvalidSnapshot naming the
	// state by its identifier, "reviewing".
	xrehRequireRenamed(t, xrehPersist(t, snap), renamed, "reviewing")
}

// xrehApprovalChart is the flat machine of XState's "malformed snapshots"
// cases.
func xrehApprovalChart(t *testing.T) *xrehMachine {
	t.Helper()

	return xrehNew(t, "order-approval", func(b *xrehBuilder) {
		b.Initial("reviewing")
		b.State("reviewing").On[xrehApprove]().To("approved")
		b.State("approved")
	})
}

// xrehNestedApprovalChart is the nested machine of XState's "malformed
// snapshots" cases, which XState calls parallelMachine although it has no
// parallel state.
//
// XState restores a snapshot of the flat machine onto it with the state
// value replaced. sc would reject that as a snapshot of another chart, so
// these ports store a snapshot of this chart and replace its active states.
func xrehNestedApprovalChart(t *testing.T) *xrehMachine {
	t.Helper()

	return xrehNew(t, "order-approval", func(b *xrehBuilder) {
		b.Initial("active")
		b.State("active", func(b *xrehBuilder) {
			b.Initial("reviewing")
			b.State("reviewing")
			b.State("approved")
		})
	}, sc.AllowUnreachable())
}

// xrehStored returns a stored snapshot of a new instance of m.
func xrehStored(t *testing.T, m *xrehMachine) []byte {
	t.Helper()

	snap, _ := m.Start(counter{N: 0}, epoch)

	return xrehPersist(t, snap)
}

// xrehWithActive returns the stored snapshot with its active states
// replaced by the JSON value given.
func xrehWithActive(t *testing.T, data []byte, active string) []byte {
	t.Helper()

	return xrehFields(t, data, func(fields map[string]json.RawMessage) {
		fields["active"] = json.RawMessage(active)
	})
}

// rehydration.test.ts: rehydration > persisted state value validation >
// malformed snapshots > should error with InvalidSnapshotError when the
// snapshot is %s (null, a string, a number, an array).
//
// XState also expects the error snapshot to answer can() with false. sc
// returns no snapshot with the error, so there is nothing to ask.
func TestXRehErrorsWhenTheSnapshotIsNotAnObject(t *testing.T) {
	t.Parallel()

	m := xrehApprovalChart(t)

	for name, data := range map[string]string{
		"null":     `null`,
		"a string": `"reviewing"`,
		"a number": `42`,
		"an array": `[]`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			restored, effects, err := m.Restore([]byte(data), epoch)

			require.ErrorIs(t, err, sc.ErrInvalidSnapshot)
			require.Empty(t, restored.Active())
			require.Empty(t, effects)
		})
	}
}

// rehydration.test.ts: rehydration > persisted state value validation >
// malformed snapshots > should error with InvalidSnapshotError when the
// state value is missing.
func TestXRehErrorsWhenTheStateValueIsMissing(t *testing.T) {
	t.Parallel()

	m := xrehApprovalChart(t)
	data := xrehFields(t, xrehStored(t, m), func(fields map[string]json.RawMessage) {
		delete(fields, "active")
	})

	_, _, err := m.Restore(data, epoch)

	require.ErrorIs(t, err, sc.ErrInvalidSnapshot)
	require.ErrorContains(t, err, "has 0 active child states, want 1")
}

// rehydration.test.ts: rehydration > persisted state value validation >
// malformed snapshots > should error with InvalidSnapshotError when the
// state value is %s (a number, null, an array, a boolean).
func TestXRehErrorsWhenTheStateValueIsInvalid(t *testing.T) {
	t.Parallel()

	m := xrehApprovalChart(t)
	stored := xrehStored(t, m)

	for name, active := range map[string]string{
		"a number":  `42`,
		"null":      `null`,
		"a boolean": `true`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, _, err := m.Restore(xrehWithActive(t, stored, active), epoch)

			require.ErrorIs(t, err, sc.ErrInvalidSnapshot)
		})
	}

	t.Run("an array", func(t *testing.T) {
		t.Parallel()

		// DIVERGES from XState: XState rejects the array ['reviewing']
		// as an invalid state value. In sc a list of state names is the
		// stored form of the active states, so the same value restores.
		restored := xrehRestore(t, m, xrehWithActive(t, stored, `["reviewing"]`), epoch)

		require.Equal(t, []string{"reviewing"}, restored.Active())
	})

	t.Run("a state name", func(t *testing.T) {
		t.Parallel()

		// The reverse of the array case, and not an XState case: a bare
		// state name, XState's valid form, is not sc's.
		_, _, err := m.Restore(xrehWithActive(t, stored, `"reviewing"`), epoch)

		require.ErrorIs(t, err, sc.ErrInvalidSnapshot)
	})
}

// rehydration.test.ts: rehydration > persisted state value validation >
// malformed snapshots > should error with InvalidSnapshotError when a nested
// state value is invalid.
func TestXRehErrorsWhenANestedStateValueIsInvalid(t *testing.T) {
	t.Parallel()

	m := xrehNestedApprovalChart(t)

	_, _, err := m.Restore(xrehWithActive(t, xrehStored(t, m), `["active", 42]`), epoch)

	require.ErrorIs(t, err, sc.ErrInvalidSnapshot)
	require.ErrorContains(t, err, "active.1")
}

// rehydration.test.ts: rehydration > persisted state value validation >
// malformed snapshots > should error with InvalidSnapshotError when a
// non-parallel state has several active children.
func TestXRehErrorsWhenANonParallelStateHasSeveralActiveChildren(t *testing.T) {
	t.Parallel()

	m := xrehNestedApprovalChart(t)

	_, _, err := m.Restore(xrehWithActive(t, xrehStored(t, m), `["active", "reviewing", "approved"]`), epoch)

	require.ErrorIs(t, err, sc.ErrInvalidSnapshot)
	require.ErrorContains(t, err, `state "active" has 2 active child states, want 1`)
}

// rehydration.test.ts: rehydration > persisted state value validation >
// malformed snapshots > should report missing states as InvalidSnapshotError.
func TestXRehReportsMissingStatesAsInvalidSnapshot(t *testing.T) {
	t.Parallel()

	m := xrehApprovalChart(t)

	_, _, err := m.Restore(xrehWithActive(t, xrehStored(t, m), `["archived"]`), epoch)

	require.ErrorIs(t, err, sc.ErrInvalidSnapshot)
	require.ErrorContains(t, err, `"archived" is not a state of the chart`)
}

// rehydration.test.ts: rehydration > persisted state value validation >
// malformed snapshots > should complete a partial state value with initial
// states.
func TestXRehCompletesAPartialStateValueWithInitialStates(t *testing.T) {
	t.Parallel()

	m := xrehNestedApprovalChart(t)

	_, _, err := m.Restore(xrehWithActive(t, xrehStored(t, m), `["active"]`), epoch)

	// DIVERGES from XState: XState completes {active: {}} with the
	// initial state and restores into active.reviewing. sc does not fill
	// in a compound state's child: it rejects the snapshot.
	require.ErrorIs(t, err, sc.ErrInvalidSnapshot)
	require.ErrorContains(t, err, `state "active" has 0 active child states, want 1`)

	// What sc does complete is the other direction: a leaf's ancestors.
	restored := xrehRestore(t, m, xrehWithActive(t, xrehStored(t, m), `["reviewing"]`), epoch)
	require.Equal(t, []string{"active", "reviewing"}, restored.Active())
	require.Equal(t, []string{"active", "reviewing"}, xrehRaw(t, xrehPersist(t, restored)).Active)
}

// rehydration.test.ts: rehydration > persisted state value validation >
// malformed snapshots > should still ignore unknown fields.
func TestXRehIgnoresUnknownFields(t *testing.T) {
	t.Parallel()

	m := xrehApprovalChart(t)
	data := xrehFields(t, xrehStored(t, m), func(fields map[string]json.RawMessage) {
		fields["somethingElse"] = json.RawMessage(`true`)
	})

	restored := xrehRestore(t, m, data, epoch)
	require.False(t, restored.Done())

	restored, _ = m.Transition(restored, xrehApprove{}, epoch)
	require.Equal(t, []string{"approved"}, restored.Active())
}

// rehydration.test.ts: rehydration > persisted state value validation >
// should restore successfully when the persisted state value is valid.
func TestXRehRestoresAValidStateValue(t *testing.T) {
	t.Parallel()

	m := xrehRegionsChart(t, "reviewing")

	restored := xrehRestore(t, m, xrehStored(t, m), epoch)

	require.False(t, restored.Done())
	require.Equal(t, []string{"review", "reviewing", "payment", "pending"}, restored.Active())
}

// rehydration.test.ts: inline invoke persistence > persists an inline invoke
// without `src` and restores it.
//
// XState stores no source key for a child invoked with inline logic. sc
// always stores the service name, so that expectation has no counterpart.
func TestXRehPersistsAndRestoresAnInlineInvoke(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		child := xrehCounting(t, "counter", 1)
		m := xrehNew(t, "parent", func(b *xrehBuilder) {
			b.Initial("active")
			b.State("active", func(b *xrehBuilder) {
				b.Initial("running")
				running := b.State("running")
				xrehInvoke(running, "counter", child, 0)
				xrehForward(running, "counter")
			})
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		synctest.Wait()
		xrehSend(t, inst, xrehInc{})

		inst, data := xrehRestart(t, m, inst)
		defer inst.Close()

		restored := xrehRestore(t, m, data, time.Now())
		require.False(t, restored.Done())
		require.Equal(t, 1, xrehChildOf(t, child, restored, "counter").Context.N)

		xrehSend(t, inst, xrehInc{})
		require.Equal(t, 2, xrehChildOf(t, child, inst.Snapshot(), "counter").Context.N)
	})
}

// rehydration.test.ts: inline invoke persistence > restores inline invokes
// in parallel regions.
//
// The second child's context {name: 'other'} is the count 7 here.
func TestXRehRestoresInlineInvokesInParallelRegions(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		child := xrehCounting(t, "counter", 1)
		other := xrehCounting(t, "other", 0)
		m := xrehNew(t, "parent", func(b *xrehBuilder) {
			b.Regions()
			a := b.State("a")
			xrehInvoke(a, "counter", child, 0)
			xrehForward(a, "counter")
			xrehInvoke(b.State("b"), "other", other, 7)
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		synctest.Wait()
		xrehSend(t, inst, xrehInc{})

		inst, _ = xrehRestart(t, m, inst)
		defer inst.Close()

		require.Equal(t, 1, xrehChildOf(t, child, inst.Snapshot(), "counter").Context.N)
		require.Equal(t, 7, xrehChildOf(t, other, inst.Snapshot(), "other").Context.N)
	})
}

// rehydration.test.ts: inline invoke persistence > resolves a reused invoke
// id against the active state only.
func TestXRehResolvesAReusedInvokeIDAgainstTheActiveState(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		child := xrehCounting(t, "counter", 1)
		other := xrehCounting(t, "other", 100)
		m := xrehNew(t, "parent", func(b *xrehBuilder) {
			b.Initial("first")
			first := b.State("first")
			xrehInvoke(first, "worker", other, 100)
			first.On[xrehNext]().To("second")
			second := b.State("second")
			xrehInvoke(second, "worker", child, 0)
			xrehForward(second, "worker")
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		synctest.Wait()
		xrehSend(t, inst, xrehNext{})
		xrehSend(t, inst, xrehInc{})

		inst, _ = xrehRestart(t, m, inst)
		defer inst.Close()

		require.Equal(t, 1, xrehChildOf(t, child, inst.Snapshot(), "worker").Context.N)

		xrehSend(t, inst, xrehInc{})
		require.Equal(t, 2, xrehChildOf(t, child, inst.Snapshot(), "worker").Context.N)
	})
}

// rehydration.test.ts: inline invoke persistence > keeps the source key of a
// registered invoke.
//
// As in XState, the machine has no states.
func TestXRehKeepsTheSourceKeyOfARegisteredInvoke(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		child := xrehCounting(t, "counter", 1)
		m := xrehNew(t, "parent", func(b *xrehBuilder) {
			xrehInvoke(b.Chart(), "counter", child, 0)
			xrehForward(b.Chart(), "counter")
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		synctest.Wait()
		xrehSend(t, inst, xrehInc{})

		inst, data := xrehRestart(t, m, inst)
		defer inst.Close()

		raw := xrehRaw(t, data)
		require.Len(t, raw.Invocations, 1)
		require.Equal(t, "counter", raw.Invocations[0].Name)
		require.Equal(t, "parent", raw.Invocations[0].State)
		require.Equal(t, 1, xrehChildOf(t, child, inst.Snapshot(), "counter").Context.N)
	})
}

// rehydration.test.ts: inline invoke persistence > errors when no active
// state invokes a source-less child.
func TestXRehAChildNoActiveStateInvokes(t *testing.T) {
	t.Parallel()

	m := xrehNew(t, "parent", func(b *xrehBuilder) {
		b.Initial("idle")
		b.State("idle").On[xrehGo]().To("running")
		xrehInvoke(b.State("running"), "counter", xrehCounting(t, "counter", 1), 0)
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, effects := m.Transition(snap, xrehGo{}, epoch)
	run := effect(t, effects, sc.EffectStartInvocation)

	data := xrehTamper(t, xrehPersist(t, snap), func(raw *sc.RawSnapshot) { raw.Active = []string{"idle"} })

	// DIVERGES from XState: XState puts the restored actor in the error
	// status, "Unable to restore child actor 'counter': it has no source
	// key, and no active state of machine 'parent' invokes it". sc
	// restores the snapshot, drops the run no active state owns, and
	// returns the effect that stops it.
	restored, effects, err := m.Restore(data, epoch)
	require.NoError(t, err)

	require.Equal(t, []string{"idle"}, restored.Active())
	require.Empty(t, restored.Services())
	require.Equal(t, []sc.EffectKind{sc.EffectStopInvocation}, kinds(effects))
	require.Equal(t, run.ID, effects[0].ID)
	require.Equal(t, "counter", effects[0].Name)
}

// ---------------------------------------------------------------------------
// restoreEventless.test.ts
// ---------------------------------------------------------------------------

// xrehWaitingChart waits until its context says go. guards counts the calls
// of its guard.
func xrehWaitingChart(t *testing.T, guards *atomic.Int32) *sc.Machine[string, xrehFlag] {
	t.Helper()

	m, err := sc.New("eventless", func(b *sc.Builder[string, xrehFlag]) {
		b.Initial("waiting")
		waiting := b.State("waiting")
		waiting.Always().When("go", func(c xrehFlag, _ struct{}) bool {
			guards.Add(1)

			return c.Go
		}).To("done")
		b.State("done")
	})
	require.NoError(t, err)

	return m
}

// restoreEventless.test.ts: restoring into eventless transitions > does not
// re-evaluate always transitions on restore.
func TestXRehDoesNotReevaluateAlwaysTransitionsOnRestore(t *testing.T) {
	t.Parallel()

	var guards atomic.Int32
	m := xrehWaitingChart(t, &guards)

	snap, _ := m.Start(xrehFlag{Go: false}, epoch)
	tampered := xrehTamper(t, xrehPersist(t, snap), func(raw *sc.RawSnapshot) {
		raw.Context = json.RawMessage(`{"go":true}`)
	})

	restored := xrehRestore(t, m, tampered, epoch)
	require.True(t, restored.Context.Go)
	require.Equal(t, []string{"waiting"}, restored.Active())

	// Starting the restored instance does not evaluate them either.
	inst, err := m.InstanceFrom(t.Context(), restored)
	require.NoError(t, err)
	defer inst.Close()

	require.Equal(t, []string{"waiting"}, inst.Snapshot().Active())

	// XState's warning says they are evaluated at the next event. So they
	// are in sc, even for an event no state handles, and a caller that
	// has no event brings the snapshot up to date with Advance.
	next, _ := m.Transition(restored, xrehUnmatched{}, epoch)
	require.Equal(t, []string{"done"}, next.Active())

	advanced, _ := m.Advance(restored, epoch)
	require.Equal(t, []string{"done"}, advanced.Active())

	_, err = inst.Advance(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"done"}, inst.Snapshot().Active())
}

// restoreEventless.test.ts: restoring into eventless transitions > warns in
// development when the restored configuration has eventless transitions.
//
// sc has no warnings. What is ported is the last expectation: restoring runs
// no guard.
func TestXRehRestoreRunsNoGuardOfAnEventlessTransition(t *testing.T) {
	t.Parallel()

	var guards atomic.Int32
	m := xrehWaitingChart(t, &guards)

	snap, _ := m.Start(xrehFlag{Go: false}, epoch)
	data := xrehPersist(t, snap)
	guards.Store(0)

	restored := xrehRestore(t, m, data, epoch)
	require.Empty(t, m.Resume(restored))

	require.Zero(t, guards.Load())
}

// ---------------------------------------------------------------------------
// restoreTerminal.test.ts
// ---------------------------------------------------------------------------

// xrehTerminalChart is the chart of restoreTerminal.test.ts, with its entry
// action on the root as in XState.
func xrehTerminalChart(t *testing.T) *xrehMachine {
	t.Helper()

	return xrehNew(t, "terminal", func(b *xrehBuilder) {
		b.Chart().Entry("entry", noop)
		b.Initial("a")
		b.State("a").On[xrehNext]().Do("action", func(context.Context, counter, xrehNext) {}).To("b")
		b.State("b")
	})
}

// restoreTerminal.test.ts: restoring terminal snapshots > keeps a restored
// %s snapshot terminal after start() without running transitions (done,
// error, stopped).
//
// An sc snapshot is either done or not: it has no error or stopped status,
// so only the done case is ported.
func TestXRehKeepsARestoredDoneSnapshotTerminal(t *testing.T) {
	t.Parallel()

	m := xrehTerminalChart(t)
	data := xrehTamper(t, xrehStored(t, m), func(raw *sc.RawSnapshot) { raw.Done = true })

	// As in XState, the status is taken as stored although "a" is not a
	// final state.
	restored := xrehRestore(t, m, data, epoch)
	require.True(t, restored.Done())
	require.Empty(t, m.Resume(restored))
	require.False(t, m.Can(restored, xrehNext{}, epoch))

	next, effects := m.Transition(restored, xrehNext{}, epoch)

	require.True(t, next.Done())
	require.Equal(t, []string{"a"}, next.Active())
	require.Equal(t, restored.Seq(), next.Seq())
	require.Empty(t, effects)
}

// restoreTerminal.test.ts: restoring terminal snapshots > surfaces a
// restore-time failure as an error snapshot and subscriber error.
func TestXRehSurfacesARestoreTimeFailure(t *testing.T) {
	t.Parallel()

	m := xrehTerminalChart(t)
	data := xrehTamper(t, xrehStored(t, m), func(raw *sc.RawSnapshot) { raw.Active = []string{"missing"} })

	// DIVERGES from XState: XState's restoreActor does not throw: it
	// returns an actor whose snapshot has the error status and tells its
	// subscribers. sc's Restore returns the error itself, with no
	// snapshot to drive.
	restored, effects, err := m.Restore(data, epoch)

	require.ErrorIs(t, err, sc.ErrInvalidSnapshot)
	require.ErrorContains(t, err, `"missing" is not a state of the chart`)
	require.Empty(t, restored.Active())
	require.Empty(t, effects)
}

// ---------------------------------------------------------------------------
// restored-effects.test.ts
// ---------------------------------------------------------------------------

// restored-effects.test.ts: reattaches a JSON-restored callback and cleans
// up each incarnation.
//
// XState restores the callback logic as an actor of its own. In sc the
// callback is a service of the chart itself, which has no states and passes
// it what it is sent.
func TestXRehReattachesARestoredCallbackAndCleansUpEachIncarnation(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var starts, received, cleanups atomic.Int32
		m := xrehNew(t, "callback", func(b *xrehBuilder) {
			chart := b.Chart()
			chart.Invoke("callback", func(ctx context.Context, _ counter) (int, error) {
				starts.Add(1)
				defer cleanups.Add(1)

				for ev := range sc.Inbox(ctx) {
					if _, ok := ev.(xrehPing); ok {
						received.Add(1)
					}
				}

				return 0, context.Cause(ctx)
			}).OnError().Stay()
			chart.On[xrehPing]().SendTo("forward", "callback", func(_ counter, ev xrehPing) any { return ev })
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)

		inst, _ = xrehRestart(t, m, inst)
		require.Equal(t, int32(1), cleanups.Load())

		xrehSend(t, inst, xrehPing{})
		require.Equal(t, int32(2), starts.Load())
		require.Equal(t, int32(1), received.Load())

		inst.Close()
		require.Equal(t, int32(2), cleanups.Load())
	})
}

// restored-effects.test.ts: reattaches active keyed effects without
// replaying completed effects.
//
// sc has no keyed effects. The nearest thing a chart keeps across a restart
// is a service run: one still running is started again, one that finished
// is not. XState marks the effect completed by editing the stored snapshot;
// here the service completes. The services are those of the chart, which
// has no states.
func TestXRehRestartsRunningServicesWithoutReplayingCompletedOnes(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var active, completed atomic.Int32
		m := xrehNew(t, "effects", func(b *xrehBuilder) {
			chart := b.Chart()
			chart.Invoke("active", func(ctx context.Context, c counter) (int, error) {
				active.Add(1)

				return xrehBlock(ctx, c)
			}).OnError().Stay()
			done := chart.Invoke("completed", func(context.Context, counter) (int, error) {
				completed.Add(1)

				return 42, nil
			})
			done.OnDone().Assign("keep", func(c counter, d sc.Done[int]) counter { c.N = d.Result; return c })
			done.OnError().Stay()
			chart.On[xrehPing]().Stay()
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)

		inst, data := xrehRestart(t, m, inst)
		defer inst.Close()

		raw := xrehRaw(t, data)
		require.Len(t, raw.Invocations, 1)
		require.Equal(t, "active", raw.Invocations[0].Name)

		xrehSend(t, inst, xrehPing{})

		require.Equal(t, int32(2), active.Load())
		require.Equal(t, int32(1), completed.Load())
		require.Equal(t, 42, inst.Snapshot().Context.N)
	})
}

// restored-effects.test.ts: journals and restores an effect keyed %s
// (constructor, toString, __proto__).
//
// The keys are JavaScript's inherited property names, which mean nothing to
// Go. The port names a service of the chart by each and checks that its run
// is stored under that name and started again.
func TestXRehStoresAndRestartsAServiceNamedLikeAJavaScriptProperty(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"constructor", "toString", "__proto__"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				var runs atomic.Int32
				m := xrehNew(t, "keyed", func(b *xrehBuilder) {
					b.Chart().Invoke(key, func(ctx context.Context, c counter) (int, error) {
						runs.Add(1)

						return xrehBlock(ctx, c)
					}).OnError().Stay()
				})

				inst, err := m.Instance(t.Context(), counter{N: 0})
				require.NoError(t, err)
				synctest.Wait()
				require.Equal(t, int32(1), runs.Load())

				inst, data := xrehRestart(t, m, inst)
				defer inst.Close()

				raw := xrehRaw(t, data)
				require.Len(t, raw.Invocations, 1)
				require.Equal(t, key, raw.Invocations[0].Name)
				require.Equal(t, int32(2), runs.Load())
			})
		})
	}
}

// restored-effects.test.ts: reuses JSON-restored completed steps keyed %s
// (constructor, toString, __proto__).
//
// sc has no step journal. A service keeps the result of a finished step by
// saving it with Checkpoint and reading it back with Resumed, which is what
// the port checks: the work is done once, and the run after the restart
// gets its result from the stored snapshot.
func TestXRehReusesACompletedStepAfterARestart(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"constructor", "toString", "__proto__"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				var work atomic.Int32
				results := &xrehLog{mu: sync.Mutex{}, vals: nil}

				m := xrehNew(t, "steps", func(b *xrehBuilder) {
					b.Chart().Invoke(key, func(ctx context.Context, c counter) (int, error) {
						result := 0

						found, err := sc.Resumed(ctx, &result)
						if err != nil {
							return 0, fmt.Errorf("resume step: %w", err)
						}

						if !found {
							work.Add(1)
							result = 42

							if err := sc.Checkpoint(ctx, result); err != nil {
								return 0, fmt.Errorf("save step: %w", err)
							}
						}

						results.add(result)

						return xrehBlock(ctx, c)
					}).OnError().Stay()
				})

				inst, err := m.Instance(t.Context(), counter{N: 0})
				require.NoError(t, err)

				inst, data := xrehRestart(t, m, inst)
				defer inst.Close()

				raw := xrehRaw(t, data)
				require.Len(t, raw.Invocations, 1)
				require.Equal(t, key, raw.Invocations[0].Name)
				require.JSONEq(t, `42`, string(raw.Invocations[0].Saved))

				require.Equal(t, []int{42, 42}, results.list())
				require.Equal(t, int32(1), work.Load())
			})
		})
	}
}

// ---------------------------------------------------------------------------
// timers.persistence.test.ts
// ---------------------------------------------------------------------------

// xrehLightChart is XState's createLightMachine.
func xrehLightChart(t *testing.T) *xrehMachine {
	t.Helper()

	return xrehNew(t, "light", func(b *xrehBuilder) {
		b.Initial("green")
		green := b.State("green")
		green.After(time.Second).To("yellow")
		green.On[xrehStop]().To("red")
		b.State("yellow")
		b.State("red")
	})
}

// timers.persistence.test.ts: persisted logical timers > persists timer
// intent without runtime clock bookkeeping.
func TestXRehPersistsTimerIntent(t *testing.T) {
	t.Parallel()

	m := xrehLightChart(t)
	snap, effects := m.Start(counter{N: 0}, epoch)
	started := effect(t, effects, sc.EffectStartTimer)

	raw := xrehRaw(t, xrehPersist(t, snap))

	require.Len(t, raw.Timers, 1)
	require.Equal(t, started.ID, raw.Timers[0].ID)
	require.Equal(t, "green", raw.Timers[0].State)
	require.Equal(t, 0, raw.Timers[0].Index)
	require.Equal(t, int64(time.Second), raw.Timers[0].DelayNanos)
	require.Empty(t, raw.Timers[0].Kind)

	// DIVERGES from XState: XState stores only the declared delay and
	// expects no scheduledAt, dueAt or elapsed in the stored timer. sc
	// stores the absolute time the timer is due, on the clock of the
	// driver that started it.
	require.WithinDuration(t, epoch.Add(time.Second), raw.Timers[0].Due, 0)

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(xrehPersist(t, snap), &fields))

	var timers []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(fields["timers"], &timers))
	require.Contains(t, timers[0], "due")
}

// timers.persistence.test.ts: persisted logical timers > does not persist
// timers that were cancelled.
func TestXRehDoesNotPersistCancelledTimers(t *testing.T) {
	t.Parallel()

	m := xrehLightChart(t)
	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, effects := m.Transition(snap, xrehStop{}, epoch)

	require.Equal(t, []sc.EffectKind{sc.EffectCancelTimer}, kinds(effects))
	require.Empty(t, snap.Timers())
	require.Empty(t, xrehRaw(t, xrehPersist(t, snap)).Timers)
}

// timers.persistence.test.ts: persisted logical timers > restarts logical
// timers with their declared delay when locally restored.
func TestXRehRestoredTimersKeepTheTimeTheyAreDue(t *testing.T) {
	t.Parallel()

	m := xrehLightChart(t)
	snap, _ := m.Start(counter{N: 0}, epoch)
	data := xrehPersist(t, snap)

	// The snapshot is restored half a second after the timer started.
	restoredAt := epoch.Add(500 * time.Millisecond)
	restored := xrehRestore(t, m, data, restoredAt)

	// DIVERGES from XState: XState starts a restored timer again with its
	// whole declared delay, so its light stays green for 999ms after the
	// restore and turns yellow at 1000ms. sc keeps the time the timer was
	// due when it started: the light turns yellow one second after the
	// timer started, which is half a second after this restore.
	resumed := m.Resume(restored)
	require.Equal(t, []sc.EffectKind{sc.EffectStartTimer}, kinds(resumed))
	require.Equal(t, time.Second, resumed[0].Delay)
	require.Equal(t, epoch.Add(time.Second), resumed[0].At)

	early, _ := m.Advance(restored, restoredAt.Add(499*time.Millisecond))
	require.Equal(t, []string{"green"}, early.Active())

	due, _ := m.Advance(restored, restoredAt.Add(500*time.Millisecond))
	require.Equal(t, []string{"yellow"}, due.Active())

	// A snapshot restored after the timer was due is behind until it is
	// advanced: Restore fires nothing.
	late := xrehRestore(t, m, data, epoch.Add(time.Hour))
	require.Equal(t, []string{"green"}, late.Active())

	late, _ = m.Advance(late, epoch.Add(time.Hour))
	require.Equal(t, []string{"yellow"}, late.Active())
}

// timers.persistence.test.ts: persisted logical timers > materializes a
// restored invoke timeout for the current child session.
//
// sc has no timeout on an invocation. The nearest declaration is a budget
// on the invoking state, sc.StateBuilder.Within, which fails the service
// when it runs out. XState's session IDs have no counterpart: an sc run
// keeps its ID across a restart.
func TestXRehARestoredServiceBudgetKeepsItsDeadline(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xrehNew(t, "timeout", func(b *xrehBuilder) {
			b.Initial("working")
			inv := b.State("working").Within(100*time.Millisecond).Invoke("child", xrehBlock)
			inv.OnError().To("timedOut")
			b.State("timedOut")
		})

		started := time.Now()
		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		synctest.Wait()

		before := inst.Snapshot().Services()
		require.Len(t, before, 1)

		// The process is down for 40ms of the budget.
		inst.Close()
		data := xrehPersist(t, inst.Snapshot())

		// The budget is not stored as a timer but as the state's
		// deadline.
		raw := xrehRaw(t, data)
		require.Empty(t, raw.Timers)
		require.WithinDuration(t, started.Add(100*time.Millisecond), raw.Scopes["working"], 0)

		xrehPass(40 * time.Millisecond)

		inst, err = m.InstanceFrom(t.Context(), xrehRestore(t, m, data, time.Now()))
		require.NoError(t, err)
		defer inst.Close()

		require.Equal(t, before, inst.Snapshot().Services())

		// DIVERGES from XState: XState gives the restored child its whole
		// timeout again, 100ms from the restore. sc keeps the deadline
		// the budget had when it started, so the service is stopped 60ms
		// after this restore.
		xrehPass(59 * time.Millisecond)
		require.Equal(t, []string{"working"}, inst.Snapshot().Active())

		xrehPass(time.Millisecond)
		require.Equal(t, []string{"timedOut"}, inst.Snapshot().Active())
	})
}

// timers.persistence.test.ts: persisted logical timers > cancels a restored
// timer when its declaring state exits.
func TestXRehCancelsARestoredTimerWhenItsStateExits(t *testing.T) {
	t.Parallel()

	m := xrehLightChart(t)
	snap, effects := m.Start(counter{N: 0}, epoch)
	started := effect(t, effects, sc.EffectStartTimer)

	restored := xrehRestore(t, m, xrehPersist(t, snap), epoch)

	restored, effects = m.Transition(restored, xrehStop{}, epoch)
	require.Equal(t, []sc.EffectKind{sc.EffectCancelTimer}, kinds(effects))
	require.Equal(t, started.ID, effects[0].ID)

	restored, _ = m.Advance(restored, epoch.Add(2*time.Second))
	require.Equal(t, []string{"red"}, restored.Active())

	// A driver that could not cancel the timer delivers it for nothing.
	restored, _ = m.Transition(restored, sc.TimerFired{ID: started.ID}, epoch.Add(2*time.Second))
	require.Equal(t, []string{"red"}, restored.Active())
}

// timers.persistence.test.ts: persisted logical timers > round-trips timers
// through a restored-but-never-started actor.
func TestXRehRoundTripsTimersThroughARestoredSnapshot(t *testing.T) {
	t.Parallel()

	m := xrehLightChart(t)
	snap, _ := m.Start(counter{N: 0}, epoch)
	data := xrehPersist(t, snap)

	again := xrehPersist(t, xrehRestore(t, m, data, epoch.Add(time.Minute)))

	require.JSONEq(t, string(data), string(again))
	require.Len(t, xrehRaw(t, again).Timers, 1)
	require.Equal(t, int64(time.Second), xrehRaw(t, again).Timers[0].DelayNanos)
}

// timers.persistence.test.ts: persisted logical timers > restores timers of
// rehydrated child actors.
func TestXRehRestoresTheTimersOfRehydratedChildren(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		child := xrehNew(t, "child", func(b *xrehBuilder) {
			b.Initial("waiting")
			b.State("waiting").After(time.Second).To("done")
			b.Final("done")
		})
		m := xrehNew(t, "parent", func(b *xrehBuilder) {
			b.Initial("working")
			inv := b.State("working").InvokeMachine("child", child, func(c counter) counter { return c }, xrehOutput)
			inv.OnDone().To("finished")
			inv.OnError().Stay()
			b.State("finished")
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)

		// No time passes between the start and the restore, so the
		// timer is due a second after either.
		inst, data := xrehRestart(t, m, inst)
		defer inst.Close()

		require.Len(t, xrehChildOf(t, child, xrehRestore(t, m, data, time.Now()), "child").Timers(), 1)
		require.Equal(t, []string{"working"}, inst.Snapshot().Active())

		xrehPass(999 * time.Millisecond)
		require.Equal(t, []string{"working"}, inst.Snapshot().Active())

		xrehPass(time.Millisecond)
		require.Equal(t, []string{"finished"}, inst.Snapshot().Active())
	})
}

// xrehPinging makes st the parent of the delayed-send cases: it invokes
// child, and on xrehSchedule sends it xrehPing a tenth of a second later.
//
// sc has no delayed SendTo. The parent raises an event for itself after the
// delay with RaiseAfter, and sends to the child when that event arrives, so
// the stored timer is a delayed event of the parent and names no target.
func xrehPinging(st *sc.StateBuilder[string, counter], child *xrehMachine) {
	xrehInvoke(st, "child", child, 0)
	st.On[xrehSchedule]().RaiseAfter("ping", 100*time.Millisecond, func(counter, xrehSchedule) any { return xrehPingDue{} })
	st.On[xrehPingDue]().SendTo("deliver", "child", func(counter, xrehPingDue) any { return xrehPing{} })
}

// timers.persistence.test.ts: persisted logical timers > restores the
// logical target of a delayed child send.
//
// XState expects the stored timer to be a sendTo naming the child. See
// xrehPinging for what sc stores instead. As in XState, neither machine has
// states.
func TestXRehRestoresADelayedSendToAChild(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var received atomic.Int32
		child := xrehNew(t, "child", func(b *xrehBuilder) {
			b.On[xrehPing]().Do("received", func(context.Context, counter, xrehPing) { received.Add(1) })
		})
		m := xrehNew(t, "parent", func(b *xrehBuilder) {
			xrehPinging(b.Chart(), child)
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		synctest.Wait()
		xrehSend(t, inst, xrehSchedule{})

		inst, data := xrehRestart(t, m, inst)
		defer inst.Close()

		raw := xrehRaw(t, data)
		require.Len(t, raw.Timers, 1)
		require.Equal(t, "raise", raw.Timers[0].Kind)
		require.Equal(t, "ping", raw.Timers[0].Name)
		require.Equal(t, "parent", raw.Timers[0].State)
		require.Equal(t, xrehPingDue{}, inst.Snapshot().Timers()[0].Raised)

		xrehPass(99 * time.Millisecond)
		require.Zero(t, received.Load())

		xrehPass(time.Millisecond)
		require.Equal(t, int32(1), received.Load())
	})
}

// timers.persistence.test.ts: persisted logical timers > restores the
// logical parent target of a delayed child send.
//
// XState expects the child's stored timer to be a sendTo whose target is
// {type: 'parent'}. In sc the child raises an event for itself after the
// delay and emits to its parent when it arrives, so the timer in the
// child's stored snapshot is a delayed event of the child. As in XState,
// neither machine has states.
func TestXRehRestoresADelayedSendFromAChildToItsParent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		child := xrehNew(t, "child", func(b *xrehBuilder) {
			b.On[xrehSchedule]().RaiseAfter("ping-parent", 100*time.Millisecond, func(counter, xrehSchedule) any { return xrehPingDue{} })
			b.On[xrehPingDue]().Emit("ping", func(counter, xrehPingDue) xrehPing { return xrehPing{} })
		})

		var received atomic.Int32
		m := xrehNew(t, "parent", func(b *xrehBuilder) {
			xrehInvoke(b.Chart(), "child", child, 0)
			b.On[xrehBegin]().SendTo("schedule", "child", func(counter, xrehBegin) any { return xrehSchedule{} })
			b.On[xrehPing]().Do("received", func(context.Context, counter, xrehPing) { received.Add(1) })
		})

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		synctest.Wait()
		xrehSend(t, inst, xrehBegin{})

		inst, data := xrehRestart(t, m, inst)
		defer inst.Close()

		require.Empty(t, xrehRaw(t, data).Timers)

		timers := xrehChildOf(t, child, xrehRestore(t, m, data, time.Now()), "child").Timers()
		require.Len(t, timers, 1)
		require.Equal(t, "ping-parent", timers[0].Name)
		require.Equal(t, xrehPingDue{}, timers[0].Raised)

		xrehPass(99 * time.Millisecond)
		require.Zero(t, received.Load())

		xrehPass(time.Millisecond)
		require.Equal(t, int32(1), received.Load())
	})
}

// timers.persistence.test.ts: persisted logical timers > does not rebind a
// delayed send to a replacement child with the same id.
//
// The state "active" is XState's own: re-entering it replaces the child.
func TestXRehADelayedSendAfterItsChildWasReplaced(t *testing.T) {
	t.Parallel()

	child := xrehNew(t, "child", func(b *xrehBuilder) {
		b.Initial("listening")
		b.State("listening").On[xrehPing]().Stay()
	})
	m := xrehNew(t, "parent", func(b *xrehBuilder) {
		b.Initial("active")
		active := b.State("active")
		xrehPinging(active, child)
		active.On[xrehReenter]().To("active").Reenter()
	})

	snap, effects := m.Start(counter{N: 0}, epoch)
	first := effect(t, effects, sc.EffectStartInvocation)

	snap, _ = m.Transition(snap, xrehSchedule{}, epoch)
	snap, effects = m.Transition(snap, xrehReenter{}, epoch)
	replacement := effect(t, effects, sc.EffectStartInvocation)
	require.NotEqual(t, first.ID, replacement.ID)

	// DIVERGES from XState: XState refuses to persist, "Unable to persist
	// timer 'ping'", because the delayed send was bound to the child that
	// re-entering the state replaced. sc cannot bind a delayed send to a
	// run: the delay is a delayed event of the parent, which survives the
	// re-entry and is stored, and the send it leads to goes to whichever
	// run has the service's name by then, the replacement.
	data, err := json.Marshal(snap)
	require.NoError(t, err)
	require.Len(t, xrehRaw(t, data).Timers, 1)

	restored := xrehRestore(t, m, data, epoch)
	_, effects = m.Advance(restored, epoch.Add(100*time.Millisecond))

	send := effect(t, effects, sc.EffectSend)
	require.Equal(t, replacement.ID, send.ID)
	require.Equal(t, xrehPing{}, send.Event)
}

// ---------------------------------------------------------------------------
// tags.test.ts
// ---------------------------------------------------------------------------

// tags.test.ts: tags > supports tagging states.
func TestXRehSupportsTaggingStates(t *testing.T) {
	t.Parallel()

	m := xrehNew(t, "lights", func(b *xrehBuilder) {
		b.Initial("green")
		b.State("green").Tag("go").On[xrehTimer]().To("yellow")
		b.State("yellow").Tag("go").On[xrehTimer]().To("red")
		b.State("red").Tag("stop")
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	require.True(t, snap.HasTag("go"))

	snap, _ = m.Transition(snap, xrehTimer{}, epoch)
	require.True(t, snap.HasTag("go"))

	snap, _ = m.Transition(snap, xrehTimer{}, epoch)
	require.False(t, snap.HasTag("go"))
}

// tags.test.ts: tags > supports tags in compound states.
func TestXRehSupportsTagsInCompoundStates(t *testing.T) {
	t.Parallel()

	m := xrehNew(t, "crossing", func(b *xrehBuilder) {
		b.Initial("red")
		b.State("green").Tag("go")
		b.State("yellow")
		b.State("red", func(b *xrehBuilder) {
			b.Initial("walk")
			b.State("walk").Tag("crosswalkLight")
			b.State("wait").Tag("crosswalkLight")
		}).Tag("stop")
	}, sc.AllowUnreachable())

	snap, _ := m.Start(counter{N: 0}, epoch)

	require.False(t, snap.HasTag("go"))
	require.True(t, snap.HasTag("stop"))
	require.True(t, snap.HasTag("crosswalkLight"))
}

// tags.test.ts: tags > supports tags in parallel states.
//
// The states of the regions are qualified by their region, because a state
// identifier is unique in an sc chart.
func TestXRehSupportsTagsInParallelStates(t *testing.T) {
	t.Parallel()

	m := xrehNew(t, "regions", func(b *xrehBuilder) {
		b.Regions()
		b.State("foo", func(b *xrehBuilder) {
			b.Initial("foo.active")
			b.State("foo.active").Tag("yes")
			b.State("foo.inactive").Tag("no")
		})
		b.State("bar", func(b *xrehBuilder) {
			b.Initial("bar.active")
			b.State("bar.active").Tag("yes").On[xrehDeactivate]().To("bar.inactive")
			b.State("bar.inactive").Tag("no")
		})
	}, sc.AllowUnreachable())

	snap, _ := m.Start(counter{N: 0}, epoch)
	require.Equal(t, []string{"yes"}, snap.Tags())

	snap, _ = m.Transition(snap, xrehDeactivate{}, epoch)
	require.Equal(t, []string{"no", "yes"}, snap.Tags())
}

// tags.test.ts: tags > sets tags correctly after not selecting any
// transition.
func TestXRehKeepsTagsAfterSelectingNoTransition(t *testing.T) {
	t.Parallel()

	m := xrehNew(t, "unmatched", func(b *xrehBuilder) {
		b.Initial("a")
		b.State("a").Tag("myTag")
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	snap, effects := m.Transition(snap, xrehUnmatched{}, epoch)

	require.Empty(t, effects)
	require.True(t, snap.HasTag("myTag"))
}

// tags.test.ts: tags > tags can be single (not array).
func TestXRehTagsCanBeSingle(t *testing.T) {
	t.Parallel()

	m := xrehNew(t, "single", func(b *xrehBuilder) {
		b.Initial("green")
		b.State("green").Tag("go")
	})

	snap, _ := m.Start(counter{N: 0}, epoch)

	require.True(t, snap.HasTag("go"))
}

// tags.test.ts: tags > stringifies to an array.
func TestXRehTagsInTheStoredSnapshot(t *testing.T) {
	t.Parallel()

	m := xrehNew(t, "stringified", func(b *xrehBuilder) {
		b.Initial("green")
		b.State("green").Tag("go", "light")
	})

	snap, _ := m.Start(counter{N: 0}, epoch)
	require.Equal(t, []string{"go", "light"}, snap.Tags())

	data := xrehPersist(t, snap)

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &fields))

	// DIVERGES from XState: XState's snapshot JSON has "tags":
	// ["go","light"]. sc does not store tags: they follow from the active
	// states, and a restored snapshot works them out again.
	require.NotContains(t, fields, "tags")
	require.Equal(t, []string{"go", "light"}, xrehRestore(t, m, data, epoch).Tags())
}

// Not ported.
//
// rehydration.test.ts
//
// sc has no actor system to register children in:
//   - a rehydrated active child should be registered in the system
//   - a rehydrated done child should not be registered in the system
//
// An sc snapshot has no error status, and a service must declare an OnError
// transition, so a failing service cannot put its machine in error:
//   - should error on a rehydrated error state
//
// An sc transition cannot stop a service and start other logic under its
// name:
//   - (replaced inline invoke persistence) does not persist a child that
//     replaced an inline invoke as that invoke
//
// restoreEventless.test.ts
//
// sc has no warnings:
//   - does not warn when the restored configuration has no eventless
//     transitions
//   - warns in development when the restored configuration has eventless
//     transitions (its last expectation, that no guard runs, is ported)
//
// restoreTerminal.test.ts
//
// An sc snapshot has no error or stopped status, an instance has no
// subscribers, and a failed Restore returns no snapshot:
//   - keeps a restored error snapshot terminal after start() without running
//     transitions
//   - keeps a restored stopped snapshot terminal after start() without
//     running transitions
//   - delivers a restored error to subscribers added before and after start()
//   - restore failure yields a machine snapshot with matches()
//
// restored-effects.test.ts
//
// sc has no step logic with keyed effects or journaled steps outside a
// chart, and Go has no inherited object properties:
//   - deduplicates concurrently started local steps and settles waiters on
//     stop
//   - does not mistake inherited journal values for an effect keyed
//     constructor / toString
//   - drains all attachment cleanups once when the first throws
