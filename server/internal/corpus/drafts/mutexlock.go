package drafts

import (
	"context"
	"sync"
)

// MutexWriteLock is an in-process WriteLock backed by sync.Mutex.
// Suitable for single-pod dev environments.
type MutexWriteLock struct {
	mu sync.Mutex
}

func NewMutexWriteLock() *MutexWriteLock {
	return &MutexWriteLock{mu: sync.Mutex{}}
}

func (m *MutexWriteLock) Lock(_ context.Context, _ string) error {
	m.mu.Lock()
	return nil
}

func (m *MutexWriteLock) Unlock(_ context.Context, _ string) error {
	m.mu.Unlock()
	return nil
}
