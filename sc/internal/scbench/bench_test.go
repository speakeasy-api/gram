// Package scbench measures sc in memory against the same machines written
// by hand, so that a change to the core or a driver shows its cost.
package scbench

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/sc"
)

// ---- the workload: an order that is submitted, paid, shipped, and reset.

type order struct {
	Items    int `json:"items"`
	Attempts int `json:"attempts"`
	Paid     int `json:"paid"`
}

type (
	submit struct{ Items int }
	pay    struct{ Amount int }
	ship   struct{}
	reset  struct{}
	noise  struct{}
)

var now = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func flat(tb testing.TB) *sc.Machine[string, order] {
	tb.Helper()

	m, err := sc.New("order", func(b *sc.Builder[string, order]) {
		b.Initial("idle")
		b.State("idle").On[submit]().
			When("hasItems", func(_ order, e submit) bool { return e.Items > 0 }).
			Assign("setItems", func(o order, e submit) order { o.Items = e.Items; return o }).
			To("pending")
		b.State("pending").On[pay]().
			Assign("setPaid", func(o order, e pay) order { o.Paid += e.Amount; o.Attempts++; return o }).
			To("paid")
		b.State("paid").On[ship]().To("shipped")
		b.State("shipped").On[reset]().To("idle")
	})
	if err != nil {
		tb.Fatal(err)
	}
	return m
}

// The same thing written by hand.
type handState uint8

const (
	hIdle handState = iota
	hPending
	hPaid
	hShipped
)

type hand struct {
	state handState
	ctx   order
}

func (h *hand) send(ev any) bool {
	switch e := ev.(type) {
	case submit:
		if h.state == hIdle && e.Items > 0 {
			h.ctx.Items = e.Items
			h.state = hPending
			return true
		}
	case pay:
		if h.state == hPending {
			h.ctx.Paid += e.Amount
			h.ctx.Attempts++
			h.state = hPaid
			return true
		}
	case ship:
		if h.state == hPaid {
			h.state = hShipped
			return true
		}
	case reset:
		if h.state == hShipped {
			h.state = hIdle
			return true
		}
	}
	return false
}

var cycle = []any{submit{Items: 2}, pay{Amount: 10}, ship{}, reset{}}

func BenchmarkCore_Flat_Hand(b *testing.B) {
	h := &hand{}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		h.send(cycle[i&3])
	}
}

func BenchmarkCore_Flat_SC(b *testing.B) {
	m := flat(b)
	snap, _ := m.Start(order{}, now)
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		snap, _ = m.Transition(snap, cycle[i&3], now)
	}
}

func BenchmarkCore_IgnoredEvent_Hand(b *testing.B) {
	h := &hand{}
	b.ReportAllocs()
	for b.Loop() {
		h.send(noise{})
	}
}

func BenchmarkCore_IgnoredEvent_SC(b *testing.B) {
	m := flat(b)
	snap, _ := m.Start(order{}, now)
	b.ReportAllocs()
	for b.Loop() {
		snap, _ = m.Transition(snap, noise{}, now)
	}
}

// ---- a deeper chart: three levels and two parallel regions, with entry
// actions and a timer, which is closer to the lifecycles sc is for.

type (
	connect    struct{}
	connected  struct{}
	disconnect struct{}
	beat       struct{}
)

func deep(tb testing.TB) *sc.Machine[string, order] {
	tb.Helper()

	m, err := sc.New("host", func(b *sc.Builder[string, order]) {
		b.Initial("offline")
		b.State("offline").On[connect]().To("online")
		b.Parallel("online", func(b *sc.Builder[string, order]) {
			b.State("link", func(b *sc.Builder[string, order]) {
				b.Initial("connecting")
				b.State("connecting").After(30 * time.Second).To("failed")
				b.State("connecting").On[connected]().To("up")
				b.State("up", func(b *sc.Builder[string, order]) {
					b.Initial("steady")
					b.State("steady").On[beat]().Assign("count", func(o order, _ beat) order { o.Attempts++; return o })
				}).Entry("announce", func(context.Context, order) {})
				b.State("failed")
			})
			b.State("health", func(b *sc.Builder[string, order]) {
				b.Initial("unknown")
				b.State("unknown").On[beat]().To("healthy")
				b.State("healthy").After(time.Minute).To("unknown")
			})
		}).On[disconnect]().To("offline")
	})
	if err != nil {
		tb.Fatal(err)
	}
	return m
}

var deepCycle = []any{connect{}, connected{}, beat{}, beat{}, disconnect{}}

func BenchmarkCore_Deep_SC(b *testing.B) {
	m := deep(b)
	snap, _ := m.Start(order{}, now)
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		snap, _ = m.Transition(snap, deepCycle[i%5], now)
	}
}

// The steady state of that chart: an event that only assigns, two regions
// active, two timers pending.
func BenchmarkCore_Deep_TargetlessAssign_SC(b *testing.B) {
	m := deep(b)
	snap, _ := m.Start(order{}, now)
	for _, ev := range []any{connect{}, connected{}, beat{}} {
		snap, _ = m.Transition(snap, ev, now)
	}
	b.ReportAllocs()
	for b.Loop() {
		snap, _ = m.Transition(snap, beat{}, now)
	}
}

// ---- drivers, against the hand-written machine made safe the same way.

type lockedHand struct {
	mu sync.Mutex
	h  hand
}

func (l *lockedHand) send(ev any) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.h.send(ev)
}

func BenchmarkDriver_Instance_Hand(b *testing.B) {
	l := &lockedHand{}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		l.send(cycle[i&3])
	}
}

func BenchmarkDriver_Instance_SC(b *testing.B) {
	inst, err := flat(b).Instance(context.Background(), order{})
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close()
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		_, _ = inst.Send(ctx, cycle[i&3])
	}
}

func BenchmarkDriver_InstanceParallel_Hand(b *testing.B) {
	l := &lockedHand{}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for i := 0; pb.Next(); i++ {
			l.send(cycle[i&3])
		}
	})
}

func BenchmarkDriver_InstanceParallel_SC(b *testing.B) {
	inst, err := flat(b).Instance(context.Background(), order{})
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close()
	ctx := context.Background()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for i := 0; pb.Next(); i++ {
			_, _ = inst.Send(ctx, cycle[i&3])
		}
	})
}

// A goroutine that owns the machine and takes events from a channel.
type chanHand struct {
	in chan chanMsg
}

type chanMsg struct {
	ev    any
	reply chan bool
}

func newChanHand(ctx context.Context) *chanHand {
	c := &chanHand{in: make(chan chanMsg)}
	go func() {
		var h hand
		for {
			select {
			case m := <-c.in:
				ok := h.send(m.ev)
				if m.reply != nil {
					m.reply <- ok
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return c
}

func BenchmarkDriver_ActorCall_Hand(b *testing.B) {
	ctx := b.Context()
	c := newChanHand(ctx)
	reply := make(chan bool, 1)
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		c.in <- chanMsg{ev: cycle[i&3], reply: reply}
		<-reply
	}
}

func BenchmarkDriver_ActorCall_SC(b *testing.B) {
	ctx := b.Context()
	a := flat(b).Actor(order{})
	go func() { _ = a.Run(ctx) }()
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		_, _ = a.Call(ctx, cycle[i&3])
	}
}

// ---- a service round trip: enter a state, run a function on a goroutine,
// take its result.

type (
	work struct{}
)

func BenchmarkService_RoundTrip_Hand(b *testing.B) {
	var mu sync.Mutex
	runs := 0
	b.ReportAllocs()
	for b.Loop() {
		done := make(chan int, 1)
		mu.Lock()
		runs++
		mu.Unlock()
		go func() { done <- 1 }()
		result := <-done
		mu.Lock()
		runs += result - 1
		mu.Unlock()
	}
	if runs != b.N {
		b.Logf("runs: %d", runs)
	}
}

func BenchmarkService_RoundTrip_SC(b *testing.B) {
	m, err := sc.New("svc", func(b *sc.Builder[string, order]) {
		b.Initial("idle")
		b.State("idle").On[work]().To("working")
		inv := b.State("working").Invoke("do", func(context.Context, order) (int, error) { return 1, nil })
		inv.OnDone().To("idle")
		inv.OnError().To("idle")
	})
	if err != nil {
		b.Fatal(err)
	}
	inst, err := m.Instance(context.Background(), order{})
	if err != nil {
		b.Fatal(err)
	}
	defer inst.Close()
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		_, _ = inst.SendAndSettle(ctx, work{})
	}
}

// ---- persistence and set-up.

func BenchmarkSnapshot_Marshal_SC(b *testing.B) {
	m := deep(b)
	snap, _ := m.Start(order{}, now)
	for _, ev := range []any{connect{}, connected{}, beat{}} {
		snap, _ = m.Transition(snap, ev, now)
	}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = json.Marshal(snap)
	}
}

func BenchmarkSnapshot_Marshal_Hand(b *testing.B) {
	type stored struct {
		State string `json:"state"`
		Ctx   order  `json:"ctx"`
	}
	v := stored{State: "up", Ctx: order{Items: 2, Attempts: 3}}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = json.Marshal(v)
	}
}

func BenchmarkSnapshot_Restore_SC(b *testing.B) {
	m := deep(b)
	snap, _ := m.Start(order{}, now)
	for _, ev := range []any{connect{}, connected{}, beat{}} {
		snap, _ = m.Transition(snap, ev, now)
	}
	data, _ := json.Marshal(snap)
	b.ReportAllocs()
	for b.Loop() {
		_, _, _ = m.Restore(data, now)
	}
}

func BenchmarkSetup_New_SC(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		deep(b)
	}
}

func BenchmarkSetup_Start_SC(b *testing.B) {
	m := deep(b)
	b.ReportAllocs()
	for b.Loop() {
		m.Start(order{}, now)
	}
}

func BenchmarkSetup_Instance_SC(b *testing.B) {
	m := flat(b)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		inst, _ := m.Instance(ctx, order{})
		inst.Close()
	}
}

// ---- checking.

func BenchmarkExplore_Deep_SC(b *testing.B) {
	m := deep(b)
	states := 0
	b.ReportAllocs()
	for b.Loop() {
		r, err := m.Explore(order{}, sc.Exploration[string, order]{
			Events: []any{connect{}, connected{}, disconnect{}},
		})
		if err != nil {
			b.Fatal(err)
		}
		states = r.States
	}
	b.ReportMetric(float64(states), "states")
}
