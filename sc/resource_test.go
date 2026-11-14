package sc_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

type (
	dial   struct{}
	hangUp struct{}
)

// line is the live value the link chart holds: something that cannot be
// stored, like a connection.
type line struct {
	name string
	done chan struct{}
}

// journal records what the link chart's functions did, in order.
type journal struct {
	mu      sync.Mutex
	entries []string
}

func (j *journal) add(format string, args ...any) {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.entries = append(j.entries, fmt.Sprintf(format, args...))
}

func (j *journal) list() []string {
	j.mu.Lock()
	defer j.mu.Unlock()

	return append([]string(nil), j.entries...)
}

// link is how the tests shape the link chart's behaviour.
type link struct {
	log *journal

	// acquire opens the line; nil opens one at once.
	acquire func(ctx context.Context) (*line, error)
}

// linkChart holds a line while it is open. Dialling acquires it, the ready
// state runs a service that uses it until stopped, and hanging up leaves the
// open state.
func linkChart(t *testing.T, l link) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("link", func(b *sc.Builder[string, counter]) {
		b.Initial("idle")
		b.State("idle").On[dial]().To("open")

		var held *sc.Resource[string, counter, *line]

		open := b.State("open", func(b *sc.Builder[string, counter]) {
			b.Initial("dialling")
			b.State("dialling")

			ready := b.State("ready")
			use := ready.Invoke("use", func(ctx context.Context, run *sc.Run, _ counter) (int, error) {
				ln, ok := held.Held(run)
				if !ok {
					l.log.add("use: no line")
					return 0, nil
				}

				l.log.add("use %s", ln.name)
				<-ctx.Done()
				l.log.add("use stopped")

				return 0, ctx.Err()
			})
			use.OnDone().Stay()
			use.OnError().Stay()
		})

		held = open.Holds("line",
			func(ctx context.Context, _ *sc.Run, _ counter) (*line, error) {
				if l.acquire != nil {
					return l.acquire(ctx)
				}

				l.log.add("acquire")

				return &line{name: "line-1", done: make(chan struct{})}, nil
			},
			func(_ context.Context, ln *line, why sc.Leave[string]) {
				close(ln.done)
				l.log.add("release %s closed=%t to=%v", ln.name, why.Closed, why.Move.To)
			},
		)
		held.OnAcquired().
			Do("greet", func(ctx context.Context, _ counter, a sc.Acquired[*line]) {
				ln, ok := held.From(ctx)
				l.log.add("greet %s held=%t", a.Value.name, ok && ln == a.Value)
			}).
			To("ready")
		held.OnError().To("broken")

		open.On[hangUp]().To("idle")
		b.State("broken").On[dial]().To("open")
	})
	require.NoError(t, err)

	return m
}

func inReady(s sc.Snapshot[string, counter]) bool { return s.In("ready") }

func TestAResourceIsHeldWhileItsStateIsActiveAndReleasedAfterItsServicesStop(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		log := &journal{}
		m := linkChart(t, link{log: log})

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()

		_, err = inst.Send(t.Context(), dial{})
		require.NoError(t, err)
		ready, err := inst.Await(t.Context(), inReady)
		require.NoError(t, err)
		synctest.Wait()

		require.Equal(t, []sc.HeldResource[string]{{ID: ready.Resources()[0].ID, State: "open", Name: "line", Acquired: true}}, ready.Resources())
		require.Len(t, ready.Services(), 1)

		_, err = inst.Send(t.Context(), hangUp{})
		require.NoError(t, err)

		require.Equal(t, []string{
			"acquire",
			"greet line-1 held=true",
			"use line-1",
			"use stopped",
			"release line-1 closed=false to=[idle]",
		}, log.list())
		require.Empty(t, inst.Snapshot().Resources())
	})
}

func TestAFailedAcquisitionTakesTheErrorRoute(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		log := &journal{}
		m := linkChart(t, link{log: log, acquire: func(context.Context) (*line, error) { return nil, errWorkFailed }})

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()

		_, err = inst.SendAndSettle(t.Context(), dial{})
		require.NoError(t, err)

		require.Equal(t, []string{"broken"}, inst.Snapshot().Active())
		require.Empty(t, inst.Snapshot().Resources())
		require.Empty(t, log.list())
	})
}

func TestLeavingDuringAcquisitionReleasesTheValueWhenItArrives(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		log := &journal{}
		proceed := make(chan struct{})
		m := linkChart(t, link{log: log, acquire: func(context.Context) (*line, error) {
			<-proceed
			log.add("acquired late")

			return &line{name: "late", done: make(chan struct{})}, nil
		}})

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()

		_, err = inst.Send(t.Context(), dial{})
		require.NoError(t, err)
		_, err = inst.Send(t.Context(), hangUp{})
		require.NoError(t, err)
		require.Empty(t, log.list())

		close(proceed)
		synctest.Wait()

		// The acquisition was left to finish, its value was released at
		// once, and the chart never heard of it.
		require.Equal(t, []string{"acquired late", "release late closed=false to=[idle]"}, log.list())
		require.Equal(t, []string{"idle"}, inst.Snapshot().Active())
	})
}

func TestClosingReleasesWhatIsHeldAndResumingAcquiresItAgainBeforeServicesRun(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		log := &journal{}
		opened := 0
		m := linkChart(t, link{log: log, acquire: func(context.Context) (*line, error) {
			// Slow, so a resumed service would run first if it did not
			// wait for the line.
			time.Sleep(time.Second) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
			opened++
			log.add("acquire %d", opened)

			return &line{name: fmt.Sprintf("line-%d", opened), done: make(chan struct{})}, nil
		}})

		first, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		_, err = first.Send(t.Context(), dial{})
		require.NoError(t, err)
		_, err = first.Await(t.Context(), inReady)
		require.NoError(t, err)
		synctest.Wait()

		snap := first.Snapshot()
		first.Close()

		// The snapshot stores no value, only that the line was held.
		data, err := json.Marshal(snap)
		require.NoError(t, err)
		require.NotContains(t, string(data), "line-1")
		restored, _, err := m.Restore(data, time.Now())
		require.NoError(t, err)
		require.True(t, restored.Resources()[0].Acquired)

		second, err := m.InstanceFrom(t.Context(), restored)
		require.NoError(t, err)
		defer second.Close()

		time.Sleep(2 * time.Second) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
		synctest.Wait()

		require.Equal(t, []string{
			"acquire 1",
			"greet line-1 held=true",
			"use line-1",
			"use stopped",
			"release line-1 closed=true to=[]",
			"acquire 2",
			"use line-2",
		}, log.list())
		require.Equal(t, []string{"open", "ready"}, second.Snapshot().Active())
	})
}

func TestFailingToAcquireAgainOnResumeTakesTheErrorRoute(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		log := &journal{}
		fail := false
		m := linkChart(t, link{log: log, acquire: func(context.Context) (*line, error) {
			if fail {
				return nil, errWorkFailed
			}

			return &line{name: "line-1", done: make(chan struct{})}, nil
		}})

		first, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		_, err = first.Send(t.Context(), dial{})
		require.NoError(t, err)
		_, err = first.Await(t.Context(), inReady)
		require.NoError(t, err)
		synctest.Wait()
		snap := first.Snapshot()
		first.Close()

		fail = true
		second, err := m.InstanceFrom(t.Context(), snap)
		require.NoError(t, err)
		defer second.Close()

		broken, err := second.Await(t.Context(), func(s sc.Snapshot[string, counter]) bool { return s.In("broken") })
		require.NoError(t, err)
		require.Empty(t, broken.Resources())
	})
}

func TestResourcesAreReleasedInnerFirstAndInReverseOrder(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var released []string
	hold := func(sb *sc.StateBuilder[string, counter], name string) {
		res := sb.Holds(name,
			func(context.Context, *sc.Run, counter) (string, error) { return name, nil },
			func(_ context.Context, value string, _ sc.Leave[string]) {
				mu.Lock()
				defer mu.Unlock()
				released = append(released, value)
			},
		)
		res.OnError().Stay()
	}

	m, err := sc.New("nested", func(b *sc.Builder[string, counter]) {
		b.Initial("outer")
		outer := b.State("outer", func(b *sc.Builder[string, counter]) {
			b.Initial("inner")
			hold(b.State("inner"), "c")
		})
		hold(outer, "a")
		hold(outer, "b")
		outer.On[leave]().To("gone")
		b.State("gone")
	})
	require.NoError(t, err)

	synctest.Test(t, func(t *testing.T) {
		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()

		_, err = inst.Await(t.Context(), func(s sc.Snapshot[string, counter]) bool {
			held := 0
			for _, r := range s.Resources() {
				if r.Acquired {
					held++
				}
			}

			return held == 3
		})
		require.NoError(t, err)

		_, err = inst.Send(t.Context(), leave{})
		require.NoError(t, err)

		require.Equal(t, []string{"c", "b", "a"}, released)
	})
}

func TestExploreTreatsAcquisitionAsAnOutcome(t *testing.T) {
	t.Parallel()

	m := linkChart(t, link{log: &journal{}})

	report, err := m.Explore(counter{}, sc.Exploration[string, counter]{
		Events: []any{dial{}, hangUp{}},
		Outcomes: map[string][]sc.Outcome{
			"line": {sc.Acquires(&line{name: "explored", done: nil}), sc.Fails(errWorkFailed)},
		},
		Invariants: []sc.Invariant[string, counter]{{
			Name: "ready only while the line is held",
			Holds: func(s sc.Snapshot[string, counter]) bool {
				held := len(s.Resources()) == 1 && s.Resources()[0].Acquired
				return !s.In("ready") || held
			},
		}},
		Steps: []sc.StepInvariant[string, counter]{{
			Name: "every line that is left is released",
			Holds: func(st sc.Step[string, counter]) bool {
				released := 0
				for _, e := range st.Effects {
					if e.Kind == sc.EffectRelease {
						released++
					}
				}

				// A failed acquisition holds nothing to release.
				_, failed := st.Event.(sc.Failed)
				left := len(st.From.Resources()) == 1 && len(st.To.Resources()) == 0 && !failed

				return (released == 1) == left
			},
		}},
	})
	require.NoError(t, err)

	require.Empty(t, report.Violations)
	require.True(t, report.Reached("ready"))
	require.True(t, report.Reached("broken"))
}

func TestAResourceCanBeDeclaredAndBoundLater(t *testing.T) {
	t.Parallel()

	var held *sc.Resource[string, counter, int]
	m, err := sc.New("late", func(b *sc.Builder[string, counter]) {
		b.Initial("open")
		open := b.State("open")
		held = open.HoldsRequired[int]("number")
		held.OnAcquired().To("ready")
		held.OnError().To("ready")
		b.State("ready")
	})
	require.NoError(t, err)
	require.Equal(t, []string{"resource number"}, m.Unbound())
	require.Equal(t, []string{"open.number"}, m.Resources())

	_, err = m.Bind(func(b *sc.Binder[string, counter]) {
		b.Resource[string]("number", func(context.Context, *sc.Run, counter) (string, error) { return "", nil }, func(context.Context, string, sc.Leave[string]) {})
	})
	require.ErrorContains(t, err, `resource "number" in state "open" reports sc.Acquired[int], not sc.Acquired[string]`)

	bound, err := m.Bind(func(b *sc.Binder[string, counter]) {
		b.Resource[int]("number", func(context.Context, *sc.Run, counter) (int, error) { return 7, nil }, func(context.Context, int, sc.Leave[string]) {})
	})
	require.NoError(t, err)
	require.Empty(t, bound.Unbound())

	// Outside a driver nothing is held.
	_, ok := held.From(t.Context())
	require.False(t, ok)
}

func TestActorHoldsAndReleasesResources(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		log := &journal{}
		m := linkChart(t, link{log: log})

		ctx, stop := context.WithCancel(t.Context())
		a := m.Actor(counter{})
		wait := runActor(ctx, a)

		require.NoError(t, a.Send(t.Context(), dial{}))
		_, err := a.Await(t.Context(), inReady)
		require.NoError(t, err)
		synctest.Wait()

		_, err = a.Call(t.Context(), hangUp{})
		require.NoError(t, err)
		require.Equal(t, []string{
			"acquire",
			"greet line-1 held=true",
			"use line-1",
			"use stopped",
			"release line-1 closed=false to=[idle]",
		}, log.list())

		// Stopping the actor while a line is held releases it as closed.
		require.NoError(t, a.Send(t.Context(), dial{}))
		_, err = a.Await(t.Context(), inReady)
		require.NoError(t, err)
		synctest.Wait()

		stop()
		require.ErrorIs(t, wait(), context.Canceled)
		require.Equal(t, "release line-1 closed=true to=[]", log.list()[len(log.list())-1])
	})
}
