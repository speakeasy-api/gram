package sc_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

func TestNewReportsDeclarationErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		def  func(b *sc.Builder[string, counter])
		want string
	}{
		{
			name: "no states",
			def:  func(*sc.Builder[string, counter]) {},
			want: "machine declares no states",
		},
		{
			name: "missing initial",
			def: func(b *sc.Builder[string, counter]) {
				b.State("a")
			},
			want: `state "bad": has child states but no initial state`,
		},
		{
			name: "initial is not a child",
			def: func(b *sc.Builder[string, counter]) {
				b.Initial("nested")
				b.State("a", func(b *sc.Builder[string, counter]) {
					b.Initial("nested")
					b.State("nested")
				})
			},
			want: `initial state "nested" is not one of its child states`,
		},
		{
			name: "undeclared target",
			def: func(b *sc.Builder[string, counter]) {
				b.Initial("a")
				b.State("a").On[tick]().To("missing")
			},
			want: `state "a": transition targets undeclared state "missing"`,
		},
		{
			name: "unreachable state",
			def: func(b *sc.Builder[string, counter]) {
				b.Initial("a")
				b.State("a")
				b.State("island")
			},
			want: `state "island": unreachable`,
		},
		{
			name: "transition that does nothing",
			def: func(b *sc.Builder[string, counter]) {
				b.Initial("a")
				b.State("a").On[tick]().When("never", func(counter, tick) bool { return false })
			},
			want: `state "a": a transition has neither a target nor any steps`,
		},
		{
			name: "two guards",
			def: func(b *sc.Builder[string, counter]) {
				b.Initial("a")
				b.State("a").On[tick]().
					When("one", func(counter, tick) bool { return true }).
					When("two", func(counter, tick) bool { return true }).
					Assign("increment", increment[tick])
			},
			want: `state "a": a transition has more than one guard`,
		},
		{
			name: "duplicate action name",
			def: func(b *sc.Builder[string, counter]) {
				b.Initial("a")
				b.State("a").Entry("log", noop).Exit("log", noop)
			},
			want: `state "a": more than one action is named "log"`,
		},
		{
			name: "service without error route",
			def: func(b *sc.Builder[string, counter]) {
				b.Initial("a")
				b.State("a").Invoke("load", func(context.Context, counter) (int, error) { return 0, nil }).OnDone().To("b")
				b.State("b")
			},
			want: `state "a": service "load" has no OnError transition`,
		},
		{
			name: "state declared under two parents",
			def: func(b *sc.Builder[string, counter]) {
				b.Initial("a")
				b.State("a", func(b *sc.Builder[string, counter]) {
					b.Initial("shared")
					b.State("shared")
				})
				b.State("shared")
			},
			want: `state "shared": declared under both "a" and "bad"`,
		},
		{
			name: "history default outside its parent",
			def: func(b *sc.Builder[string, counter]) {
				b.Initial("a")
				b.State("a", func(b *sc.Builder[string, counter]) {
					b.Initial("inner")
					b.State("inner").On[tick]().To("b")
					b.History("a_history", sc.Shallow).Default("b")
				})
				b.State("b").On[tick]().To("a_history")
			},
			want: `history state "a_history": default "b" is not a state inside "a"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := sc.New("bad", tc.def)

			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestNewReportsEveryErrorAtOnce(t *testing.T) {
	t.Parallel()

	_, err := sc.New("bad", func(b *sc.Builder[string, counter]) {
		b.Initial("a")
		b.State("a").On[tick]().To("missing")
		b.State("a").Entry("", noop)
	})

	require.ErrorContains(t, err, `transition targets undeclared state "missing"`)
	require.ErrorContains(t, err, `state "a": an action has no name`)
}

func TestSnapshotRoundTripsThroughJSON(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)
	snap, chargeID := submitted(t, m)
	snap, _ = m.Transition(snap, sc.Failed{ID: chargeID, Err: errCardDeclined})
	snap, _ = m.Transition(snap, hold{})

	data, err := json.Marshal(snap)
	require.NoError(t, err)

	restored, effects, err := m.Restore(data)
	require.NoError(t, err)
	require.Empty(t, effects)
	require.Equal(t, snap, restored)

	// History survived: resuming goes back to retrying, not to charging.
	restored, _ = m.Transition(restored, resume{})
	require.Equal(t, []state{paying, retrying}, restored.Active())
}

func TestRestoredSnapshotStillAcceptsItsPendingTimer(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)
	snap, chargeID := submitted(t, m)
	snap, effects := m.Transition(snap, sc.Failed{ID: chargeID, Err: errCardDeclined})
	timerID := effect(t, effects, sc.EffectStartTimer).ID

	data, err := json.Marshal(snap)
	require.NoError(t, err)

	restored, _, err := m.Restore(data)
	require.NoError(t, err)

	restored, _ = m.Transition(restored, sc.TimerFired{ID: timerID})

	require.Equal(t, []state{paying, charging}, restored.Active())
}

func TestResumeRestartsOutstandingTimersAndServices(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)
	charging, chargeID := submitted(t, m)
	retrying, effects := m.Transition(charging, sc.Failed{ID: chargeID, Err: errCardDeclined})
	timerID := effect(t, effects, sc.EffectStartTimer).ID

	service := m.Resume(charging)
	require.Equal(t, []sc.EffectKind{sc.EffectStartInvocation}, kinds(service))
	require.Equal(t, chargeID, service[0].ID)
	require.Equal(t, []string{"book"}, service[0].Context.Items)

	timer := m.Resume(retrying)
	require.Equal(t, []sc.EffectKind{sc.EffectStartTimer}, kinds(timer))
	require.Equal(t, timerID, timer[0].ID)
	require.Equal(t, retryDelay, timer[0].Delay)
}

func TestRestoreRejectsSnapshotOfAnotherVersionWithoutMigration(t *testing.T) {
	t.Parallel()

	snap, _ := orderChart(t, chargeOK).Start(order{})
	data, err := json.Marshal(snap)
	require.NoError(t, err)

	_, _, err = orderChart(t, chargeOK, sc.Version(2)).Restore(data)

	require.ErrorIs(t, err, sc.ErrVersionMismatch)
}

func TestRestoreRejectsChartChangedWithoutVersionChange(t *testing.T) {
	t.Parallel()

	before, err := sc.New("drift", func(b *sc.Builder[string, counter]) {
		b.Initial("a")
		b.State("a").On[tick]().To("b")
		b.State("b")
	})
	require.NoError(t, err)

	after, err := sc.New("drift", func(b *sc.Builder[string, counter]) {
		b.Initial("a")
		b.State("a").On[tick]().To("c")
		b.State("c")
	})
	require.NoError(t, err)

	snap, _ := before.Start(counter{})
	data, err := json.Marshal(snap)
	require.NoError(t, err)

	_, _, err = after.Restore(data)

	require.ErrorIs(t, err, sc.ErrChartChanged)
}

func TestRestoreRejectsIllegalConfiguration(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)
	snap, _ := m.Start(order{})
	data, err := json.Marshal(snap)
	require.NoError(t, err)

	var raw sc.RawSnapshot
	require.NoError(t, json.Unmarshal(data, &raw))
	raw.Active = []string{"idle", "on_hold"}
	data, err = json.Marshal(raw)
	require.NoError(t, err)

	_, _, err = m.Restore(data)

	require.ErrorIs(t, err, sc.ErrInvalidSnapshot)
}

// reviewChart is a chart at two versions. Version 1 waits in "pending" on a
// timer. Version 2 replaces that state with "review", which runs a service.
func reviewChart(t *testing.T, version int) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("review", func(b *sc.Builder[string, counter]) {
		b.Initial("open")
		b.State("approved")

		if version == 1 {
			b.State("open").On[tick]().To("pending")
			b.State("pending").After(time.Hour).To("approved")
			return
		}

		b.State("open").On[tick]().To("review")
		inv := b.State("review").Invoke("score", func(context.Context, counter) (int, error) { return 0, nil })
		inv.OnDone().To("approved")
		inv.OnError().To("open")
	}, sc.Version(version), sc.Migrate(func(old sc.RawSnapshot) (sc.RawSnapshot, error) {
		for i, name := range old.Active {
			if name == "pending" {
				old.Active[i] = "review"
			}
		}

		return old, nil
	}))
	require.NoError(t, err)

	return m
}

func TestRestoreMigratesSnapshotAndReconcilesTimersAndServices(t *testing.T) {
	t.Parallel()

	v1 := reviewChart(t, 1)
	snap, _ := v1.Start(counter{N: 7})
	snap, effects := v1.Transition(snap, tick{})
	timerID := effect(t, effects, sc.EffectStartTimer).ID

	data, err := json.Marshal(snap)
	require.NoError(t, err)

	v2 := reviewChart(t, 2)
	migrated, effects, err := v2.Restore(data)
	require.NoError(t, err)

	require.Equal(t, []string{"review"}, migrated.Active())
	require.Equal(t, 7, migrated.Context.N)
	require.Equal(t, snap.Seq(), migrated.Seq())

	// The timer belonged to a state that no longer exists, and the state
	// that replaced it has a service nothing has started yet.
	require.Equal(t, []sc.EffectKind{sc.EffectCancelTimer, sc.EffectStartInvocation}, kinds(effects))
	require.Equal(t, timerID, effects[0].ID)

	// The migrated snapshot is a normal snapshot of the new chart.
	migrated, _ = v2.Transition(migrated, sc.Done[int]{ID: effects[1].ID, Result: 1})
	require.Equal(t, []string{"approved"}, migrated.Active())

	again, err := json.Marshal(migrated)
	require.NoError(t, err)
	_, effects, err = v2.Restore(again)
	require.NoError(t, err)
	require.Empty(t, effects)
}

func TestEffectRoundTripsThroughJSONAndExecutes(t *testing.T) {
	t.Parallel()

	type seen struct {
		items []string
		event submit
	}
	got := make(chan seen, 1)

	m, err := sc.New("codec", func(b *sc.Builder[state, order]) {
		b.Initial(idle)
		b.State(idle).On[submit]().
			Assign("setItems", func(o order, e submit) order { o.Items = e.Items; return o }).
			Do("announce", func(_ context.Context, o order, e submit) { got <- seen{items: o.Items, event: e} }).
			To(shipped)
		b.Final(shipped)
	})
	require.NoError(t, err)

	snap, _ := m.Start(order{})
	_, effects := m.Transition(snap, submit{Items: []string{"book"}})
	require.Len(t, effects, 1)

	data, err := m.EncodeEffect(effects[0])
	require.NoError(t, err)

	decoded, err := m.DecodeEffect(data)
	require.NoError(t, err)
	require.Equal(t, effects[0], decoded)

	require.NoError(t, m.Exec(t.Context(), decoded, func(any) {}))
	require.Equal(t, seen{items: []string{"book"}, event: submit{Items: []string{"book"}}}, <-got)
}

func TestExecRunsServiceAndReportsItsOutcome(t *testing.T) {
	t.Parallel()

	m := orderChart(t, func(_ context.Context, o order) (string, error) {
		if len(o.Items) > 1 {
			return "", errCardDeclined
		}

		return "rcpt-" + o.Items[0], nil
	})

	run := func(items ...string) any {
		snap, _ := m.Start(order{})
		_, effects := m.Transition(snap, submit{Items: items})

		var events []any
		require.NoError(t, m.Exec(t.Context(), effect(t, effects, sc.EffectStartInvocation), func(ev any) {
			events = append(events, ev)
		}))
		require.Len(t, events, 1)

		return events[0]
	}

	done, ok := run("book").(sc.Done[string])
	require.True(t, ok)
	require.Equal(t, "rcpt-book", done.Result)

	failed, ok := run("book", "pen").(sc.Failed)
	require.True(t, ok)
	require.ErrorIs(t, failed.Err, errCardDeclined)
}

func TestExecRejectsEffectThatDoesNotMatchTheChart(t *testing.T) {
	t.Parallel()

	m := orderChart(t, chargeOK)

	err := m.Exec(t.Context(), sc.Effect[order]{Kind: sc.EffectAction, State: "idle", Name: "removed"}, func(any) {})

	require.ErrorIs(t, err, sc.ErrUnknownEffect)
}

func TestDefinitionExportsChartStructure(t *testing.T) {
	t.Parallel()

	def := orderChart(t, chargeOK).Definition()

	require.Equal(t, "order", def.ID)
	require.Equal(t, "idle", def.Initial)

	require.Equal(t, []sc.TransitionDefinition{{
		Target:  "#paying",
		Guard:   "hasItems",
		Actions: []string{"setItems", "announce"},
	}}, def.States["idle"].On["submit"])

	pay := def.States["paying"]
	require.Equal(t, "charging", pay.Initial)
	require.Equal(t, "history", pay.States["paying_history"].Type)
	require.Equal(t, "deep", pay.States["paying_history"].History)
	require.Equal(t, []sc.TransitionDefinition{{Target: "#cancelled"}}, pay.On["cancel"])

	require.Equal(t, []sc.InvokeDefinition{{
		ID:     "charge",
		Src:    "charge",
		OnDone: []sc.TransitionDefinition{{Target: "#fulfilling", Actions: []string{"setReceipt"}}},
		OnError: []sc.TransitionDefinition{
			{Target: "#retrying", Guard: "canRetry"},
			{Target: "#cancelled"},
		},
	}}, pay.States["charging"].Invoke)

	require.Equal(t,
		[]sc.TransitionDefinition{{Target: "#charging", Actions: []string{"countAttempt"}}},
		pay.States["retrying"].After["2000"],
	)

	fulfil := def.States["fulfilling"]
	require.Equal(t, "parallel", fulfil.Type)
	require.Equal(t, []sc.TransitionDefinition{{Target: "#shipped"}}, fulfil.OnDone)
	require.Equal(t, "final", fulfil.States["packing"].States["packed"].Type)

	require.Equal(t, []string{"refund"}, def.States["cancelled"].Entry)

	_, err := json.Marshal(def)
	require.NoError(t, err)
}
