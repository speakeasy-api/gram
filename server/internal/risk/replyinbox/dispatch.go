package replyinbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
)

const (
	// DefaultWaitTimeout prevents deadline-free callers from retaining waiters.
	DefaultWaitTimeout = 30 * time.Second

	// MaxContentBytes matches the established per-message Presidio safety budget.
	MaxContentBytes = 50 * 1024
)

// DispatcherConfig controls bounded request publication and reply waiting.
type DispatcherConfig struct {
	// WaitTimeout caps dispatch publication and reply waiting.
	WaitTimeout time.Duration
}

// DispatchRequest contains tenant context, content, and requested lanes.
type DispatchRequest struct {
	// OrganizationID is the tenant used for fingerprint isolation.
	OrganizationID string

	// ProjectID identifies the project whose policy configuration applies.
	ProjectID string

	// Content is the raw text scanned by each lane.
	Content string

	// Lanes is the distinct set of scanner and policy results required.
	Lanes []Lane
}

// Dispatcher publishes enforcement requests and awaits their correlated replies.
type Dispatcher struct {
	inbox       *Inbox
	gitleaksPub gcp.Publisher[*riskv1.GitleaksEnforcement]
	waitTimeout time.Duration
}

// NewDispatcher resolves publishers for the supported enforcement lanes.
func NewDispatcher(ctx context.Context, broker gcp.PublisherBroker, inbox *Inbox, cfg DispatcherConfig) (*Dispatcher, error) {
	if inbox == nil {
		return nil, errors.New("enforcement reply inbox is required")
	}
	if cfg.WaitTimeout <= 0 {
		cfg.WaitTimeout = DefaultWaitTimeout
	}
	gitleaksPub, err := gcp.PubSubPublisherForMessage(ctx, broker, &riskv1.GitleaksEnforcement{})
	if err != nil {
		return nil, fmt.Errorf("create gitleaks enforcement publisher: %w", err)
	}
	return &Dispatcher{inbox: inbox, gitleaksPub: gitleaksPub, waitTimeout: cfg.WaitTimeout}, nil
}

// Dispatch fans content out to distinct lanes and folds replies by lane.
func (d *Dispatcher) Dispatch(ctx context.Context, request DispatchRequest) (Outcome, error) {
	if request.OrganizationID == "" {
		return Outcome{}, fmt.Errorf("enforcement organization id is required")
	}
	if request.ProjectID == "" {
		return Outcome{}, fmt.Errorf("enforcement project id is required")
	}
	if len(request.Content) > MaxContentBytes {
		return Outcome{}, fmt.Errorf("enforcement content is %d bytes; maximum is %d bytes", len(request.Content), MaxContentBytes)
	}
	if len(request.Lanes) == 0 {
		return Outcome{ByLane: map[Lane]*riskv1.EnforcementReply{}, Complete: true, Deadline: false}, nil
	}
	seen := make(map[Lane]struct{}, len(request.Lanes))
	for _, lane := range request.Lanes {
		if _, duplicate := seen[lane]; duplicate {
			return Outcome{}, fmt.Errorf("duplicate enforcement lane %s", lane.String())
		}
		seen[lane] = struct{}{}
		if lane.Scanner != riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_GITLEAKS || lane.PolicyID != "" {
			return Outcome{}, fmt.Errorf("unsupported enforcement lane %s", lane.String())
		}
	}

	scanUUID, err := uuid.NewV7()
	if err != nil {
		return Outcome{}, fmt.Errorf("mint enforcement scan id: %w", err)
	}
	scanID := scanUUID.String()
	started := time.Now()
	w, release, err := d.inbox.register(scanID, request.Lanes)
	if err != nil {
		return Outcome{}, err
	}
	defer release()

	waitCtx, cancel := context.WithTimeout(ctx, d.waitTimeout)
	defer cancel()
	createdAt := time.Now().UTC().Format(time.RFC3339Nano)
	results := make([]gcp.PublishResult, 0, len(request.Lanes))
	for _, lane := range request.Lanes {
		switch lane.Scanner {
		case riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_GITLEAKS:
			enforcement := riskv1.GitleaksEnforcement_builder{
				RequestId:      new(scanID),
				ProjectId:      new(request.ProjectID),
				OrganizationId: new(request.OrganizationID),
				CreatedAt:      new(createdAt),
				ReplyUrn:       new(d.inbox.ReplyURN(scanID)),
				Content:        new(request.Content),
			}.Build()
			results = append(results, d.gitleaksPub.Publish(waitCtx, enforcement))
		default:
			return Outcome{}, fmt.Errorf("unsupported enforcement lane %s", lane.String())
		}
	}
	for _, result := range results {
		if _, err := result.Get(waitCtx); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return d.inbox.awaitRegistered(waitCtx, scanID, w, started)
			}
			return Outcome{}, fmt.Errorf("publish enforcement request: %w", err)
		}
	}

	return d.inbox.awaitRegistered(waitCtx, scanID, w, started)
}

// Close flushes and stops the dispatcher's publishers.
func (d *Dispatcher) Close(ctx context.Context) error {
	if err := d.gitleaksPub.Stop(ctx); err != nil {
		return fmt.Errorf("stop gitleaks enforcement publisher: %w", err)
	}
	return nil
}
