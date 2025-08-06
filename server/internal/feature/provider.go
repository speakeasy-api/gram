package feature

import (
	"context"
	"sync"
)

type Provider interface {
	IsFlagEnabled(ctx context.Context, flag Flag, distinctID string) (bool, error)
}

type InMemory sync.Map

func (imp *InMemory) IsFlagEnabled(ctx context.Context, flag Flag, distinctID string) (bool, error) {
	key := distinctID + ":" + string(flag)

	val, ok := (*sync.Map)(imp).Load(key)
	if !ok {
		return false, nil
	}

	enabled, ok := val.(bool)
	if !ok {
		return false, nil
	}

	return enabled, nil
}

func (imp *InMemory) SetFlag(flag Flag, distinctID string, enabled bool) {
	key := distinctID + ":" + string(flag)

	(*sync.Map)(imp).Store(key, enabled)
}
