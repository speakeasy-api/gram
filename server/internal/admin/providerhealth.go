package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
)

// ErrProviderHealthUnavailable means this deployment has no provider health reader.
var ErrProviderHealthUnavailable = errors.New("provider health is unavailable")

// SetProviderHealth installs the cross-organization provider health reader for Admin MCP.
func (s *Service) SetProviderHealth(health *mcpconnectionattempts.ProviderHealth) {
	s.providerHealth = health
}

// ReadProviderHealth returns every known provider's health across organizations, as counts only.
func (s *Service) ReadProviderHealth(ctx context.Context, query mcpconnectionattempts.ProviderHealthQuery) ([]mcpconnectionattempts.ProviderHealthReport, error) {
	if s.providerHealth == nil {
		return nil, ErrProviderHealthUnavailable
	}
	reports, err := s.providerHealth.Read(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("read provider health: %w", err)
	}
	return reports, nil
}
