package activities

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/assistants"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

type AdmitAssistantThreadsInput struct {
	AssistantID string
}

type AdmitAssistantThreadsResult struct {
	ThreadIDs []string
}

type ProcessAssistantThreadInput struct {
	ThreadID string
}

type ProcessAssistantThreadResult struct {
	AssistantID       string
	WarmUntil         string
	RuntimeActive     bool
	RetryAdmission    bool
	ProcessedAnyEvent bool
}

type ExpireAssistantThreadRuntimeInput struct {
	ThreadID string
}

type SignalAssistantCoordinatorInput struct {
	AssistantID string
}

type SignalAssistantThreadInput struct {
	ThreadID string
}

type AdmitAssistantThreads struct {
	core *assistants.ServiceCore
}

type ProcessAssistantThread struct {
	core *assistants.ServiceCore
}

type ExpireAssistantThreadRuntime struct {
	core *assistants.ServiceCore
}

type ReapStuckAssistantRuntimesResult struct {
	StaleRuntimesStopped int64
	StaleEventsRequeued  int64
	AffectedAssistantIDs []string
}

type ReapStuckAssistantRuntimes struct {
	core *assistants.ServiceCore
}

type SignalAssistantCoordinator struct {
	temporalEnv *tenv.Environment
}

type SignalAssistantThread struct {
	temporalEnv *tenv.Environment
}

func NewAdmitAssistantThreads(core *assistants.ServiceCore) *AdmitAssistantThreads {
	return &AdmitAssistantThreads{core: core}
}

func NewProcessAssistantThread(core *assistants.ServiceCore) *ProcessAssistantThread {
	return &ProcessAssistantThread{core: core}
}

func NewExpireAssistantThreadRuntime(core *assistants.ServiceCore) *ExpireAssistantThreadRuntime {
	return &ExpireAssistantThreadRuntime{core: core}
}

func NewReapStuckAssistantRuntimes(core *assistants.ServiceCore) *ReapStuckAssistantRuntimes {
	return &ReapStuckAssistantRuntimes{core: core}
}

func NewSignalAssistantCoordinator(temporalEnv *tenv.Environment) *SignalAssistantCoordinator {
	return &SignalAssistantCoordinator{temporalEnv: temporalEnv}
}

func NewSignalAssistantThread(temporalEnv *tenv.Environment) *SignalAssistantThread {
	return &SignalAssistantThread{temporalEnv: temporalEnv}
}

func (a *AdmitAssistantThreads) Do(ctx context.Context, input AdmitAssistantThreadsInput) (*AdmitAssistantThreadsResult, error) {
	if a.core == nil {
		return nil, fmt.Errorf("assistant service core is not configured")
	}
	assistantID, err := uuid.Parse(input.AssistantID)
	if err != nil {
		return nil, fmt.Errorf("parse assistant id: %w", err)
	}
	threadIDs, err := a.core.AdmitPendingThreads(ctx, assistantID)
	if err != nil {
		return nil, fmt.Errorf("admit assistant threads: %w", err)
	}
	result := &AdmitAssistantThreadsResult{ThreadIDs: make([]string, 0, len(threadIDs))}
	for _, threadID := range threadIDs {
		result.ThreadIDs = append(result.ThreadIDs, threadID.String())
	}
	return result, nil
}

func (a *ProcessAssistantThread) Do(ctx context.Context, input ProcessAssistantThreadInput) (*ProcessAssistantThreadResult, error) {
	if a.core == nil {
		return nil, fmt.Errorf("assistant service core is not configured")
	}
	threadID, err := uuid.Parse(input.ThreadID)
	if err != nil {
		return nil, fmt.Errorf("parse thread id: %w", err)
	}

	// Heartbeat periodically so a worker crash is detected within HeartbeatTimeout
	// instead of waiting the full 20-minute StartToCloseTimeout.
	hbCtx, hbCancel := context.WithCancel(ctx)
	defer hbCancel()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				activity.RecordHeartbeat(ctx)
			}
		}
	}()

	result, err := a.core.ProcessThreadEvents(ctx, threadID)
	if err != nil {
		return nil, fmt.Errorf("process assistant thread: %w", err)
	}
	out := &ProcessAssistantThreadResult{
		AssistantID:       result.AssistantID.String(),
		WarmUntil:         "",
		RuntimeActive:     result.RuntimeActive,
		RetryAdmission:    result.RetryAdmission,
		ProcessedAnyEvent: result.ProcessedAnyEvent,
	}
	if !result.WarmUntil.IsZero() {
		out.WarmUntil = result.WarmUntil.UTC().Format(time.RFC3339Nano)
	}
	return out, nil
}

func (a *ReapStuckAssistantRuntimes) Do(ctx context.Context) (*ReapStuckAssistantRuntimesResult, error) {
	if a.core == nil {
		return nil, fmt.Errorf("assistant service core is not configured")
	}
	result, err := a.core.ReapStuckRuntimes(ctx)
	if err != nil {
		return nil, fmt.Errorf("reap stuck assistant runtimes: %w", err)
	}
	ids := make([]string, 0, len(result.AffectedAssistantIDs))
	for _, id := range result.AffectedAssistantIDs {
		ids = append(ids, id.String())
	}
	return &ReapStuckAssistantRuntimesResult{
		StaleRuntimesStopped: result.StaleRuntimesStopped,
		StaleEventsRequeued:  result.StaleEventsRequeued,
		AffectedAssistantIDs: ids,
	}, nil
}

func (a *ExpireAssistantThreadRuntime) Do(ctx context.Context, input ExpireAssistantThreadRuntimeInput) error {
	if a.core == nil {
		return fmt.Errorf("assistant service core is not configured")
	}
	threadID, err := uuid.Parse(input.ThreadID)
	if err != nil {
		return fmt.Errorf("parse thread id: %w", err)
	}
	if err := a.core.ExpireThreadRuntime(ctx, threadID); err != nil {
		return fmt.Errorf("expire assistant thread runtime: %w", err)
	}
	return nil
}

func (a *SignalAssistantCoordinator) Do(ctx context.Context, input SignalAssistantCoordinatorInput) error {
	assistantID, err := uuid.Parse(input.AssistantID)
	if err != nil {
		return fmt.Errorf("parse assistant id: %w", err)
	}
	if err := assistants.SignalAssistantCoordinator(ctx, a.temporalEnv, assistantID); err != nil {
		return fmt.Errorf("signal assistant coordinator: %w", err)
	}
	return nil
}

func (a *SignalAssistantThread) Do(ctx context.Context, input SignalAssistantThreadInput) error {
	threadID, err := uuid.Parse(input.ThreadID)
	if err != nil {
		return fmt.Errorf("parse thread id: %w", err)
	}
	if err := assistants.SignalAssistantThread(ctx, a.temporalEnv, threadID); err != nil {
		return fmt.Errorf("signal assistant thread: %w", err)
	}
	return nil
}
