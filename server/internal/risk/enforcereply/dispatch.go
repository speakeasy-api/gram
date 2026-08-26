package enforcereply

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/redisinbox"
	"github.com/speakeasy-api/gram/server/internal/requestreply"
)

const (
	// DefaultWaitTimeout prevents deadline-free callers from retaining waiters.
	DefaultWaitTimeout = 30 * time.Second

	// MaxContentBytes matches the established per-message Presidio safety budget.
	MaxContentBytes = 50 * 1024
)

// EnforcementLane is the non-generic request seam used by enforcement fan-out.
type EnforcementLane interface {
	Request(ctx context.Context, req proto.Message) (*riskv1.EnforcementReply, error)
}

type typedEnforcementLane[Req proto.Message] struct {
	broker requestreply.RequestBroker[Req, *riskv1.EnforcementReply]
}

func (l *typedEnforcementLane[Req]) Request(ctx context.Context, req proto.Message) (*riskv1.EnforcementReply, error) {
	typed, ok := req.(Req)
	if !ok {
		return nil, fmt.Errorf("unexpected enforcement request type %T", req)
	}
	reply, err := l.broker.Request(ctx, typed)
	if err != nil {
		return nil, fmt.Errorf("request typed enforcement lane: %w", err)
	}
	return reply, nil
}

// DispatcherConfig controls bounded request publication and reply waiting.
type DispatcherConfig struct {
	// WaitTimeout caps each lane's publication and reply wait.
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

// Dispatcher fans enforcement work out over independent request brokers.
type Dispatcher struct {
	gitleaks    EnforcementLane
	presidio    EnforcementLane
	close       func(context.Context) error
	waitTimeout time.Duration
}

// NewDispatcher resolves request brokers for the supported enforcement lanes.
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
	presidioPub, err := gcp.PubSubPublisherForMessage(ctx, broker, &riskv1.PresidioEnforcement{})
	if err != nil {
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = gitleaksPub.Stop(stopCtx)
		return nil, fmt.Errorf("create presidio enforcement publisher: %w", err)
	}
	gitleaksReq := redisinbox.NewRequestBroker(inbox, gitleaksPub)
	presidioReq := redisinbox.NewRequestBroker(inbox, presidioPub)
	return &Dispatcher{
		gitleaks: &typedEnforcementLane[*riskv1.GitleaksEnforcement]{broker: gitleaksReq},
		presidio: &typedEnforcementLane[*riskv1.PresidioEnforcement]{broker: presidioReq},
		close: func(ctx context.Context) error {
			var closeErrs []error
			if err := gitleaksReq.Close(ctx); err != nil {
				closeErrs = append(closeErrs, fmt.Errorf("close gitleaks enforcement lane: %w", err))
			}
			if err := presidioReq.Close(ctx); err != nil {
				closeErrs = append(closeErrs, fmt.Errorf("close presidio enforcement lane: %w", err))
			}
			return errors.Join(closeErrs...)
		},
		waitTimeout: cfg.WaitTimeout,
	}, nil
}

// Dispatch fans content out to distinct lanes and folds replies by lane.
func (d *Dispatcher) Dispatch(ctx context.Context, request DispatchRequest) (Outcome, error) {
	if request.OrganizationID == "" {
		return Outcome{}, errors.New("enforcement organization id is required")
	}
	if request.ProjectID == "" {
		return Outcome{}, errors.New("enforcement project id is required")
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
		supported := lane.Scanner == riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_GITLEAKS ||
			lane.Scanner == riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_PRESIDIO
		if !supported || lane.PolicyID != "" {
			return Outcome{}, fmt.Errorf("unsupported enforcement lane %s", lane.String())
		}
	}

	requestID, err := uuid.NewV7()
	if err != nil {
		return Outcome{}, fmt.Errorf("mint enforcement request id: %w", err)
	}
	createdAt := time.Now().UTC().Format(time.RFC3339Nano)
	byLane := make(map[Lane]*riskv1.EnforcementReply, len(request.Lanes))
	deadline := false
	var mu sync.Mutex
	group, groupCtx := errgroup.WithContext(ctx)
	for _, lane := range request.Lanes {
		group.Go(func() error {
			laneCtx, cancel := context.WithTimeout(groupCtx, d.waitTimeout)
			defer cancel()
			var laneBroker EnforcementLane
			var enforcement proto.Message
			switch lane.Scanner {
			case riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_PRESIDIO:
				laneBroker = d.presidio
				enforcement = riskv1.PresidioEnforcement_builder{
					RequestId:      new(requestID.String()),
					ProjectId:      new(request.ProjectID),
					OrganizationId: new(request.OrganizationID),
					CreatedAt:      new(createdAt),
					Content:        new(request.Content),
				}.Build()
			default:
				laneBroker = d.gitleaks
				enforcement = riskv1.GitleaksEnforcement_builder{
					RequestId:      new(requestID.String()),
					ProjectId:      new(request.ProjectID),
					OrganizationId: new(request.OrganizationID),
					CreatedAt:      new(createdAt),
					Content:        new(request.Content),
				}.Build()
			}
			reply, requestErr := laneBroker.Request(laneCtx, enforcement)
			if requestErr != nil {
				if errors.Is(requestErr, context.DeadlineExceeded) {
					mu.Lock()
					deadline = true
					mu.Unlock()
					return nil
				}
				return fmt.Errorf("request enforcement lane %s: %w", lane.String(), requestErr)
			}
			mu.Lock()
			byLane[lane] = reply
			mu.Unlock()
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return Outcome{}, fmt.Errorf("dispatch enforcement lanes: %w", err)
	}
	return Outcome{ByLane: byLane, Complete: len(byLane) == len(request.Lanes), Deadline: deadline}, nil
}

// Close flushes and stops the dispatcher's publishers.
func (d *Dispatcher) Close(ctx context.Context) error {
	if err := d.close(ctx); err != nil {
		return fmt.Errorf("close enforcement dispatcher: %w", err)
	}
	return nil
}

var _ EnforcementLane = (*typedEnforcementLane[*riskv1.GitleaksEnforcement])(nil)
