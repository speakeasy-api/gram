package agentevents

import (
	"fmt"
	"sync"

	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
)

type agentHandle interface {
	ProviderID() types.Provider
}

type Mux struct {
	mu     sync.Mutex
	Agents map[types.Provider]agentHandle
}

func NewMux() *Mux {
	return &Mux{
		Agents: make(map[types.Provider]agentHandle),
	}
}

func (m *Mux) Register(agent agentHandle, err error) error {
	if err != nil {
		return err
	}
	if agent == nil {
		return ErrNilHandle
	}
	if agent.ProviderID() == "" {
		return ErrEmptyProvider
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.Agents[agent.ProviderID()]; ok {
		return fmt.Errorf("%w: %s", ErrDuplicateProvider, agent.ProviderID())
	}
	m.Agents[agent.ProviderID()] = agent
	return nil
}
