package sc_test

import (
	"context"
	"encoding/json"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

// requestBudget is the time one request may take across all its steps, as
// the certificate tracker allows two minutes for up to four calls.
const requestBudget = 2 * time.Minute

// requestChart runs two steps one after the other inside a request state
// with one budget for both.
func requestChart(t *testing.T, step func(context.Context, counter) (int, error)) *sc.Machine[string, counter] {
	t.Helper()

	m, err := sc.New("request", func(b *sc.Builder[string, counter]) {
		b.Initial("idle")
		b.State("idle").On[tick]().To("request")
		b.State("request", func(b *sc.Builder[string, counter]) {
			b.Initial("first")
			first := b.State("first").Invoke("first", step)
			first.OnDone().To("second")
			first.OnError().To("failed")
			second := b.State("second").Invoke("second", step)
			second.OnDone().To("done")
			second.OnError().To("failed")
			b.Final("done")
			b.Final("failed")
		}).Within(requestBudget).OnDone().To("idle")
	})
	require.NoError(t, err)

	return m
}

func TestServicesInsideABudgetShareOneDeadline(t *testing.T) {
	t.Parallel()

	m := requestChart(t, func(context.Context, counter) (int, error) { return 0, nil })
	snap, _ := m.Start(counter{}, epoch)

	snap, effects := m.Transition(snap, tick{}, epoch)
	first := effect(t, effects, sc.EffectStartInvocation)
	require.Equal(t, epoch.Add(requestBudget), first.At)

	_, effects = m.Transition(snap, sc.Done[int]{ID: first.ID, Result: 1}, epoch.Add(time.Minute))
	second := effect(t, effects, sc.EffectStartInvocation)
	require.Equal(t, epoch.Add(requestBudget), second.At)
}

func TestAServiceThatOverrunsItsBudgetFails(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		m := requestChart(t, func(ctx context.Context, _ counter) (int, error) {
			<-ctx.Done()
			return 0, ctx.Err()
		})

		inst, err := m.Instance(t.Context(), counter{})
		require.NoError(t, err)
		defer inst.Close()

		_, err = inst.Send(t.Context(), tick{})
		require.NoError(t, err)

		time.Sleep(requestBudget + time.Second) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
		synctest.Wait()

		require.Equal(t, []string{"idle"}, inst.Snapshot().Active())
	})
}

func TestABudgetSurvivesARestart(t *testing.T) {
	t.Parallel()

	m := requestChart(t, func(context.Context, counter) (int, error) { return 0, nil })
	snap, _ := m.Start(counter{}, epoch)
	snap, _ = m.Transition(snap, tick{}, epoch)

	data, err := json.Marshal(snap)
	require.NoError(t, err)
	restored, _, err := m.Restore(data, epoch.Add(time.Minute))
	require.NoError(t, err)

	resumed := m.Resume(restored)
	require.Len(t, resumed, 1)
	require.Equal(t, epoch.Add(requestBudget), resumed[0].At)
}

func TestTheEarlierOfNestedBudgetsApplies(t *testing.T) {
	t.Parallel()

	m, err := sc.New("nested", func(b *sc.Builder[string, counter]) {
		b.Initial("outer")
		b.State("outer", func(b *sc.Builder[string, counter]) {
			b.Initial("inner")
			inv := b.State("inner").Within(time.Minute).Invoke("call", func(context.Context, counter) (int, error) { return 0, nil })
			inv.OnDone().Stay()
			inv.OnError().Stay()
		}).Within(time.Hour)
	})
	require.NoError(t, err)

	_, effects := m.Start(counter{}, epoch)
	require.Equal(t, epoch.Add(time.Minute), effect(t, effects, sc.EffectStartInvocation).At)
}

func TestExploreFailsAServiceWhenItsBudgetRunsOut(t *testing.T) {
	t.Parallel()

	m := requestChart(t, func(context.Context, counter) (int, error) { return 0, nil })

	report, err := m.Explore(counter{}, sc.Exploration[string, counter]{
		Events:   []any{tick{}},
		Outcomes: map[string][]sc.Outcome{"first": {sc.Succeeds(1)}, "second": {sc.Succeeds(1)}},
	})
	require.NoError(t, err)

	// No outcome fails, so only the budget can lead to failed.
	require.True(t, report.Reached("failed"))
}
