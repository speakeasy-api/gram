package activities

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/assistants"
)

type AdmitAssistantThreadsInput struct {
	AssistantID string
}

type AdmitAssistantThreadsResult struct {
	ThreadIDs []string
	ProjectID string
}

type ProcessAssistantThreadInput struct {
	ThreadID  string
	ProjectID string
}

type ProcessAssistantThreadResult struct {
	AssistantID       string
	WarmUntil         string
	WarmTTLSeconds    int
	RuntimeActive     bool
	RetryAdmission    bool
	ProcessedAnyEvent bool
}

type ExpireAssistantThreadRuntimeInput struct {
	ThreadID       string
	ProjectID      string
	WarmTTLSeconds int
}

// ExpireAssistantThreadRuntimeResult reports the outcome of an expire attempt.
// Stopped=false + RemainingSeconds means a turn slipped in past the warm
// timer; the workflow should re-arm with that window and try again.
type ExpireAssistantThreadRuntimeResult struct {
	Stopped          bool
	RemainingSeconds int
}

type SignalAssistantCoordinatorInput struct {
	AssistantID string
}

type SignalAssistantThreadInput struct {
	ThreadID  string
	ProjectID string
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
	signaler assistants.WorkflowSignaler
}

type SignalAssistantThread struct {
	signaler assistants.WorkflowSignaler
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

func NewSignalAssistantCoordinator(signaler assistants.WorkflowSignaler) *SignalAssistantCoordinator {
	return &SignalAssistantCoordinator{signaler: signaler}
}

func NewSignalAssistantThread(signaler assistants.WorkflowSignaler) *SignalAssistantThread {
	return &SignalAssistantThread{signaler: signaler}
}

func (a *AdmitAssistantThreads) Do(ctx context.Context, input AdmitAssistantThreadsInput) (*AdmitAssistantThreadsResult, error) {
	assistantID, err := uuid.Parse(input.AssistantID)
	if err != nil {
		return nil, fmt.Errorf("parse assistant id: %w", err)
	}
	admitted, err := a.core.AdmitPendingThreads(ctx, assistantID)
	if err != nil {
		return nil, fmt.Errorf("admit assistant threads: %w", err)
	}
	result := &AdmitAssistantThreadsResult{
		ProjectID: admitted.ProjectID.String(),
		ThreadIDs: make([]string, 0, len(admitted.ThreadIDs)),
	}
	for _, threadID := range admitted.ThreadIDs {
		result.ThreadIDs = append(result.ThreadIDs, threadID.String())
	}
	return result, nil
}

func (a *ProcessAssistantThread) Do(ctx context.Context, input ProcessAssistantThreadInput) (*ProcessAssistantThreadResult, error) {
	threadID, err := uuid.Parse(input.ThreadID)
	if err != nil {
		return nil, fmt.Errorf("parse thread id: %w", err)
	}
	projectID, err := uuid.Parse(input.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("parse project id: %w", err)
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

	result, err := a.core.ProcessThreadEventsByThreadID(ctx, projectID, threadID)
	if err != nil {
		return nil, fmt.Errorf("process assistant thread: %w", err)
	}
	out := &ProcessAssistantThreadResult{
		AssistantID:       result.AssistantID.String(),
		WarmUntil:         "",
		WarmTTLSeconds:    result.WarmTTLSeconds,
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

func (a *ExpireAssistantThreadRuntime) Do(ctx context.Context, input ExpireAssistantThreadRuntimeInput) (*ExpireAssistantThreadRuntimeResult, error) {
	threadID, err := uuid.Parse(input.ThreadID)
	if err != nil {
		return nil, fmt.Errorf("parse thread id: %w", err)
	}
	projectID, err := uuid.Parse(input.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("parse project id: %w", err)
	}
	result, err := a.core.ExpireThreadRuntime(ctx, projectID, threadID, input.WarmTTLSeconds)
	if err != nil {
		return nil, fmt.Errorf("expire assistant thread runtime: %w", err)
	}
	return &ExpireAssistantThreadRuntimeResult{
		Stopped:          result.Stopped,
		RemainingSeconds: result.RemainingSeconds,
	}, nil
}

func (a *SignalAssistantCoordinator) Do(ctx context.Context, input SignalAssistantCoordinatorInput) error {
	assistantID, err := uuid.Parse(input.AssistantID)
	if err != nil {
		return fmt.Errorf("parse assistant id: %w", err)
	}
	if err := a.signaler.SignalCoordinator(ctx, assistantID); err != nil {
		return fmt.Errorf("signal assistant coordinator: %w", err)
	}
	return nil
}

func (a *SignalAssistantThread) Do(ctx context.Context, input SignalAssistantThreadInput) error {
	threadID, err := uuid.Parse(input.ThreadID)
	if err != nil {
		return fmt.Errorf("parse thread id: %w", err)
	}
	projectID, err := uuid.Parse(input.ProjectID)
	if err != nil {
		return fmt.Errorf("parse project id: %w", err)
	}
	if err := a.signaler.SignalThread(ctx, threadID, projectID); err != nil {
		return fmt.Errorf("signal assistant thread: %w", err)
	}
	return nil
}
