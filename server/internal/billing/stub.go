package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/must"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

type StubClient struct {
	mut    sync.Mutex
	logger *slog.Logger
}

func NewStubClient(logger *slog.Logger) *StubClient {
	if logger == nil {
		logger = slog.Default()
	}

	return &StubClient{
		mut:    sync.Mutex{},
		logger: logger.With(attr.SlogComponent("billing-stub")),
	}
}

var _ Tracker = (*StubClient)(nil)
var _ Repository = (*StubClient)(nil)

func (s *StubClient) CreateCheckout(ctx context.Context, orgID string, serverURL string) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (s *StubClient) CreateCustomerSession(ctx context.Context, orgID string) (string, error) {
	return "", fmt.Errorf("not implemented")
}

// GetCustomer implements Repository.
func (s *StubClient) GetCustomer(ctx context.Context, orgID string) (*Customer, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	pu, err := s.readPeriodUsage(orgID)
	if err != nil {
		return nil, fmt.Errorf("read period usage file: %w", err)
	}

	return &Customer{
		OrganizationID: orgID,
		Tier:           TierPro,
		PeriodUsage:    pu,
	}, nil
}

func (s *StubClient) GetPeriodUsage(ctx context.Context, orgID string) (*gen.PeriodUsage, error) {
	s.mut.Lock()
	defer s.mut.Unlock()

	pu, err := s.readPeriodUsage(orgID)
	if err != nil {
		return nil, fmt.Errorf("read period usage file: %w", err)
	}

	return pu, nil
}

func (s *StubClient) GetUsageTiers(ctx context.Context) (*gen.UsageTiers, error) {
	return &gen.UsageTiers{
		Free: &gen.TierLimits{
			BasePrice:                  0,
			IncludedToolCalls:          1e4,
			IncludedServers:            3,
			PricePerAdditionalToolCall: 0,
			PricePerAdditionalServer:   0,
		},
		Business: &gen.TierLimits{
			BasePrice:                  500,
			IncludedToolCalls:          1e8,
			IncludedServers:            50,
			PricePerAdditionalToolCall: 0.00001,
			PricePerAdditionalServer:   0.5,
		},
	}, nil
}

func (s *StubClient) TrackPlatformUsage(ctx context.Context, event PlatformUsageEvent) {
	s.mut.Lock()
	defer s.mut.Unlock()

	pu, err := s.readPeriodUsage(event.OrganizationID)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to read period usage file", attr.SlogError(err))
		return
	}

	pu.Servers = int(event.PrivateMCPServers)
	pu.ActualPublicServerCount = int(event.PublicMCPServers)

	if err := s.writePeriodUsage(ctx, event.OrganizationID, pu); err != nil {
		s.logger.ErrorContext(ctx, "failed to write period usage file", attr.SlogError(err))
		return
	}
}

func (s *StubClient) TrackPromptCallUsage(ctx context.Context, event PromptCallUsageEvent) {
	s.logger.ErrorContext(ctx, "failed to track prompt call usage: not implemented")
}

func (s *StubClient) TrackToolCallUsage(ctx context.Context, event ToolCallUsageEvent) {
	s.mut.Lock()
	defer s.mut.Unlock()

	pu, err := s.readPeriodUsage(event.OrganizationID)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to read period usage file", attr.SlogError(err))
		return
	}

	pu.ToolCalls += 1

	if err := s.writePeriodUsage(ctx, event.OrganizationID, pu); err != nil {
		s.logger.ErrorContext(ctx, "failed to write period usage file", attr.SlogError(err))
		return
	}
}

func (s *StubClient) ensureDataDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	billingDir := filepath.Join(wd, "scratch")
	if err := os.MkdirAll(billingDir, 0750); err != nil {
		return "", fmt.Errorf("create billing scratch directory: %w", err)
	}

	return billingDir, nil
}

func (s *StubClient) readPeriodUsage(orgID string) (*gen.PeriodUsage, error) {
	datadir, err := s.ensureDataDir()
	if err != nil {
		return nil, fmt.Errorf("get or create local billing data dir: %w", err)
	}

	tier := must.Value(s.GetUsageTiers(context.Background())).Business
	zero := &gen.PeriodUsage{
		ToolCalls:               0,
		MaxToolCalls:            tier.IncludedToolCalls,
		Servers:                 0,
		MaxServers:              tier.IncludedServers,
		ActualPublicServerCount: 0,
	}

	usagefile := filepath.Join(datadir, fmt.Sprintf("billingusage-%s.local.json", orgID))
	content, err := os.ReadFile(filepath.Clean(usagefile))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return zero, nil
	case err != nil:
		return nil, fmt.Errorf("read local billing file: %w", err)
	}

	if len(content) == 0 {
		return zero, nil
	}

	var pu gen.PeriodUsage
	if err := json.Unmarshal(content, &pu); err != nil {
		return nil, fmt.Errorf("unmarshal local billing file: %w", err)
	}

	return &pu, nil
}

func (s *StubClient) writePeriodUsage(ctx context.Context, orgID string, pu *gen.PeriodUsage) error {
	datadir, err := s.ensureDataDir()
	if err != nil {
		return fmt.Errorf("get or create local billing data dir: %w", err)
	}

	usagefile := filepath.Join(datadir, fmt.Sprintf("billingusage-%s.local.json", orgID))
	f, err := os.Create(filepath.Clean(usagefile))
	if err != nil {
		return fmt.Errorf("open local billing file: %w", err)
	}
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return f.Close()
	})

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(pu); err != nil {
		return fmt.Errorf("serialize local billing data: %w", err)
	}

	return nil
}
