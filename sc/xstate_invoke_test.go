package sc_test

// Ports of XState's packages/core/test/invoke.test.ts (6.0.0-beta.2), to check
// sc's semantics of invoked services against XState's. Each test names the
// XState case it ports. Cases that test an XState-only API are listed at the
// end of the file with the reason they were not ported.
//
// Several differences in what sc can declare shape every port:
//
//   - XState's promise actor is an sc service (Invoke), its callback and
//     observable actors are sc streams (Stream), and an invoked machine is
//     InvokeMachine. A callback actor never completes on its own, so the
//     streams that stand for one block until their context ends.
//   - XState delivers what an invoked actor sends back inside the parent's
//     own send, so its tests read the result synchronously. sc runs every
//     service on a goroutine of its own, so the ports wait for the outcome
//     with Await or synctest.Wait.
//   - A service must declare an OnError transition in sc. Where XState
//     declares none, the port declares OnError().Stay().
//   - What an XState root declares for itself (an invocation, an entry
//     action, a delayed or a re-entering transition) is declared on
//     Builder.Chart, and a parallel root is Builder.Regions. An XState
//     machine that declares no states is a chart without states.
//   - A service's input is the context as it is when the state is entered.
//     Only InvokeMachine has an input mapper. A service declares the event
//     its state was entered with by InvokeOn; a stream cannot, so a
//     transition records what the stream needs in the context.
//   - An XState entry action that sends to the parent is EmitOnEntry, and
//     one that sends to an actor its state invokes is SendOnEntry.
//   - XState runs its promise cases twice, for a Promise and for a
//     PromiseLike. Go has one kind of function, so each is ported once.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

type (
	xinvNext        struct{}
	xinvStart       struct{}
	xinvDec         struct{}
	xinvForwardDec  struct{}
	xinvGoToWaiting struct{}
	xinvResolve     struct{ User string }
	xinvReject      struct{}
	xinvSuccess     struct{ Data int }
	xinvUpdate      struct{}
	xinvFinished    struct{}
	xinvStarted     struct{}
	xinvStopped     struct{}
	xinvBegin       struct{ Payload bool }
	xinvCallback    struct{ Data int }
	xinvInc         struct{}
	xinvPing        struct{}
	xinvPong        struct{}
	xinvStopChild   struct{}
	xinvStop        struct{}
	xinvCount       struct{ Value int }
	xinvOne         struct{}
	xinvTwo         struct{}
	xinvStopOne     struct{}
	xinvDeactivate  struct{}
	xinvFetch       struct{}
	xinvRetry       struct{}
	xinvFinish      struct{}
	xinvEvent       struct{}
	xinvActivate    struct{}
	xinvReenter     struct{}
	xinvGoAway      struct{}
	xinvSetValue    struct{ Value int }
	xinvDoubled     struct{ Value int }
)

type xinvBuilder = sc.Builder[string, counter]

// xinvFetchChild is the context of the child machine that fetches a user,
// and xinvFetcher that of its parent.
type (
	xinvFetchChild struct {
		UserID string `json:"user_id"`
		User   string `json:"user"`
	}
	xinvFetcher struct {
		SelectedUserID string `json:"selected_user_id"`
		User           string `json:"user"`
	}
)

// xinvPromise is the context of XState's invokePromiseMachine.
type xinvPromise struct {
	ID      int  `json:"id"`
	Succeed bool `json:"succeed"`
}

// xinvFlags is a context with XState's "foo" flag.
type xinvFlags struct {
	Foo bool `json:"foo"`
}

// xinvBegun is a context with XState's "foo" flag that also records whether
// the BEGIN event was received.
type xinvBegun struct {
	Foo   bool `json:"foo"`
	Began bool `json:"began"`
}

// xinvText is a context holding one string.
type xinvText struct {
	Value string `json:"value"`
}

// xinvPair is a context holding two results.
type xinvPair struct {
	First  int `json:"first"`
	Second int `json:"second"`
}

// xinvNames is the context of XState's "multiple simultaneous services".
type xinvNames struct {
	One string `json:"one"`
	Two string `json:"two"`
}

var errXinvTest = errors.New("test")

// xinvLog collects what services and actions report, from any goroutine.
type xinvLog struct {
	mu    sync.Mutex
	lines []string
}

func xinvNewLog() *xinvLog {
	return &xinvLog{mu: sync.Mutex{}, lines: nil}
}

func (l *xinvLog) add(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.lines = append(l.lines, line)
}

func (l *xinvLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return slices.Clone(l.lines)
}

// xinvCallbackOf is a stream standing for an XState callback actor: it sends
// events back and then stays alive until it is stopped.
func xinvCallbackOf[C any](events ...any) func(context.Context, *sc.Run, C) iter.Seq2[any, error] {
	return func(ctx context.Context, _ *sc.Run, _ C) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			for _, ev := range events {
				if !yield(ev, nil) {
					return
				}
			}

			<-ctx.Done()
		}
	}
}

// xinvDisposable is a stream standing for an XState callback actor that
// returns a disposal function: it calls started, stays alive until it is
// stopped, and then calls stopped.
func xinvDisposable[C any](started, stopped func()) func(context.Context, *sc.Run, C) iter.Seq2[any, error] {
	return func(ctx context.Context, _ *sc.Run, _ C) iter.Seq2[any, error] {
		return func(func(any, error) bool) {
			started()
			defer stopped()

			<-ctx.Done()
		}
	}
}

// xinvFailing is a stream standing for an XState callback actor that throws
// as it starts.
func xinvFailing[C any](context.Context, *sc.Run, C) iter.Seq2[any, error] {
	return func(yield func(any, error) bool) {
		yield(nil, errXinvTest)
	}
}

// xinvPromiseOf is a service that succeeds with value.
func xinvPromiseOf[C, R any](value R) func(context.Context, *sc.Run, C) (R, error) {
	return func(context.Context, *sc.Run, C) (R, error) { return value, nil }
}

// xinvNoOutput is the output of a child machine whose result is not used.
func xinvNoOutput[C any](sc.Snapshot[string, C]) (struct{}, error) {
	return struct{}{}, nil
}

func xinvSame(c counter) counter { return c }

// xinvInstance starts an instance that is closed when the test ends.
func xinvInstance[C any](t *testing.T, m *sc.Machine[string, C], c C) *sc.Instance[string, C] {
	t.Helper()

	inst, err := m.Instance(t.Context(), c)
	require.NoError(t, err)
	t.Cleanup(inst.Close)

	return inst
}

func xinvSend[C any](t *testing.T, inst *sc.Instance[string, C], ev any) sc.Step[string, C] {
	t.Helper()

	step, err := inst.Send(t.Context(), ev)
	require.NoError(t, err)

	return step
}

func xinvAwait[C any](t *testing.T, inst *sc.Instance[string, C], cond func(sc.Snapshot[string, C]) bool) sc.Snapshot[string, C] {
	t.Helper()

	snap, err := inst.Await(t.Context(), cond)
	require.NoError(t, err)

	return snap
}

// xinvDone waits for the machine to complete.
func xinvDone[C any](t *testing.T, inst *sc.Instance[string, C]) sc.Snapshot[string, C] {
	t.Helper()

	return xinvAwait(t, inst, func(s sc.Snapshot[string, C]) bool { return s.Done() })
}

// xinvCountKind counts the effects of one kind.
func xinvCountKind[C any](effects []sc.Effect[C], kind sc.EffectKind) int {
	n := 0
	for _, e := range effects {
		if e.Kind == kind {
			n++
		}
	}

	return n
}

// XState: child can immediately respond to the parent with multiple events
func TestXInvChildCanImmediatelyRespondToTheParentWithMultipleEvents(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		dec := func(counter, xinvForwardDec) xinvDec { return xinvDec{} }

		child, err := sc.New("child", func(b *xinvBuilder) {
			b.Initial("init")
			b.State("init").On[xinvForwardDec]().
				Emit("dec1", dec).
				Emit("dec2", dec).
				Emit("dec3", dec)
		})
		require.NoError(t, err)

		parent, err := sc.New("parent", func(b *xinvBuilder) {
			b.Initial("start")
			start := b.State("start")
			someService := start.InvokeMachine("someService", child, xinvSame, xinvNoOutput[counter])
			someService.OnError().Stay()
			start.Always().When("minus three", func(c counter, _ struct{}) bool { return c.N == -3 }).To("stop")
			start.On[xinvDec]().Assign("decrement", func(c counter, _ xinvDec) counter { c.N--; return c })
			start.On[xinvForwardDec]().SendTo("forward", someService, func(_ counter, ev xinvForwardDec) any { return ev })
			b.Final("stop")
		})
		require.NoError(t, err)

		inst := xinvInstance(t, parent, counter{N: 0})
		synctest.Wait()

		xinvSend(t, inst, xinvForwardDec{})

		snap := xinvDone(t, inst)
		require.Equal(t, -3, snap.Context.N)
		require.Equal(t, []string{"stop"}, snap.Active())
	})
}

// XState: should start services (explicit machine, invoke = config)
//
// The child's final state assigns the user from the RESOLVE event on entry
// in XState; an sc entry assignment sees no event, so the transition that
// enters the final state assigns it. XState's child also declares a state
// "failure" that nothing reaches, which sc accepts when told to.
func TestXInvStartsServicesExplicitMachineInvokeConfig(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		child, err := sc.New("fetch", func(b *sc.Builder[string, xinvFetchChild]) {
			b.Initial("pending")
			pending := b.State("pending")
			pending.RaiseOnEntry("resolve", func(xinvFetchChild) any { return xinvResolve{User: "David"} })
			pending.On[xinvResolve]().
				When("has user id", func(c xinvFetchChild, _ xinvResolve) bool { return c.UserID != "" }).
				Assign("keep user", func(c xinvFetchChild, ev xinvResolve) xinvFetchChild { c.User = ev.User; return c }).
				To("success")
			b.Final("success")
			b.State("failure").EmitOnEntry("reject", func(xinvFetchChild) xinvReject { return xinvReject{} })
		}, sc.AllowUnreachable())
		require.NoError(t, err)

		m, err := sc.New("fetcher", func(b *sc.Builder[string, xinvFetcher]) {
			b.Initial("idle")
			b.State("idle").On[xinvGoToWaiting]().To("waiting")
			fetch := b.State("waiting").InvokeMachine("fetch", child,
				func(c xinvFetcher) xinvFetchChild { return xinvFetchChild{UserID: c.SelectedUserID, User: ""} },
				func(snap sc.Snapshot[string, xinvFetchChild]) (string, error) { return snap.Context.User, nil },
			)
			fetch.OnDone().
				When("is David", func(_ xinvFetcher, d sc.Done[string]) bool { return d.Result == "David" }).
				To("received")
			fetch.OnError().Stay()
			b.Final("received")
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, xinvFetcher{SelectedUserID: "42", User: ""})
		xinvSend(t, inst, xinvGoToWaiting{})

		snap := xinvDone(t, inst)
		require.Equal(t, []string{"received"}, snap.Active())
	})
}

// XState: should start services (explicit machine, invoke = machine)
func TestXInvStartsServicesExplicitMachineInvokeMachine(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		child, err := sc.New("child", func(b *xinvBuilder) {
			b.Initial("pending")
			pending := b.State("pending")
			pending.RaiseOnEntry("resolve", func(counter) any { return xinvResolve{User: ""} })
			pending.On[xinvResolve]().To("success")
			b.Final("success")
		})
		require.NoError(t, err)

		m, err := sc.New("m", func(b *xinvBuilder) {
			b.Initial("idle")
			b.State("idle").On[xinvGoToWaiting]().To("waiting")
			invoked := b.State("waiting").InvokeMachine("child", child, xinvSame, xinvNoOutput[counter])
			invoked.OnDone().To("received")
			invoked.OnError().Stay()
			b.Final("received")
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, counter{N: 0})
		xinvSend(t, inst, xinvGoToWaiting{})

		snap := xinvDone(t, inst)
		require.Equal(t, []string{"received"}, snap.Active())
	})
}

// xinvSendingChild is XState's child machine whose entry action sends
// SUCCESS with 42 to its parent.
func xinvSendingChild(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	child, err := sc.New("child", func(b *xinvBuilder) {
		b.Initial("sending")
		b.State("sending").EmitOnEntry("report", func(counter) xinvSuccess { return xinvSuccess{Data: 42} })
	})
	require.NoError(t, err)

	return child
}

// XState: should start services (machine as invoke config)
func TestXInvStartsServicesMachineAsInvokeConfig(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("machine-invoke", func(b *xinvBuilder) {
			b.Initial("pending")
			pending := b.State("pending")
			pending.InvokeMachine("child", xinvSendingChild(t), xinvSame, xinvNoOutput[counter]).OnError().Stay()
			pending.On[xinvSuccess]().
				When("is 42", func(_ counter, ev xinvSuccess) bool { return ev.Data == 42 }).
				To("success")
			b.Final("success")
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, counter{N: 0})

		snap := xinvDone(t, inst)
		require.Equal(t, []string{"success"}, snap.Active())
	})
}

// XState: should start deeply nested service (machine as invoke config)
func TestXInvStartsDeeplyNestedServiceMachineAsInvokeConfig(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("parent", func(b *xinvBuilder) {
			b.Initial("a")
			b.State("a", func(b *xinvBuilder) {
				b.Initial("b")
				b.State("b").InvokeMachine("child", xinvSendingChild(t), xinvSame, xinvNoOutput[counter]).OnError().Stay()
			})
			b.Final("success")
			b.On[xinvSuccess]().
				When("is 42", func(_ counter, ev xinvSuccess) bool { return ev.Data == 42 }).
				To("success")
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, counter{N: 0})

		snap := xinvDone(t, inst)
		require.Equal(t, []string{"success"}, snap.Active())
	})
}

// xinvSubMachine is the child of XState's "parent to child" cases: NEXT takes
// it to "two", whose entry action sends NEXT to the parent.
func xinvSubMachine(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	child, err := sc.New("child", func(b *xinvBuilder) {
		b.Initial("one")
		b.State("one").On[xinvNext]().To("two")
		b.State("two").EmitOnEntry("tell parent", func(counter) xinvNext { return xinvNext{} })
	})
	require.NoError(t, err)

	return child
}

// XState: should communicate with the child machine (invoke on machine)
//
// XState skips this case (it.skip): there the child the root invokes does
// not exist yet when the entry action of "one" sends to it. sc enters the
// chart, starting its services, before the chart's states, so the send
// reaches the child and the case is ported with the outcome XState means.
func TestXInvCommunicatesWithTheChildMachineInvokeOnMachine(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("parent", func(b *xinvBuilder) {
			fooChild := b.Chart().InvokeMachine("foo-child", xinvSubMachine(t), xinvSame, xinvNoOutput[counter])
			fooChild.OnError().Stay()

			b.Initial("one")
			one := b.State("one").SendOnEntry("send next", fooChild, func(counter) any { return xinvNext{} })
			one.On[xinvNext]().To("two")
			b.Final("two")
		})
		require.NoError(t, err)

		// The chart's child is started before the state that sends to it
		// is entered.
		snap, effects := m.Start(counter{N: 0}, epoch)
		require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation, sc.EffectSend}, kinds(effects))
		require.Equal(t, effects[0].ID, effects[1].ID)
		require.Equal(t, "parent", effects[0].State)
		require.Empty(t, snap.Services()[0].State)

		inst := xinvInstance(t, m, counter{N: 0})

		done := xinvDone(t, inst)
		require.Equal(t, []string{"two"}, done.Active())

		// Finishing stopped the chart's child.
		require.Empty(t, done.Services())
	})
}

// XState: should communicate with the child machine (invoke on state)
func TestXInvCommunicatesWithTheChildMachineInvokeOnState(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("parent", func(b *xinvBuilder) {
			b.Initial("one")
			one := b.State("one")
			fooChild := one.InvokeMachine("foo-child", xinvSubMachine(t), xinvSame, xinvNoOutput[counter])
			fooChild.OnError().Stay()
			one.SendOnEntry("send next", fooChild, func(counter) any { return xinvNext{} })
			one.On[xinvNext]().To("two")
			b.Final("two")
		})
		require.NoError(t, err)

		// The step that starts the child also sends to it.
		_, effects := m.Start(counter{N: 0}, epoch)
		require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation, sc.EffectSend}, kinds(effects))
		require.Equal(t, effects[0].ID, effects[1].ID)

		inst := xinvInstance(t, m, counter{N: 0})

		snap := xinvDone(t, inst)
		require.Equal(t, []string{"two"}, snap.Active())
	})
}

// XState: should transition correctly if child invocation causes it to directly go to final state
func TestXInvTransitionsCorrectlyIfChildInvocationGoesDirectlyToFinalState(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		child, err := sc.New("child", func(b *xinvBuilder) {
			b.Initial("one")
			b.State("one").On[xinvNext]().To("two")
			b.Final("two")
		})
		require.NoError(t, err)

		m, err := sc.New("parent", func(b *xinvBuilder) {
			b.Initial("one")
			one := b.State("one")
			invoked := one.InvokeMachine("foo-child", child, xinvSame, xinvNoOutput[counter])
			one.SendOnEntry("send next", invoked, func(counter) any { return xinvNext{} })
			invoked.OnDone().To("two")
			invoked.OnError().Stay()
			b.State("two").On[xinvNext]().To("three")
			b.Final("three")
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, counter{N: 0})
		synctest.Wait()

		require.Equal(t, []string{"two"}, inst.Snapshot().Active())
	})
}

// XState: should work with invocations defined in orthogonal state nodes
//
// XState's test resolves nothing it waits for; what its machine does is
// complete once its one region has, which the port checks.
func TestXInvWorksWithInvocationsDefinedInOrthogonalStateNodes(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		pong, err := sc.New("pong", func(b *xinvBuilder) {
			b.Initial("active")
			b.Final("active").Output(func(counter) string { return "pingpong" })
		})
		require.NoError(t, err)

		ping, err := sc.New("ping", func(b *xinvBuilder) {
			b.Regions()
			b.State("one", func(b *xinvBuilder) {
				b.Initial("one.active")
				invoked := b.State("one.active").InvokeMachine("pong", pong, xinvSame,
					func(snap sc.Snapshot[string, counter]) (string, error) {
						output, _ := pong.Output(snap)
						secret, _ := output.(string)

						return secret, nil
					},
				)
				invoked.OnDone().
					When("has the secret", func(_ counter, d sc.Done[string]) bool { return d.Result == "pingpong" }).
					To("one.success")
				invoked.OnError().Stay()
				b.Final("one.success")
			})
		})
		require.NoError(t, err)

		inst := xinvInstance(t, ping, counter{N: 0})

		snap := xinvDone(t, inst)
		require.Equal(t, []string{"one", "one.success"}, snap.Active())
	})
}

// XState: should not reinvoke root-level invocations on root non-reentering transitions
func TestXInvDoesNotReinvokeRootLevelInvocationsOnRootNonReenteringTransitions(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var invoked, disposed, actions, entries atomic.Int32

		m, err := sc.New("m", func(b *xinvBuilder) {
			chart := b.Chart()
			chart.Entry("count entry", func(context.Context, counter) { entries.Add(1) })
			chart.Stream("callback", xinvDisposable[counter](
				func() { invoked.Add(1) },
				func() { disposed.Add(1) },
			)).OnError().Stay()
			chart.On[xinvUpdate]().Do("count action", func(context.Context, counter, xinvUpdate) { actions.Add(1) })
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, counter{N: 0})
		synctest.Wait()

		check := func(wantActions int32) {
			require.Equal(t, int32(1), entries.Load())
			require.Equal(t, int32(1), invoked.Load())
			require.Equal(t, int32(0), disposed.Load())
			require.Equal(t, wantActions, actions.Load())
		}
		check(0)

		for i := range int32(2) {
			step := xinvSend(t, inst, xinvUpdate{})
			require.Equal(t, []sc.EffectKind{sc.EffectAction}, kinds(step.Effects))
			synctest.Wait()
			check(i + 1)
		}
	})
}

// xinvRootInvokeChart is XState's machine that invokes a callback actor at
// its root and completes on finish: the chart's service runs until the
// machine is done.
func xinvRootInvokeChart[Ev any](t *testing.T, stream func(context.Context, *sc.Run, counter) iter.Seq2[any, error]) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("machine", func(b *xinvBuilder) {
		b.Chart().Stream("test", stream).OnError().Stay()

		b.Initial("running")
		b.State("running").On[Ev]().To("complete")
		b.Final("complete")
	})
	require.NoError(t, err)

	return m
}

// XState: should stop a child actor when reaching a final state
func TestXInvStopsAChildActorWhenReachingAFinalState(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var stopped atomic.Bool

		m := xinvRootInvokeChart[xinvFinished](t, xinvDisposable[counter](func() {}, func() { stopped.Store(true) }))

		inst := xinvInstance(t, m, counter{N: 0})
		synctest.Wait()

		// XState: the child "test" is defined.
		services := inst.Snapshot().Services()
		require.Len(t, services, 1)
		require.Equal(t, "test", services[0].Name)

		step := xinvSend(t, inst, xinvFinished{})

		require.True(t, step.To.Done())
		require.Empty(t, step.To.Services())
		require.True(t, stopped.Load())
	})
}

// XState: child should not invoke an actor when it transitions to an invoking state when it gets stopped by its parent
//
// DIVERGES from XState: XState's child sends STOPPED to its parent inside
// its own step, the parent stops the child before that step's invocations
// start, and the callback of "idle" runs exactly once. sc hands the event a
// child emits to its parent on another goroutine, so the child goes on with
// its step and starts the service of "idle" a second time before the parent
// stops it: the count is 2 in practice, and 1 if the parent is quicker.
func TestXInvChildDoesNotInvokeAnActorWhenStoppedByItsParent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var invoked atomic.Int32

		child, err := sc.New("child", func(b *xinvBuilder) {
			b.Initial("idle")
			idle := b.State("idle")
			idle.Stream("starter", func(ctx context.Context, _ *sc.Run, _ counter) iter.Seq2[any, error] {
				return func(yield func(any, error) bool) {
					// Only the first run sends, so the child cannot loop.
					if invoked.Add(1) == 1 && !yield(xinvStarted{}, nil) {
						return
					}

					<-ctx.Done()
				}
			}).OnError().Stay()
			idle.On[xinvStarted]().To("active")

			active := b.State("active")
			active.Stream("stopper", xinvCallbackOf[counter](xinvStopped{})).OnError().Stay()
			active.On[xinvStopped]().
				Emit("tell parent", func(_ counter, ev xinvStopped) xinvStopped { return ev }).
				To("idle")
		})
		require.NoError(t, err)

		parent, err := sc.New("parent", func(b *xinvBuilder) {
			b.Initial("idle")
			b.State("idle").On[xinvStart]().To("active")
			active := b.State("active")
			active.InvokeMachine("child", child, xinvSame, xinvNoOutput[counter]).OnError().Stay()
			active.On[xinvStopped]().To("done")
			b.Final("done")
		})
		require.NoError(t, err)

		inst := xinvInstance(t, parent, counter{N: 0})
		xinvSend(t, inst, xinvStart{})

		snap := xinvDone(t, inst)
		require.Equal(t, []string{"done"}, snap.Active())

		synctest.Wait()
		require.Contains(t, []int32{1, 2}, invoked.Load())
	})
}

// xinvPendingChart is the chart most of XState's promise cases use: pending
// invokes a service, and its completion leads to the final state success.
func xinvPendingChart[C, R any](t *testing.T, service func(context.Context, *sc.Run, C) (R, error), done func(*sc.Transition[string, C, sc.Done[R]])) *sc.Machine[string, C] {
	t.Helper()

	m, err := sc.New("promise", func(b *sc.Builder[string, C]) {
		b.Initial("pending")
		inv := b.State("pending").Invoke("promise", service)
		done(inv.OnDone().To("success"))
		inv.OnError().Stay()
		b.Final("success")
	})
	require.NoError(t, err)

	return m
}

// xinvPlain adds nothing to an OnDone transition.
func xinvPlain[C, R any](*sc.Transition[string, C, sc.Done[R]]) {}

// XState: should be invoked with a promise factory and resolve through onDone
func TestXInvPromiseResolvesThroughOnDone(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xinvPendingChart(t, xinvPromiseOf[counter](struct{}{}), xinvPlain[counter, struct{}])

		inst := xinvInstance(t, m, counter{N: 0})

		snap := xinvDone(t, inst)
		require.Equal(t, []string{"success"}, snap.Active())
	})
}

// XState: should be invoked with a promise factory and reject with ErrorExecution
func TestXInvPromiseRejectsThroughOnError(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("invokePromise", func(b *sc.Builder[string, xinvPromise]) {
			b.Initial("pending")
			inv := b.State("pending").Invoke("promise", func(_ context.Context, _ *sc.Run, c xinvPromise) (int, error) {
				if !c.Succeed {
					return 0, fmt.Errorf("%w: failed on purpose for: %d", errXinvTest, c.ID)
				}

				return c.ID, nil
			})
			inv.OnDone().
				When("is the id", func(c xinvPromise, d sc.Done[int]) bool { return d.Result == c.ID }).
				To("success")
			inv.OnError().To("failure")
			b.Final("success")
			b.Final("failure")
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, xinvPromise{ID: 31, Succeed: false})

		snap := xinvDone(t, inst)
		require.Equal(t, []string{"failure"}, snap.Active())

		// With XState's default input the same machine succeeds.
		inst = xinvInstance(t, m, xinvPromise{ID: 42, Succeed: true})

		snap = xinvDone(t, inst)
		require.Equal(t, []string{"success"}, snap.Active())
	})
}

// xinvUnhandledChart is XState's machine whose invoked actor fails and which
// declares no onError. withOnError adds the OnError().Stay() sc asks for.
func xinvUnhandledChart(withOnError bool, service func(*sc.StateBuilder[string, counter]) *sc.Invocation[string, counter, struct{}]) (*sc.Machine[string, counter], error) {
	m, err := sc.New("invokePromise", func(b *xinvBuilder) {
		b.Initial("pending")
		inv := service(b.State("pending"))
		inv.OnDone().To("success")
		if withOnError {
			inv.OnError().Stay()
		}
		b.Final("success")
	})
	if err != nil {
		return nil, fmt.Errorf("declare chart: %w", err)
	}

	return m, nil
}

// xinvCheckUnhandledError checks what sc does with a failing service whose
// chart handles no error: the chart is rejected without an OnError, and with
// OnError().Stay() the failure is consumed and the machine goes on waiting.
func xinvCheckUnhandledError(t *testing.T, service func(*sc.StateBuilder[string, counter]) *sc.Invocation[string, counter, struct{}]) {
	t.Helper()

	_, err := xinvUnhandledChart(false, service)
	require.ErrorContains(t, err, `state "pending": service "failing" has no OnError transition`)

	synctest.Test(t, func(t *testing.T) {
		m, err := xinvUnhandledChart(true, service)
		require.NoError(t, err)

		inst, err := m.Instance(t.Context(), counter{N: 0})
		require.NoError(t, err)
		defer inst.Close()

		synctest.Wait()

		snap := inst.Snapshot()
		require.Equal(t, []string{"pending"}, snap.Active())
		require.False(t, snap.Done())
		require.Empty(t, snap.Services())
	})
}

func xinvFailingPromise(s *sc.StateBuilder[string, counter]) *sc.Invocation[string, counter, struct{}] {
	return s.Invoke("failing", func(context.Context, *sc.Run, counter) (struct{}, error) { return struct{}{}, errXinvTest })
}

func xinvFailingCallback(s *sc.StateBuilder[string, counter]) *sc.Invocation[string, counter, struct{}] {
	return s.Stream("failing", xinvFailing[counter])
}

// XState: should be invoked with a promise factory and surface any unhandled errors
//
// DIVERGES from XState: XState lets a machine leave onError out, and the
// rejection then errors the machine's own actor, whose error observer is
// called with it. sc rejects a chart whose service has no OnError, and has
// no errored status for an instance: with OnError().Stay() the failure is
// consumed and the instance stays in "pending", neither done nor failed.
func TestXInvPromiseSurfacesUnhandledErrors(t *testing.T) {
	t.Parallel()

	xinvCheckUnhandledError(t, xinvFailingPromise)
}

// XState: should be invoked with a promise factory and stop on unhandled onError target
//
// DIVERGES from XState: XState's actor stops with the error and never
// completes. sc rejects the chart without an OnError; with OnError().Stay()
// the instance does not complete either, but it is not stopped: it stays in
// "pending" and goes on accepting events.
func TestXInvPromiseStopsOnUnhandledOnErrorTarget(t *testing.T) {
	t.Parallel()

	xinvCheckUnhandledError(t, xinvFailingPromise)
}

// xinvCompoundPromiseChart is XState's machine whose compound state completes
// when the promise invoked inside it resolves.
func xinvCompoundPromiseChart(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("promise", func(b *xinvBuilder) {
		b.Initial("parent")
		b.State("parent", func(b *xinvBuilder) {
			b.Initial("pending")
			inv := b.State("pending").Invoke("promise", xinvPromiseOf[counter](struct{}{}))
			inv.OnDone().To("parent.success")
			inv.OnError().Stay()
			b.Final("parent.success")
		}).OnDone().To("success")
		b.Final("success")
	})
	require.NoError(t, err)

	return m
}

// XState: should be invoked with a promise factory and resolve through onDone for compound state nodes
func TestXInvPromiseFactoryResolvesThroughOnDoneForCompoundStateNodes(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		inst := xinvInstance(t, xinvCompoundPromiseChart(t), counter{N: 0})

		snap := xinvDone(t, inst)
		require.Equal(t, []string{"success"}, snap.Active())
	})
}

// XState: should be invoked with a promise service and resolve through onDone for compound state nodes
//
// XState tells a promise declared inline from one declared beforehand; sc
// has one way to declare a service, so the chart is the same as the one
// before. The pure core is used here: the service's result is given as its
// Done event.
func TestXInvPromiseServiceResolvesThroughOnDoneForCompoundStateNodes(t *testing.T) {
	t.Parallel()

	m := xinvCompoundPromiseChart(t)

	snap, effects := m.Start(counter{N: 0}, epoch)
	run := effect(t, effects, sc.EffectStartInvocation)
	require.Equal(t, []string{"parent", "pending"}, snap.Active())

	snap, _ = m.Transition(snap, sc.Done[struct{}]{ID: run.ID, Result: struct{}{}}, epoch)
	require.Equal(t, []string{"success"}, snap.Active())
	require.True(t, snap.Done())
}

// xinvCountPromise resolves with XState's { count: 1 }.
func xinvCountPromise(context.Context, *sc.Run, counter) (counter, error) { return counter{N: 1}, nil }

// XState: should assign the resolved data when invoked with a promise factory
func TestXInvAssignsTheResolvedDataOfAPromiseFactory(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xinvPendingChart(t, xinvCountPromise, func(done *sc.Transition[string, counter, sc.Done[counter]]) {
			done.Assign("keep count", func(_ counter, d sc.Done[counter]) counter { return d.Result })
		})

		inst := xinvInstance(t, m, counter{N: 0})

		require.Equal(t, 1, xinvDone(t, inst).Context.N)
	})
}

// XState: should provide resolved output directly to onDone
func TestXInvProvidesResolvedOutputDirectlyToOnDone(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := xinvPendingChart(t,
			func(context.Context, *sc.Run, xinvText) (xinvText, error) { return xinvText{Value: "David"}, nil },
			func(done *sc.Transition[string, xinvText, sc.Done[xinvText]]) {
				done.Assign("keep name", func(c xinvText, d sc.Done[xinvText]) xinvText { c.Value = d.Result.Value; return c })
			},
		)

		inst := xinvInstance(t, m, xinvText{Value: ""})

		require.Equal(t, "David", xinvDone(t, inst).Context.Value)
	})
}

// XState: should assign the resolved data when invoked with a promise service
//
// As above, sc does not tell a service declared beforehand from one declared
// inline. The pure core is used here.
func TestXInvAssignsTheResolvedDataOfAPromiseService(t *testing.T) {
	t.Parallel()

	m := xinvPendingChart(t, xinvCountPromise, func(done *sc.Transition[string, counter, sc.Done[counter]]) {
		done.Assign("keep count", func(_ counter, d sc.Done[counter]) counter { return d.Result })
	})

	snap, effects := m.Start(counter{N: 0}, epoch)
	run := effect(t, effects, sc.EffectStartInvocation)

	snap, _ = m.Transition(snap, sc.Done[counter]{ID: run.ID, Result: counter{N: 1}}, epoch)
	require.True(t, snap.Done())
	require.Equal(t, 1, snap.Context.N)
}

// XState: should provide the resolved data when invoked with a promise factory
//
// XState's transition function reads the output as it decides; the sc port
// reads it in a guard.
func TestXInvProvidesTheResolvedDataOfAPromiseFactory(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var count atomic.Int32

		m := xinvPendingChart(t, xinvCountPromise, func(done *sc.Transition[string, counter, sc.Done[counter]]) {
			done.When("sees the count", func(_ counter, d sc.Done[counter]) bool {
				count.Store(int32(d.Result.N)) //nolint:gosec // the count is 1.
				return true
			})
		})

		inst := xinvInstance(t, m, counter{N: 0})
		xinvDone(t, inst)

		require.Equal(t, int32(1), count.Load())
	})
}

// XState: should provide the resolved data when invoked with a promise service
func TestXInvProvidesTheResolvedDataOfAPromiseService(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var count atomic.Int32

		m := xinvPendingChart(t, xinvCountPromise, func(done *sc.Transition[string, counter, sc.Done[counter]]) {
			done.Do("read the count", func(_ context.Context, _ counter, d sc.Done[counter]) {
				count.Store(int32(d.Result.N)) //nolint:gosec // the count is 1.
			})
		})

		inst := xinvInstance(t, m, counter{N: 0})
		xinvDone(t, inst)

		require.Equal(t, int32(1), count.Load())
	})
}

// XState: should be able to specify a Promise as a service
//
// XState's input mapper hands the promise the BEGIN event that entered the
// state; the sc service declares it with InvokeOn.
func TestXInvSpecifiesAPromiseAsAService(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rejected := errors.New("rejected")

		m, err := sc.New("promise", func(b *sc.Builder[string, xinvFlags]) {
			b.Initial("pending")
			b.State("pending").On[xinvBegin]().To("first")
			inv := b.State("first").InvokeOn[xinvBegin]("promise", func(_ context.Context, _ *sc.Run, c xinvFlags, begin xinvBegin) (struct{}, error) {
				if c.Foo && begin.Payload {
					return struct{}{}, nil
				}

				return struct{}{}, rejected
			})
			inv.OnDone().To("last")
			inv.OnError().Stay()
			b.Final("last")
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, xinvFlags{Foo: true})
		xinvSend(t, inst, xinvBegin{Payload: true})

		snap := xinvDone(t, inst)
		require.Equal(t, []string{"last"}, snap.Active())
	})
}

// XState: should be able to reuse the same promise logic multiple times and create unique promise for each created actor
//
// XState's promise resolves with a random number; the port's service counts
// its calls, so that two runs sharing one result would show.
func TestXInvReusesTheSamePromiseLogicWithAUniquePromisePerActor(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		next := func(context.Context, *sc.Run, xinvPair) (int, error) { return int(calls.Add(1)), nil }

		region := func(b *sc.Builder[string, xinvPair], name string, keep func(xinvPair, sc.Done[int]) xinvPair) {
			b.State(name, func(b *sc.Builder[string, xinvPair]) {
				b.Initial(name + ".active")
				inv := b.State(name+".active").Invoke("getRandomNumber", next)
				inv.OnDone().Assign("keep result", keep).To(name + ".success")
				inv.OnError().Stay()
				b.Final(name + ".success")
			})
		}

		m, err := sc.New("m", func(b *sc.Builder[string, xinvPair]) {
			b.Initial("pending")
			b.Parallel("pending", func(b *sc.Builder[string, xinvPair]) {
				region(b, "state1", func(c xinvPair, d sc.Done[int]) xinvPair { c.First = d.Result; return c })
				region(b, "state2", func(c xinvPair, d sc.Done[int]) xinvPair { c.Second = d.Result; return c })
			}).OnDone().To("done")
			b.Final("done")
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, xinvPair{First: 0, Second: 0})

		snap := xinvDone(t, inst)
		require.NotZero(t, snap.Context.First)
		require.NotZero(t, snap.Context.Second)
		require.NotEqual(t, snap.Context.First, snap.Context.Second)
		require.Equal(t, int32(2), calls.Load())
	})
}

// XState: should not emit onSnapshot if stopped
//
// Partial port: sc has no onSnapshot. What is ported is that a promise that
// resolves after its state was left reaches the machine in no form: the
// service ignores its cancellation and returns 42 once the state is left.
func TestXInvAStoppedPromiseReportsNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var returned atomic.Bool

		m, err := sc.New("m", func(b *xinvBuilder) {
			b.Initial("active")
			active := b.State("active")
			inv := active.Invoke("promise", func(context.Context, *sc.Run, counter) (int, error) {
				<-time.After(5 * time.Millisecond)
				returned.Store(true)

				return 42, nil
			})
			inv.OnDone().Assign("unexpected result", func(c counter, d sc.Done[int]) counter { c.N = d.Result; return c })
			inv.OnError().Stay()
			active.On[xinvDeactivate]().To("inactive")
			b.State("inactive").OnAny().Assign("unexpected event", func(c counter, _ any) counter { c.N = -1; return c })
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, counter{N: 0})

		step := xinvSend(t, inst, xinvDeactivate{})
		require.Equal(t, []string{"inactive"}, step.To.Active())

		<-time.After(10 * time.Millisecond)
		synctest.Wait()

		require.True(t, returned.Load())
		require.Equal(t, step.To.Seq(), inst.Snapshot().Seq())
		require.Equal(t, 0, inst.Snapshot().Context.N)
	})
}

// XState: should be able to specify a callback as a service
//
// As for the promise, the callback's input holds the BEGIN event in XState.
// An sc stream cannot declare the event its state was entered with, as
// InvokeOn does for a service, so the BEGIN transition records it in the
// context the stream starts with.
func TestXInvSpecifiesACallbackAsAService(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("callback", func(b *sc.Builder[string, xinvBegun]) {
			b.Initial("pending")
			b.State("pending").On[xinvBegin]().
				Assign("record begin", func(c xinvBegun, _ xinvBegin) xinvBegun { c.Began = true; return c }).
				To("first")
			first := b.State("first")
			first.Stream("someCallback", func(ctx context.Context, _ *sc.Run, c xinvBegun) iter.Seq2[any, error] {
				return func(yield func(any, error) bool) {
					if c.Foo && c.Began {
						for _, data := range []int{40, 41, 42} {
							if !yield(xinvCallback{Data: data}, nil) {
								return
							}
						}
					}

					<-ctx.Done()
				}
			}).OnError().Stay()
			first.On[xinvCallback]().
				When("is 42", func(_ xinvBegun, ev xinvCallback) bool { return ev.Data == 42 }).
				To("last")
			b.Final("last")
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, xinvBegun{Foo: true, Began: false})
		xinvSend(t, inst, xinvBegin{Payload: true})

		snap := xinvDone(t, inst)
		require.Equal(t, []string{"last"}, snap.Active())
	})
}

// XState: should transition correctly if callback function sends an event
func TestXInvTransitionsCorrectlyIfCallbackFunctionSendsAnEvent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("callback", func(b *xinvBuilder) {
			b.Initial("pending")
			b.State("pending").On[xinvBegin]().To("first")
			first := b.State("first")
			first.Stream("someCallback", xinvCallbackOf[counter](xinvCallback{Data: 0})).OnError().Stay()
			first.On[xinvCallback]().To("intermediate")
			b.State("intermediate").On[xinvNext]().To("last")
			b.Final("last")
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, counter{N: 0})
		require.Equal(t, []string{"pending"}, inst.Snapshot().Active())

		step := xinvSend(t, inst, xinvBegin{Payload: false})
		require.Equal(t, []string{"first"}, step.To.Active())

		synctest.Wait()
		require.Equal(t, []string{"intermediate"}, inst.Snapshot().Active())
	})
}

// XState: should transition correctly if callback function invoked from start and sends an event
func TestXInvTransitionsCorrectlyIfCallbackInvokedFromStartSendsAnEvent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("callback", func(b *xinvBuilder) {
			b.Initial("idle")
			idle := b.State("idle")
			idle.Stream("someCallback", xinvCallbackOf[counter](xinvCallback{Data: 0})).OnError().Stay()
			idle.On[xinvCallback]().To("intermediate")
			b.State("intermediate").On[xinvNext]().To("last")
			b.Final("last")
		})
		require.NoError(t, err)

		snap, _ := m.Start(counter{N: 0}, epoch)
		require.Equal(t, []string{"idle"}, snap.Active())

		inst := xinvInstance(t, m, counter{N: 0})
		synctest.Wait()
		require.Equal(t, []string{"intermediate"}, inst.Snapshot().Active())

		// DIVERGES from XState: XState sends BEGIN, which nothing handles,
		// and ignores it. sc refuses an event of a type no transition of
		// the chart names, and the send changes nothing.
		step, err := inst.Send(t.Context(), xinvBegin{Payload: false})
		require.ErrorIs(t, err, sc.ErrUnknownEvent)
		require.False(t, step.Accepted())
		require.Equal(t, []string{"intermediate"}, inst.Snapshot().Active())
	})
}

// XState: should transition correctly if transient transition happens before current state invokes callback function and sends an event
func TestXInvTransitionsCorrectlyIfTransientTransitionHappensBeforeTheInvokingState(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("callback", func(b *xinvBuilder) {
			b.Initial("pending")
			b.State("pending").On[xinvBegin]().To("first")
			b.State("first").Always().To("second")
			second := b.State("second")
			second.Stream("someCallback", xinvCallbackOf[counter](xinvCallback{Data: 0})).OnError().Stay()
			second.On[xinvCallback]().To("third")
			b.State("third").On[xinvNext]().To("last")
			b.Final("last")
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, counter{N: 0})
		require.Equal(t, []string{"pending"}, inst.Snapshot().Active())

		step := xinvSend(t, inst, xinvBegin{Payload: false})
		require.Equal(t, []string{"second"}, step.To.Active())

		synctest.Wait()
		require.Equal(t, []string{"third"}, inst.Snapshot().Active())
	})
}

// xinvInterval is a stream standing for XState's interval(10): it yields
// what each(i) returns for 0, 1, 2 and so on, ten milliseconds apart. It
// ends after limit values when limit is positive, with failure when that is
// not nil.
func xinvInterval(limit int, failure error, each func(i int) any) func(context.Context, *sc.Run, counter) iter.Seq2[any, error] {
	return func(ctx context.Context, _ *sc.Run, _ counter) iter.Seq2[any, error] {
		return func(yield func(any, error) bool) {
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()

			for i := 0; limit <= 0 || i < limit; i++ {
				select {
				case <-ticker.C:
				case <-ctx.Done():
					return
				}

				if !yield(each(i), nil) {
					return
				}
			}

			if failure != nil {
				yield(nil, failure)
			}
		}
	}
}

// XState: should treat a callback source as an event stream
func TestXInvTreatsACallbackSourceAsAnEventStream(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("interval", func(b *xinvBuilder) {
			b.Initial("counting")
			counting := b.State("counting")
			counting.Stream("intervalService", xinvInterval(0, nil, func(int) any { return xinvInc{} })).OnError().Stay()
			counting.Always().When("is 3", func(c counter, _ struct{}) bool { return c.N == 3 }).To("finished")
			counting.On[xinvInc]().Assign("increment", increment[xinvInc])
			b.Final("finished")
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, counter{N: 0})

		snap := xinvDone(t, inst)
		require.Equal(t, 3, snap.Context.N)
		require.Equal(t, []string{"finished"}, snap.Active())
	})
}

// XState: should dispose of the callback (if disposal function provided)
func TestXInvDisposesOfTheCallback(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var disposed atomic.Int32

		m, err := sc.New("interval", func(b *xinvBuilder) {
			b.Initial("counting")
			counting := b.State("counting")
			counting.Stream("intervalService", xinvDisposable[counter](func() {}, func() { disposed.Add(1) })).OnError().Stay()
			counting.On[xinvNext]().To("idle")
			b.State("idle")
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, counter{N: 0})
		synctest.Wait()

		xinvSend(t, inst, xinvNext{})

		// Send returns once the stopped service's goroutine has returned.
		require.Equal(t, int32(1), disposed.Load())
	})
}

// xinvPonger is a stream standing for an actor that answers each PING it is
// sent with a PONG.
func xinvPonger(_ context.Context, _ *sc.Run, _ counter, inbox iter.Seq[xinvPing]) iter.Seq2[any, error] {
	return func(yield func(any, error) bool) {
		for range inbox {
			if !yield(xinvPong{}, nil) {
				return
			}
		}
	}
}

// xinvPingPongChart is XState's machine whose state sends PING to the actor
// it invokes from its entry action and completes on PONG.
func xinvPingPongChart(t *testing.T) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("ping-pong", func(b *xinvBuilder) {
		b.Initial("active")
		active := b.State("active")
		child := active.ReceiveStream("child", xinvPonger)
		child.OnError().Stay()
		active.SendOnEntry("ping", child, func(counter) xinvPing { return xinvPing{} })
		active.On[xinvPong]().To("done")
		b.Final("done")
	})
	require.NoError(t, err)

	return m
}

// xinvCheckPingPong checks that the ping sent on entry is answered, so that
// the machine completes without being sent anything.
func xinvCheckPingPong(t *testing.T, m *sc.Machine[string, counter]) {
	t.Helper()

	inst := xinvInstance(t, m, counter{N: 0})

	snap := xinvDone(t, inst)
	require.Equal(t, []string{"done"}, snap.Active())
}

// XState: callback should be able to receive messages from parent
func TestXInvCallbackReceivesMessagesFromParent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		xinvCheckPingPong(t, xinvPingPongChart(t))
	})
}

// XState: should call onError upon error (sync)
func TestXInvCallsOnErrorUponError(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("error", func(b *xinvBuilder) {
			b.Initial("safe")
			b.State("safe").Stream("failing", xinvFailing[counter]).OnError().
				When("is the test error", func(_ counter, f sc.Failed) bool {
					return errors.Is(f.Err, errXinvTest) && f.Err.Error() == "test"
				}).
				To("failed")
			b.Final("failed")
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, counter{N: 0})

		snap := xinvDone(t, inst)
		require.Equal(t, []string{"failed"}, snap.Active())
	})
}

// XState: should transition correctly upon error (sync)
func TestXInvTransitionsCorrectlyUponError(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("error", func(b *xinvBuilder) {
			b.Initial("safe")
			b.State("safe").Stream("failing", xinvFailing[counter]).OnError().To("failed")
			b.State("failed").On[xinvRetry]().To("safe")
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, counter{N: 0})
		synctest.Wait()

		require.Equal(t, []string{"failed"}, inst.Snapshot().Active())
	})
}

// XState: should call onError only on the state which has invoked failed service
func TestXInvCallsOnErrorOnlyOnTheStateWhichInvokedTheFailedService(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		region := func(b *xinvBuilder, name string, stream func(context.Context, *sc.Run, counter) iter.Seq2[any, error]) {
			b.State(name, func(b *xinvBuilder) {
				b.Initial(name + ".waiting")
				b.State(name+".waiting").Stream("callback", stream).OnError().To(name + ".failed")
				b.State(name + ".failed")
			})
		}

		m, err := sc.New("m", func(b *xinvBuilder) {
			b.Initial("start")
			b.State("start").On[xinvFetch]().To("fetch")
			b.Parallel("fetch", func(b *xinvBuilder) {
				region(b, "first", xinvFailing[counter])
				region(b, "second", xinvCallbackOf[counter]())
			})
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, counter{N: 0})
		xinvSend(t, inst, xinvFetch{})
		synctest.Wait()

		require.Equal(t, []string{"fetch", "first", "first.failed", "second", "second.waiting"}, inst.Snapshot().Active())
	})
}

// XState: should be able to be stringified
func TestXInvSnapshotWithAnInvocationCanBeStringified(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("m", func(b *xinvBuilder) {
			b.Initial("idle")
			b.State("idle").On[xinvGoToWaiting]().To("waiting")
			b.State("waiting").Stream("callback", xinvCallbackOf[counter]()).OnError().Stay()
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, counter{N: 0})
		xinvSend(t, inst, xinvGoToWaiting{})

		waiting := inst.Snapshot()
		require.Len(t, waiting.Services(), 1)

		data, err := json.Marshal(waiting)
		require.NoError(t, err)

		// The stored snapshot still knows its running service.
		restored, _, err := m.Restore(data, epoch)
		require.NoError(t, err)
		require.Equal(t, []string{"waiting"}, restored.Active())
		require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation}, kinds(m.Resume(restored)))
	})
}

// XState: should result in an error notification if callback actor throws when it starts and the error stays unhandled by the machine
//
// DIVERGES from XState: XState notifies the error observer of the machine's
// actor with the error the callback threw. sc rejects a chart whose service
// has no OnError, and an instance has no error to notify anyone of: with
// OnError().Stay() the failure is consumed and the instance stays where it
// is.
func TestXInvCallbackErrorThatStaysUnhandledByTheMachine(t *testing.T) {
	t.Parallel()

	xinvCheckUnhandledError(t, xinvFailingCallback)
}

// XState: should work with input (with callbacks)
func TestXInvCallbackWorksWithInput(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		seen := xinvNewLog()

		m, err := sc.New("m", func(b *sc.Builder[string, xinvText]) {
			b.Initial("start")
			b.State("start").Stream("callback", func(ctx context.Context, _ *sc.Run, c xinvText) iter.Seq2[any, error] {
				return func(func(any, error) bool) {
					seen.add(c.Value)
					<-ctx.Done()
				}
			}).OnError().Stay()
		})
		require.NoError(t, err)

		xinvInstance(t, m, xinvText{Value: "bar"})
		synctest.Wait()

		require.Equal(t, []string{"bar"}, seen.all())
	})
}

// XState: sub invoke race condition ends on the completed state
func TestXInvSubInvokeRaceConditionEndsOnTheCompletedState(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		child, err := sc.New("child", func(b *xinvBuilder) {
			b.Initial("start")
			b.State("start").On[xinvStop]().To("end")
			b.Final("end")
		})
		require.NoError(t, err)

		parent, err := sc.New("parent", func(b *xinvBuilder) {
			b.Initial("begin")
			begin := b.State("begin")
			invoked := begin.InvokeMachine("invoked.child", child, xinvSame, xinvNoOutput[counter])
			invoked.OnDone().To("completed")
			invoked.OnError().Stay()
			begin.On[xinvStopChild]().SendTo("stop", invoked, func(counter, xinvStopChild) any { return xinvStop{} })
			b.Final("completed")
		})
		require.NoError(t, err)

		inst := xinvInstance(t, parent, counter{N: 0})
		synctest.Wait()

		xinvSend(t, inst, xinvStopChild{})

		snap := xinvDone(t, inst)
		require.Equal(t, []string{"completed"}, snap.Active())
	})
}

// xinvObservableKind says how an observable in XState's cases ends.
type xinvObservableKind int

const (
	xinvInfinite xinvObservableKind = iota
	xinvFinite
	xinvErroring
)

var errXinvSome = errors.New("some error")

// xinvObservableChart is the chart of XState's observable cases: counting
// invokes interval(10) and keeps the last value in the context, which starts
// at -1 for XState's undefined. The infinite one leaves when the count is 5,
// the finite one completes after five values, and the erroring one fails in
// place of the sixth.
//
// XState tells an observable, whose values arrive as snapshots handled by
// onSnapshot, from an event observable, whose values are events. sc has one
// stream, whose values are events, so both groups of cases use this chart.
func xinvObservableChart(t *testing.T, kind xinvObservableKind) *sc.Machine[string, counter] {
	t.Helper()

	count := func(i int) any { return xinvCount{Value: i} }

	m, err := sc.New("obs", func(b *xinvBuilder) {
		b.Initial("counting")
		counting := b.State("counting")
		counting.On[xinvCount]().Assign("keep count", func(c counter, ev xinvCount) counter { c.N = ev.Value; return c })

		switch kind {
		case xinvInfinite:
			counting.Stream("interval", xinvInterval(0, nil, count)).OnError().Stay()
			counting.Always().When("is 5", func(c counter, _ struct{}) bool { return c.N == 5 }).To("counted")
		case xinvFinite:
			inv := counting.Stream("interval", xinvInterval(5, nil, count))
			inv.OnDone().When("is 4", func(c counter, _ sc.Done[struct{}]) bool { return c.N == 4 }).To("counted")
			inv.OnError().Stay()
		case xinvErroring:
			inv := counting.Stream("interval", xinvInterval(5, errXinvSome, count))
			inv.OnDone().To("counted")
			inv.OnError().
				When("is 4 and some error", func(c counter, f sc.Failed) bool { return c.N == 4 && f.Err.Error() == "some error" }).
				To("success")
			b.Final("success")
		}

		b.Final("counted")
	}, sc.AllowUnreachable())
	require.NoError(t, err)

	return m
}

// xinvCheckObservable runs the chart to its end and checks where it ended.
func xinvCheckObservable(t *testing.T, kind xinvObservableKind, wantState string, wantCount int) {
	t.Helper()

	synctest.Test(t, func(t *testing.T) {
		inst := xinvInstance(t, xinvObservableChart(t, kind), counter{N: -1})

		snap := xinvDone(t, inst)
		require.Equal(t, []string{wantState}, snap.Active())
		require.Equal(t, wantCount, snap.Context.N)
	})
}

// xinvCheckObservableInput is XState's observable that emits its input: the
// machine, which has no states, invokes it at its root with the input 42.
// The stream reads the 42 from the context, and the chart's action on the
// event it yields resolves the test, as XState's does.
func xinvCheckObservableInput(t *testing.T) {
	t.Helper()

	synctest.Test(t, func(t *testing.T) {
		var resolved atomic.Bool

		m, err := sc.New("m", func(b *xinvBuilder) {
			chart := b.Chart()
			chart.Stream("of", func(_ context.Context, _ *sc.Run, c counter) iter.Seq2[any, error] {
				return func(yield func(any, error) bool) { yield(xinvCount{Value: c.N}, nil) }
			}).OnError().Stay()
			chart.On[xinvCount]().
				When("is 42", func(_ counter, ev xinvCount) bool { return ev.Value == 42 }).
				Do("resolve", func(context.Context, counter, xinvCount) { resolved.Store(true) })
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, counter{N: 42})
		synctest.Wait()

		require.True(t, resolved.Load())
		require.Empty(t, inst.Snapshot().Active())
	})
}

// XState: should work with an infinite observable
func TestXInvObservableWorksWithAnInfiniteObservable(t *testing.T) {
	t.Parallel()

	xinvCheckObservable(t, xinvInfinite, "counted", 5)
}

// XState: should work with a finite observable
func TestXInvObservableWorksWithAFiniteObservable(t *testing.T) {
	t.Parallel()

	xinvCheckObservable(t, xinvFinite, "counted", 4)
}

// XState: should receive an emitted error (with observables)
func TestXInvObservableReceivesAnEmittedError(t *testing.T) {
	t.Parallel()

	xinvCheckObservable(t, xinvErroring, "success", 4)
}

// XState: should work with input (with observables)
func TestXInvObservableWorksWithInput(t *testing.T) {
	t.Parallel()

	xinvCheckObservableInput(t)
}

// XState: should work with an infinite event observable
func TestXInvEventObservableWorksWithAnInfiniteEventObservable(t *testing.T) {
	t.Parallel()

	xinvCheckObservable(t, xinvInfinite, "counted", 5)
}

// XState: should work with a finite event observable
func TestXInvEventObservableWorksWithAFiniteEventObservable(t *testing.T) {
	t.Parallel()

	xinvCheckObservable(t, xinvFinite, "counted", 4)
}

// XState: should receive an emitted error (with event observables)
func TestXInvEventObservableReceivesAnEmittedError(t *testing.T) {
	t.Parallel()

	xinvCheckObservable(t, xinvErroring, "success", 4)
}

// XState: should work with input (with event observables)
func TestXInvEventObservableWorksWithInput(t *testing.T) {
	t.Parallel()

	xinvCheckObservableInput(t)
}

// xinvCheckCountingLogic ports XState's machine that invokes a counting
// actor at its root and forwards INC to it; the test reads the actor's
// count from its snapshot. An sc service has no snapshot a parent can read,
// but it can record its progress with Checkpoint, which the parent's
// snapshot keeps: the service counts the INC events in its inbox and
// checkpoints the count. As in XState, the machine has no states.
func xinvCheckCountingLogic(t *testing.T) {
	t.Helper()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("count", func(b *xinvBuilder) {
			chart := b.Chart()
			inv := chart.Receive("count", func(_ context.Context, run *sc.Run, _ counter, inbox iter.Seq[xinvInc]) (int, error) {
				n := 0
				for range inbox {
					n++
					if err := run.Checkpoint(n); err != nil {
						return n, fmt.Errorf("save count: %w", err)
					}
				}

				return n, nil
			})
			inv.OnDone().Stay()
			inv.OnError().Stay()
			chart.On[xinvInc]().SendTo("forward", inv, func(_ counter, ev xinvInc) xinvInc { return ev })
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, counter{N: 0})
		run := inst.Snapshot().Services()[0].ID

		xinvSend(t, inst, xinvInc{})
		xinvSend(t, inst, xinvInc{})

		snap := xinvAwait(t, inst, func(s sc.Snapshot[string, counter]) bool { return string(s.Checkpoint(run)) == "2" })
		require.Empty(t, snap.Active())
	})
}

// XState: should work with actor logic
//
// Partial port: XState's custom actor logic becomes a service that reads its
// inbox, and the actor's snapshot becomes the service's checkpoint.
func TestXInvWorksWithActorLogic(t *testing.T) {
	t.Parallel()

	xinvCheckCountingLogic(t)
}

// XState: logic should have reference to the parent
//
// XState's logic sends PONG to its parent through self._parent; the sc
// stream yields it.
func TestXInvLogicHasAReferenceToTheParent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("ping", func(b *xinvBuilder) {
			b.Initial("waiting")
			waiting := b.State("waiting")
			ponger := waiting.ReceiveStream("ponger", xinvPonger)
			ponger.OnError().Stay()
			waiting.SendOnEntry("ping", ponger, func(counter) xinvPing { return xinvPing{} })
			waiting.On[xinvPong]().To("done")
			b.Final("done")
		})
		require.NoError(t, err)

		xinvCheckPingPong(t, m)
	})
}

// XState: should work with a transition function
//
// Partial port: as for the actor logic, the step logic is a service reading
// its inbox, and its context is the service's checkpoint.
func TestXInvWorksWithATransitionFunction(t *testing.T) {
	t.Parallel()

	xinvCheckCountingLogic(t)
}

// XState: should emit onSnapshot (with transition functions)
//
// Partial port: sc has no onSnapshot. The doubler is a stream that yields
// its new value as an event each time it is sent one, which is the nearest
// thing to a snapshot the parent sees. As in XState, the machine has no
// states.
func TestXInvTransitionFunctionEmitsOnSnapshot(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("m", func(b *xinvBuilder) {
			chart := b.Chart()
			doubler := chart.ReceiveStream("doubler", func(_ context.Context, _ *sc.Run, _ counter, inbox iter.Seq[xinvSetValue]) iter.Seq2[any, error] {
				return func(yield func(any, error) bool) {
					for update := range inbox {
						if !yield(xinvDoubled{Value: update.Value * 2}, nil) {
							return
						}
					}
				}
			})
			doubler.OnError().Stay()
			chart.SendOnEntry("update", doubler, func(counter) xinvSetValue { return xinvSetValue{Value: 21} })
			chart.On[xinvDoubled]().Assign("keep", func(c counter, ev xinvDoubled) counter { c.N = ev.Value; return c })
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, counter{N: 0})

		snap := xinvAwait(t, inst, func(s sc.Snapshot[string, counter]) bool { return s.Context.N != 0 })
		require.Equal(t, 42, snap.Context.N)
	})
}

// XState: should create invocations from machines in nested states
func TestXInvCreatesInvocationsFromMachinesInNestedStates(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		pong, err := sc.New("pong", func(b *xinvBuilder) {
			b.Initial("active")
			b.State("active").On[xinvPing]().Emit("pong", func(counter, xinvPing) xinvPong { return xinvPong{} })
		})
		require.NoError(t, err)

		ping, err := sc.New("ping", func(b *xinvBuilder) {
			b.Initial("innerMachine")
			b.State("innerMachine", func(b *xinvBuilder) {
				b.Initial("active")
				active := b.State("active")
				pongActor := active.InvokeMachine("pong", pong, xinvSame, xinvNoOutput[counter])
				pongActor.OnError().Stay()
				active.SendOnEntry("ping", pongActor, func(counter) any { return xinvPing{} })
				active.On[xinvPong]().To("innerSuccess")
				b.Final("innerSuccess")
			}).OnDone().To("done")
			b.Final("done")
		})
		require.NoError(t, err)

		xinvCheckPingPong(t, ping)
	})
}

// XState: should emit onSnapshot (with machines)
//
// Partial port: sc has no onSnapshot, but a parent's snapshot keeps the
// snapshot of the machine it invokes, read with Snapshot.Child, and every
// step of the child is a step of the parent. The port waits for the parent's
// snapshot to hold a child that has reached "b". As in XState, the machine
// has no states.
func TestXInvMachineEmitsOnSnapshot(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		child, err := sc.New("child", func(b *xinvBuilder) {
			b.Initial("a")
			b.State("a").After(10 * time.Millisecond).To("b")
			b.State("b")
		})
		require.NoError(t, err)

		m, err := sc.New("m", func(b *xinvBuilder) {
			b.Chart().InvokeMachine("child", child, xinvSame, xinvNoOutput[counter]).OnError().Stay()
		})
		require.NoError(t, err)

		childIn := func(state string) func(sc.Snapshot[string, counter]) bool {
			return func(s sc.Snapshot[string, counter]) bool {
				snap, ok := s.Child(child, "child")

				return ok && snap.In(state)
			}
		}

		inst := xinvInstance(t, m, counter{N: 0})
		began := time.Now()

		xinvAwait(t, inst, childIn("a"))
		xinvAwait(t, inst, childIn("b"))
		require.Equal(t, 10*time.Millisecond, time.Since(began))
	})
}

// XState: should start all services at once
//
// DIVERGES from XState: XState starts the two callbacks in the order they
// are declared and handles what each sends back as it starts, so ONE is
// always handled before the TWO that completes the machine, and the context
// ends with both. sc runs the services at the same time, so TWO can
// complete the machine before ONE is handled; ONE is then dropped. The
// context always has "two", and has "one" only when ONE came first.
func TestXInvStartsAllServicesAtOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("machine", func(b *sc.Builder[string, xinvNames]) {
			b.Initial("one")
			b.On[xinvOne]().Assign("one", func(c xinvNames, _ xinvOne) xinvNames { c.One = "one"; return c })
			b.On[xinvTwo]().Assign("two", func(c xinvNames, _ xinvTwo) xinvNames { c.Two = "two"; return c }).To("three")
			b.State("one", func(b *sc.Builder[string, xinvNames]) {
				b.Initial("two")
				two := b.State("two")
				two.Stream("child", xinvCallbackOf[xinvNames](xinvOne{})).OnError().Stay()
				two.Stream("child2", xinvCallbackOf[xinvNames](xinvTwo{})).OnError().Stay()
			})
			b.Final("three")
		})
		require.NoError(t, err)

		// Both services start in the step that enters the state.
		_, effects := m.Start(xinvNames{One: "", Two: ""}, epoch)
		require.Equal(t, []string{"two/child", "two/child2"}, names(effects))
		require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation, sc.EffectStartInvocation}, kinds(effects))

		inst := xinvInstance(t, m, xinvNames{One: "", Two: ""})

		snap := xinvDone(t, inst)
		require.Equal(t, "two", snap.Context.Two)
		require.Contains(t, []string{"", "one"}, snap.Context.One)
	})
}

// XState: should run services in parallel
func TestXInvRunsServicesInParallel(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("machine", func(b *sc.Builder[string, xinvNames]) {
			b.Initial("one")
			b.On[xinvOne]().Assign("one", func(c xinvNames, _ xinvOne) xinvNames { c.One = "one"; return c })
			b.On[xinvTwo]().Assign("two", func(c xinvNames, _ xinvTwo) xinvNames { c.Two = "two"; return c })
			// Both invoked services get a chance to send their events.
			b.Chart().After(10 * time.Millisecond).To("three")
			b.State("one", func(b *sc.Builder[string, xinvNames]) {
				b.Initial("two")
				b.Parallel("two", func(b *sc.Builder[string, xinvNames]) {
					b.State("a").Stream("child", xinvCallbackOf[xinvNames](xinvOne{})).OnError().Stay()
					b.State("b").Stream("child2", xinvCallbackOf[xinvNames](xinvTwo{})).OnError().Stay()
				})
			})
			b.Final("three")
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, xinvNames{One: "", Two: ""})

		snap := xinvDone(t, inst)
		require.Equal(t, xinvNames{One: "one", Two: "two"}, snap.Context)
	})
}

// XState: should not invoke an actor if it gets stopped immediately by transitioning away in immediate microstep
func TestXInvDoesNotInvokeAnActorStoppedByTransitioningAwayInImmediateMicrostep(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var started atomic.Bool

		m, err := sc.New("transient", func(b *xinvBuilder) {
			b.Initial("active")
			active := b.State("active")
			active.Stream("doNotInvoke", xinvDisposable[counter](func() { started.Store(true) }, func() {})).OnError().Stay()
			active.Always().To("inactive")
			b.State("inactive")
		})
		require.NoError(t, err)

		// The core neither starts nor stops the service.
		snap, effects := m.Start(counter{N: 0}, epoch)
		require.Equal(t, []string{"inactive"}, snap.Active())
		require.Empty(t, effects)

		inst := xinvInstance(t, m, counter{N: 0})
		synctest.Wait()

		require.Equal(t, []string{"inactive"}, inst.Snapshot().Active())
		require.False(t, started.Load())
	})
}

// XState: should not invoke an actor if it gets stopped immediately by transitioning away in subsequent microstep
func TestXInvDoesNotInvokeAnActorStoppedByTransitioningAwayInSubsequentMicrostep(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var started atomic.Bool

		m, err := sc.New("transient", func(b *xinvBuilder) {
			b.Initial("withNonLeafInvoke")
			b.State("withNonLeafInvoke", func(b *xinvBuilder) {
				b.Initial("first")
				b.State("first").Always().To("second")
				b.State("second").Always().To("inactive")
			}).Stream("doNotInvoke", xinvDisposable[counter](func() { started.Store(true) }, func() {})).OnError().Stay()
			b.State("inactive")
		})
		require.NoError(t, err)

		snap, effects := m.Start(counter{N: 0}, epoch)
		require.Equal(t, []string{"inactive"}, snap.Active())
		require.Empty(t, effects)

		inst := xinvInstance(t, m, counter{N: 0})
		synctest.Wait()

		require.Equal(t, []string{"inactive"}, inst.Snapshot().Active())
		require.False(t, started.Load())
	})
}

// XState: should invoke a service if other service gets stopped in subsequent microstep (#1180)
func TestXInvInvokesAServiceIfOtherServiceGetsStoppedInSubsequentMicrostep(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("m", func(b *xinvBuilder) {
			b.Initial("running")
			b.Parallel("running", func(b *xinvBuilder) {
				b.State("one", func(b *xinvBuilder) {
					b.Initial("one.active")
					b.State("one.idle")
					active := b.State("one.active")
					active.Stream("active", xinvCallbackOf[counter]()).OnError().Stay()
					active.On[xinvNext]().Raise("stop one", func(counter, xinvNext) any { return xinvStopOne{} })
				}).On[xinvStopOne]().To("one.idle")

				b.State("two", func(b *xinvBuilder) {
					b.Initial("two.idle")
					b.State("two.idle")
					post := b.State("two.active").Invoke("post", xinvPromiseOf[counter](42))
					post.OnDone().To("done")
					post.OnError().Stay()
				}).On[xinvNext]().To("two.active")
			})
			b.Final("done")
		})
		require.NoError(t, err)

		// One step stops the first service and starts the second.
		snap, _ := m.Start(counter{N: 0}, epoch)
		_, effects := m.Transition(snap, xinvNext{}, epoch)
		require.Equal(t, 1, xinvCountKind(effects, sc.EffectStopInvocation))
		require.Equal(t, "one.active/active", effect(t, effects, sc.EffectStopInvocation).State+"/"+effect(t, effects, sc.EffectStopInvocation).Name)
		require.Equal(t, "two.active/post", effect(t, effects, sc.EffectStartInvocation).State+"/"+effect(t, effects, sc.EffectStartInvocation).Name)

		inst := xinvInstance(t, m, counter{N: 0})
		xinvSend(t, inst, xinvNext{})

		done := xinvDone(t, inst)
		require.Equal(t, []string{"done"}, done.Active())
	})
}

// XState: should invoke an actor when reentering invoking state within a single macrostep
//
// XState skips this case (it.skip); its expectation is that the actor is
// started once. sc agrees: the visit that is left at once starts nothing,
// and the one the macrostep ends in starts the service.
func TestXInvInvokesAnActorWhenReenteringInvokingStateWithinASingleMacrostep(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var started atomic.Int32

		m, err := sc.New("transient", func(b *xinvBuilder) {
			b.Initial("active")
			active := b.State("active")
			active.Stream("callback", xinvDisposable[counter](func() { started.Add(1) }, func() {})).OnError().Stay()
			active.Always().When("first visit", func(c counter, _ struct{}) bool { return c.N == 0 }).To("inactive")
			inactive := b.State("inactive")
			inactive.AssignOnEntry("count", func(c counter) counter { c.N++; return c })
			inactive.Always().To("active")
		})
		require.NoError(t, err)

		snap, effects := m.Start(counter{N: 0}, epoch)
		require.Equal(t, []string{"active"}, snap.Active())
		require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation}, kinds(effects))

		xinvInstance(t, m, counter{N: 0})
		synctest.Wait()

		require.Equal(t, int32(1), started.Load())
	})
}

// xinvCheckEndpoint ports XState's machine whose promise checks the endpoint
// its input names. An sc service has no input of its own: it reads the
// context it is given.
func xinvCheckEndpoint(t *testing.T) {
	t.Helper()

	synctest.Test(t, func(t *testing.T) {
		seen := xinvNewLog()

		m, err := sc.New("m", func(b *sc.Builder[string, xinvText]) {
			b.Initial("searching")
			inv := b.State("searching").Invoke("search", func(_ context.Context, _ *sc.Run, c xinvText) (int, error) {
				seen.add(c.Value)
				return 42, nil
			})
			inv.OnDone().To("success")
			inv.OnError().Stay()
			b.Final("success")
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, xinvText{Value: "example.com"})

		snap := xinvDone(t, inst)
		require.Equal(t, []string{"success"}, snap.Active())
		require.Equal(t, []string{"example.com"}, seen.all())
	})
}

// XState: invoke `src` can be used with invoke `input`
//
// Partial port: XState's input is a static value declared on the invoke. sc
// has no input on a service, so the value comes from the context, as in the
// dynamic case below.
func TestXInvSrcCanBeUsedWithInvokeInput(t *testing.T) {
	t.Parallel()

	xinvCheckEndpoint(t)
}

// XState: invoke `src` can be used with dynamic invoke `input`
func TestXInvSrcCanBeUsedWithDynamicInvokeInput(t *testing.T) {
	t.Parallel()

	xinvCheckEndpoint(t)
}

// XState: dynamic invoke `input` should receive the context updated by the same transition
func TestXInvDynamicInputReceivesTheContextUpdatedByTheSameTransition(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var seen atomic.Int32

		m, err := sc.New("m", func(b *xinvBuilder) {
			b.Initial("idle")
			b.State("idle").On[xinvStart]().
				Assign("set value", func(c counter, _ xinvStart) counter { c.N = 100; return c }).
				To("active")
			inv := b.State("active").Invoke("promise", func(_ context.Context, _ *sc.Run, c counter) (int, error) {
				seen.Store(int32(c.N)) //nolint:gosec // the value is 100.
				return c.N, nil
			})
			inv.OnDone().To("success")
			inv.OnError().Stay()
			b.Final("success")
		})
		require.NoError(t, err)

		// The effect that starts the service carries the updated context.
		snap, _ := m.Start(counter{N: 0}, epoch)
		_, effects := m.Transition(snap, xinvStart{}, epoch)
		require.Equal(t, counter{N: 100}, effect(t, effects, sc.EffectStartInvocation).Context)

		inst := xinvInstance(t, m, counter{N: 0})
		xinvSend(t, inst, xinvStart{})

		done := xinvDone(t, inst)
		require.Equal(t, []string{"success"}, done.Active())
		require.Equal(t, int32(100), seen.Load())
	})
}

// XState: invoke generated ID should be predictable based on the state node where it is defined
//
// DIVERGES from XState: XState generates the ID "a:0" for an invocation
// that names none: the invoking state and the invocation's index in it. An
// sc service always has a name, and the ID of a run is the state, the name
// and the number of the run in the instance: "a.promise#1". It is as
// predictable, but it names a run and not an invocation, so a second run of
// the same service has another ID.
func TestXInvGeneratedIDIsPredictableFromTheStateNodeWhereItIsDefined(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m, err := sc.New("m", func(b *sc.Builder[string, xinvText]) {
			b.Initial("a")
			inv := b.State("a").Invoke("promise", xinvPromiseOf[xinvText](struct{}{}))
			inv.OnDone().
				Assign("keep id", func(c xinvText, d sc.Done[struct{}]) xinvText { c.Value = d.ID; return c }).
				To("b")
			inv.OnError().Stay()
			b.Final("b")
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, xinvText{Value: ""})

		snap := xinvDone(t, inst)
		require.Equal(t, "a.promise#1", snap.Context.Value)
	})
}

// XState: invoke config defined as %s should register unique and predictable child in state
//
// DIVERGES from XState: XState registers the child under "machine.a:0", the
// machine's ID, the state and the invocation's index. sc lists the run in
// the snapshot's services under "a.child#1": the state, the service's name
// and the number of the run.
func TestXInvInvokeConfigRegistersUniqueAndPredictableChildInState(t *testing.T) {
	t.Parallel()

	empty, err := sc.New("someId", func(*xinvBuilder) {})
	require.NoError(t, err)

	cases := map[string]func(*sc.StateBuilder[string, counter]){
		"src containing a machine directly": func(s *sc.StateBuilder[string, counter]) {
			s.InvokeMachine("child", empty, xinvSame, xinvNoOutput[counter]).OnError().Stay()
		},
		"src containing a callback actor directly": func(s *sc.StateBuilder[string, counter]) {
			s.Stream("child", xinvCallbackOf[counter]()).OnError().Stay()
		},
	}

	for name, invoke := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			m, err := sc.New("machine", func(b *xinvBuilder) {
				b.Initial("a")
				invoke(b.State("a"))
			})
			require.NoError(t, err)

			// XState reads the children of an actor that has not started;
			// the pure core's first snapshot is the nearest thing.
			snap, _ := m.Start(counter{N: 0}, epoch)
			require.Equal(t, []sc.RunningService[string]{{ID: "a.child#1", State: "a", Name: "child"}}, snap.Services())
		})
	}
}

// XState: xstate.done.actor events should only select onDone transition on the invoking state when invokee is referenced using a string
func TestXInvDoneEventsOnlySelectOnDoneOnTheInvokingState(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var successes atomic.Int32
		var invoked atomic.Bool

		region := func(b *xinvBuilder, name string) {
			b.State(name, func(b *xinvBuilder) {
				b.Initial(name + ".fetch")
				inv := b.State(name+".fetch").Invoke("fetch", func(ctx context.Context, _ *sc.Run, _ counter) (int, error) {
					// Only the first run resolves.
					if invoked.Swap(true) {
						<-ctx.Done()
						return 0, context.Cause(ctx)
					}

					return 42, nil
				})
				inv.OnDone().Do("handleSuccess", func(context.Context, counter, sc.Done[int]) { successes.Add(1) })
				inv.OnError().Stay()
			})
		}

		m, err := sc.New("m", func(b *xinvBuilder) {
			b.Regions()
			region(b, "first")
			region(b, "second")
		})
		require.NoError(t, err)

		// In the pure core, the first region's result runs its action alone.
		snap, effects := m.Start(counter{N: 0}, epoch)
		require.Equal(t, []string{"first.fetch/fetch", "second.fetch/fetch"}, names(effects))
		_, effects = m.Transition(snap, sc.Done[int]{ID: effects[0].ID, Result: 42}, epoch)
		require.Equal(t, []string{"first.fetch/handleSuccess"}, names(effects))

		xinvInstance(t, m, counter{N: 0})
		synctest.Wait()

		require.Equal(t, int32(1), successes.Load())
	})
}

// XState: xstate.done.actor events should identify each invokee
//
// DIVERGES from XState: XState collects the two done events with a wildcard
// transition on the root, and they arrive first region first. An sc OnAny
// never takes a service's result, so the wildcard collects nothing, and the
// port reads each ID in the OnDone of its invocation. The IDs are
// "first.fetch.child#1" and "second.fetch.child#2" where XState has
// "first.fetch:0" and "second.fetch:0", and the two children run at the same
// time, so the results arrive in either order.
func TestXInvDoneEventsIdentifyEachInvokee(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		done := xinvNewLog()
		wildcard := xinvNewLog()

		child, err := sc.New("child", func(b *xinvBuilder) {
			b.Initial("a")
			inv := b.State("a").Invoke("promise", xinvPromiseOf[counter](42))
			inv.OnDone().To("b")
			inv.OnError().Stay()
			b.Final("b")
		})
		require.NoError(t, err)

		region := func(b *xinvBuilder, name string) {
			b.State(name, func(b *xinvBuilder) {
				b.Initial(name + ".fetch")
				inv := b.State(name+".fetch").InvokeMachine("child", child, xinvSame, xinvNoOutput[counter])
				inv.OnDone().Do("record", func(_ context.Context, _ counter, d sc.Done[struct{}]) { done.add(d.ID) })
				inv.OnError().Stay()
			})
		}

		m, err := sc.New("m", func(b *xinvBuilder) {
			b.Regions()
			region(b, "first")
			region(b, "second")
			b.OnAny().Do("record any", func(_ context.Context, _ counter, ev any) { wildcard.add(fmt.Sprintf("%T", ev)) })
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, counter{N: 0})
		xinvAwait(t, inst, func(s sc.Snapshot[string, counter]) bool { return len(s.Services()) == 0 })
		synctest.Wait()

		require.ElementsMatch(t, []string{"first.fetch.child#1", "second.fetch.child#2"}, done.all())
		require.Empty(t, wildcard.all())
	})
}

// XState: should get reinstantiated after reentering the invoking state in a microstep
func TestXInvGetsReinstantiatedAfterReenteringTheInvokingStateInAMicrostep(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var invoked atomic.Int32

		m, err := sc.New("m", func(b *xinvBuilder) {
			b.Initial("a")
			a := b.State("a")
			a.Stream("callback", xinvDisposable[counter](func() { invoked.Add(1) }, func() {})).OnError().Stay()
			a.On[xinvGoAway]().To("b")
			b.State("b").Always().To("a")
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, counter{N: 0})
		synctest.Wait()
		require.Equal(t, int32(1), invoked.Load())

		step := xinvSend(t, inst, xinvGoAway{})
		require.Equal(t, []sc.EffectKind{sc.EffectStopInvocation, sc.EffectStartInvocation}, kinds(step.Effects))
		require.NotEqual(t, step.Effects[0].ID, step.Effects[1].ID)
		synctest.Wait()

		require.Equal(t, int32(2), invoked.Load())
	})
}

// XState: invocations should be stopped when the machine reaches done state
func TestXInvInvocationsAreStoppedWhenTheMachineReachesDoneState(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var disposed atomic.Bool

		m := xinvRootInvokeChart[xinvFinish](t, xinvDisposable[counter](func() {}, func() { disposed.Store(true) }))

		inst := xinvInstance(t, m, counter{N: 0})
		synctest.Wait()

		step := xinvSend(t, inst, xinvFinish{})

		require.True(t, step.To.Done())
		require.True(t, disposed.Load())
	})
}

// XState: deep invocations should be stopped when the machine reaches done state
//
// As in XState, the child has no states.
func TestXInvDeepInvocationsAreStoppedWhenTheMachineReachesDoneState(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var started, disposed atomic.Bool

		child, err := sc.New("child", func(b *xinvBuilder) {
			b.Chart().Stream("callback", xinvDisposable[counter](
				func() { started.Store(true) },
				func() { disposed.Store(true) },
			)).OnError().Stay()
		})
		require.NoError(t, err)

		m, err := sc.New("m", func(b *xinvBuilder) {
			b.Chart().InvokeMachine("child", child, xinvSame, xinvNoOutput[counter]).OnError().Stay()

			b.Initial("a")
			b.State("a").On[xinvFinish]().To("b")
			b.Final("b")
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, counter{N: 0})
		synctest.Wait()
		require.True(t, started.Load())
		require.False(t, disposed.Load())

		step := xinvSend(t, inst, xinvFinish{})

		require.True(t, step.To.Done())
		require.True(t, disposed.Load())
	})
}

// XState: root invocations should restart on root reentering transitions
func TestXInvRootInvocationsRestartOnRootReenteringTransitions(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var count atomic.Int32

		m, err := sc.New("root", func(b *xinvBuilder) {
			chart := b.Chart()
			inv := chart.Invoke("promise", func(context.Context, *sc.Run, counter) (int, error) {
				count.Add(1)
				return 42, nil
			})
			inv.OnDone().Stay()
			inv.OnError().Stay()
			chart.On[xinvEvent]().To("two").Reenter()

			b.Initial("one")
			b.State("one")
			b.State("two")
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, counter{N: 0})
		synctest.Wait()
		require.Equal(t, int32(1), count.Load())

		step := xinvSend(t, inst, xinvEvent{})
		require.Equal(t, []string{"two"}, step.To.Active())
		synctest.Wait()

		require.Equal(t, int32(2), count.Load())
	})
}

// XState: should be able to restart an invoke when reentering the invoking state
func TestXInvRestartsAnInvokeWhenReenteringTheInvokingState(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		actual := xinvNewLog()
		var invoked atomic.Int32

		m, err := sc.New("m", func(b *xinvBuilder) {
			b.Initial("inactive")
			b.State("inactive").On[xinvActivate]().To("active")
			active := b.State("active")
			active.Stream("callback", func(ctx context.Context, _ *sc.Run, _ counter) iter.Seq2[any, error] {
				return func(func(any, error) bool) {
					id := invoked.Add(1)
					actual.add(fmt.Sprintf("start %d", id))
					defer func() { actual.add(fmt.Sprintf("stop %d", id)) }()

					<-ctx.Done()
				}
			}).OnError().Stay()
			active.On[xinvReenter]().To("active").Reenter()
		})
		require.NoError(t, err)

		inst := xinvInstance(t, m, counter{N: 0})
		xinvSend(t, inst, xinvActivate{})
		synctest.Wait()
		require.Equal(t, []string{"start 1"}, actual.all())

		xinvSend(t, inst, xinvReenter{})
		synctest.Wait()

		require.Equal(t, []string{"start 1", "stop 1", "start 2"}, actual.all())
	})
}

// XState: should provide input to an actor creator
//
// Partial port: XState's input mapper builds the promise's input from the
// context. A plain sc service has no input mapper and derives what it needs
// from the context it is given; only InvokeMachine maps an input, which the
// second half checks with a child that completes with what it was given.
func TestXInvProvidesInputToAnActorCreator(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var seen atomic.Int32

		m := xinvPendingChart(t,
			func(_ context.Context, _ *sc.Run, c counter) (bool, error) {
				seen.Store(int32(c.N * 2)) //nolint:gosec // the count is 42.
				return true, nil
			},
			xinvPlain[counter, bool],
		)

		inst := xinvInstance(t, m, counter{N: 42})

		snap := xinvDone(t, inst)
		require.Equal(t, []string{"success"}, snap.Active())
		require.Equal(t, int32(84), seen.Load())

		child, err := sc.New("child", func(b *sc.Builder[string, xinvText]) {
			b.Initial("end")
			b.Final("end")
		})
		require.NoError(t, err)

		mapped, err := sc.New("mapped", func(b *sc.Builder[string, xinvText]) {
			b.Initial("pending")
			inv := b.State("pending").InvokeMachine("child", child,
				func(c xinvText) xinvText { return xinvText{Value: c.Value + " hello"} },
				func(snap sc.Snapshot[string, xinvText]) (string, error) { return snap.Context.Value, nil },
			)
			inv.OnDone().Assign("keep", func(c xinvText, d sc.Done[string]) xinvText { c.Value = d.Result; return c }).To("success")
			inv.OnError().Stay()
			b.Final("success")
		})
		require.NoError(t, err)

		mappedInst := xinvInstance(t, mapped, xinvText{Value: "84"})
		require.Equal(t, "84 hello", xinvDone(t, mappedInst).Context.Value)
	})
}

// XState cases that are not ported, by reason.
//
// The case hands an actor a reference to another actor (self, or the parent
// as input) and calls it. An sc service has no reference to the instance
// that runs it; it yields events and reads its inbox:
//   - starts invoked actors after committing the parent snapshot
//     (an sc instance publishes a step's snapshot once the step's effects,
//     the start of its services among them, have been carried out, so a
//     service reading the instance as it starts may see either snapshot)
//   - should schedule events in a FIFO queue
//     (the invoked actor sends to itself)
//   - should provide self to input mapper
//
// The case is about the output argument of XState's transition functions:
//   - should not provide output directly for arbitrary output events
//
// XState skips the case itself (it.skip), and it needs an XState-only API:
//   - should use the service overwritten by .provide(...)
//     (sc binds services by name with Machine.Bind, but not invoked machines)
//   - should be able to receive a delayed event sent by the entry action of the invoking state
//     (a delayed send to a child, carrying a reference to the sender; an sc
//     state sends on entry at once, and only a delayed transition sends
//     later)
