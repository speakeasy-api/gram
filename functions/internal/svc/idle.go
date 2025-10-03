package svc

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type IdleTracker struct {
	mu     sync.Mutex
	active map[net.Conn]bool
	idle   time.Duration
	timer  *time.Timer
}

func NewIdleTracker(idle time.Duration) *IdleTracker {
	return &IdleTracker{
		mu:     sync.Mutex{},
		active: make(map[net.Conn]bool),
		idle:   idle,
		timer:  time.NewTimer(idle),
	}
}

func (t *IdleTracker) ConnState(conn net.Conn, state http.ConnState) {
	t.mu.Lock()
	defer t.mu.Unlock()

	prevActive := len(t.active)
	switch state {
	case http.StateNew, http.StateActive, http.StateHijacked:
		t.active[conn] = true
		if prevActive == 0 {
			t.timer.Stop()
		}
	case http.StateIdle, http.StateClosed:
		delete(t.active, conn)
		if prevActive > 0 && len(t.active) == 0 {
			t.timer.Reset(t.idle)
		}
	}
}

func (t *IdleTracker) Done() <-chan time.Time {
	return t.timer.C
}
