package litellm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/litellm/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	healthCoalesceWindow = time.Second
	healthWriteTimeout   = 5 * time.Second
	maxReportedVersion   = 128
)

type healthSignal uint8

const (
	healthSignalNone healthSignal = iota
	healthSignalGuardrail
	healthSignalOTEL
)

type healthErrorKind string

const (
	healthErrorNone          healthErrorKind = ""
	healthErrorAuthFailure   healthErrorKind = "auth_failure"
	healthErrorDecode        healthErrorKind = "decode_failure"
	healthErrorLimitExceeded healthErrorKind = "limit_exceeded"
)

type healthInstanceKey struct {
	organizationID string
	projectID      uuid.UUID
	apiKeyID       uuid.UUID
	instanceID     uuid.UUID
}

type healthUpdate struct {
	guardrailObservedAt time.Time
	otelObservedAt      time.Time
	errorObservedAt     time.Time
	errorKind           healthErrorKind
	reportedVersionAt   time.Time
	reportedVersion     string
}

type healthWriteFunc func(context.Context, repo.RecordLiteLLMInstanceHealthParams) error

// HealthProcessor coalesces request-path health observations before writing
// them so diagnostics never add a database round trip to an ingest response.
type HealthProcessor struct {
	logger         *slog.Logger
	write          healthWriteFunc
	coalesceWindow time.Duration
	wake           chan struct{}
	stop           chan struct{}
	done           chan struct{}

	mu       sync.Mutex
	pending  map[healthInstanceKey]healthUpdate
	started  bool
	stopping bool
}

func NewHealthProcessor(logger *slog.Logger, db *pgxpool.Pool) *HealthProcessor {
	queries := repo.New(db)
	return newHealthProcessor(logger, healthCoalesceWindow, queries.RecordLiteLLMInstanceHealth)
}

func newHealthProcessor(logger *slog.Logger, coalesceWindow time.Duration, write healthWriteFunc) *HealthProcessor {
	return &HealthProcessor{
		logger:         logger.With(attr.SlogComponent("litellm.health")),
		write:          write,
		coalesceWindow: coalesceWindow,
		wake:           make(chan struct{}, 1),
		stop:           make(chan struct{}),
		done:           make(chan struct{}),
		mu:             sync.Mutex{},
		pending:        make(map[healthInstanceKey]healthUpdate),
		started:        false,
		stopping:       false,
	}
}

func (p *HealthProcessor) Start(ctx context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started {
		return
	}
	p.started = true
	go p.run(context.WithoutCancel(ctx))
}

func (p *HealthProcessor) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	if !p.started {
		p.mu.Unlock()
		return nil
	}
	if !p.stopping {
		p.stopping = true
		close(p.stop)
	}
	done := p.done
	p.mu.Unlock()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("drain LiteLLM health processor: %w", ctx.Err())
	}
}

func (p *HealthProcessor) Record(ctx context.Context, signal healthSignal, version string, observedErr error) {
	p.mu.Lock()
	if !p.started || p.stopping {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil || authCtx.APIKeyID == "" {
		return
	}
	apiKeyID, err := uuid.Parse(authCtx.APIKeyID)
	if err != nil {
		p.logger.WarnContext(ctx, "parse LiteLLM health API key ID",
			attr.SlogError(err),
			attr.SlogAPIKeyID(authCtx.APIKeyID),
		)
		return
	}

	version = strings.TrimSpace(version)
	if len(version) > maxReportedVersion {
		version = ""
	}
	observedAt := time.Now().UTC()
	update := healthUpdate{
		guardrailObservedAt: time.Time{},
		otelObservedAt:      time.Time{},
		errorObservedAt:     time.Time{},
		errorKind:           classifyHealthError(observedErr),
		reportedVersionAt:   time.Time{},
		reportedVersion:     version,
	}
	if signal == healthSignalGuardrail {
		update.guardrailObservedAt = observedAt
	}
	if signal == healthSignalOTEL {
		update.otelObservedAt = observedAt
	}
	if update.errorKind != healthErrorNone {
		update.errorObservedAt = observedAt
	}
	if update.reportedVersion != "" {
		update.reportedVersionAt = observedAt
	}
	key := healthInstanceKey{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      *authCtx.ProjectID,
		apiKeyID:       apiKeyID,
		instanceID:     uuid.Nil,
	}
	key.instanceID, _ = auth.LiteLLMInstanceIDFromAPIKeyName(authCtx.APIKeyName)

	p.mu.Lock()
	if p.stopping {
		p.mu.Unlock()
		return
	}
	current := p.pending[key]
	if update.guardrailObservedAt.After(current.guardrailObservedAt) {
		current.guardrailObservedAt = update.guardrailObservedAt
	}
	if update.otelObservedAt.After(current.otelObservedAt) {
		current.otelObservedAt = update.otelObservedAt
	}
	if update.errorKind != healthErrorNone && !update.errorObservedAt.Before(current.errorObservedAt) {
		current.errorObservedAt = update.errorObservedAt
		current.errorKind = update.errorKind
	}
	if update.reportedVersion != "" && !update.reportedVersionAt.Before(current.reportedVersionAt) {
		current.reportedVersionAt = update.reportedVersionAt
		current.reportedVersion = update.reportedVersion
	}
	p.pending[key] = current
	p.mu.Unlock()

	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func classifyHealthError(err error) healthErrorKind {
	if err == nil {
		return healthErrorNone
	}
	var shareable *oops.ShareableError
	if !errors.As(err, &shareable) {
		return healthErrorNone
	}
	switch shareable.Code {
	case oops.CodeUnauthorized, oops.CodeForbidden:
		return healthErrorAuthFailure
	case oops.CodeRequestTooLarge:
		return healthErrorLimitExceeded
	case oops.CodeBadRequest, oops.CodeUnsupportedMedia:
		return healthErrorDecode
	default:
		return healthErrorNone
	}
}

func (p *HealthProcessor) run(ctx context.Context) {
	defer close(p.done)
	for {
		select {
		case <-p.wake:
			timer := time.NewTimer(p.coalesceWindow)
			select {
			case <-timer.C:
				p.flush(ctx)
			case <-p.stop:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				p.flush(ctx)
				return
			}
		case <-p.stop:
			p.flush(ctx)
			return
		}
	}
}

func (p *HealthProcessor) flush(ctx context.Context) {
	p.mu.Lock()
	pending := p.pending
	p.pending = make(map[healthInstanceKey]healthUpdate)
	p.mu.Unlock()
	if len(pending) == 0 {
		return
	}

	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), healthWriteTimeout)
	defer cancel()
	for key, update := range pending {
		err := p.write(writeCtx, repo.RecordLiteLLMInstanceHealthParams{
			GuardrailObservedAt:       conv.PtrToPGTimestamptz(conv.PtrEmpty(update.guardrailObservedAt)),
			OtelObservedAt:            conv.PtrToPGTimestamptz(conv.PtrEmpty(update.otelObservedAt)),
			ErrorObservedAt:           conv.PtrToPGTimestamptz(conv.PtrEmpty(update.errorObservedAt)),
			ErrorKind:                 string(update.errorKind),
			ReportedVersionObservedAt: conv.PtrToPGTimestamptz(conv.PtrEmpty(update.reportedVersionAt)),
			ReportedLitellmVersion:    update.reportedVersion,
			OrganizationID:            key.organizationID,
			ProjectID:                 key.projectID,
			InstanceID:                uuid.NullUUID{UUID: key.instanceID, Valid: key.instanceID != uuid.Nil},
			ApiKeyID:                  key.apiKeyID,
		})
		if err != nil {
			p.logger.WarnContext(writeCtx, "record LiteLLM instance health",
				attr.SlogError(err),
				attr.SlogOrganizationID(key.organizationID),
				attr.SlogProjectID(key.projectID.String()),
				attr.SlogAPIKeyID(key.apiKeyID.String()),
			)
		}
	}
}
