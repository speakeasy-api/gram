package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/billingnotifications"
	tenvironment "github.com/speakeasy-api/gram/server/internal/temporal"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
)

const (
	billingEmailWorkflowRunTimeout            = 7 * 24 * time.Hour
	billingEmailRetryInitialInterval          = 5 * time.Second
	billingEmailRetryMaximumInterval          = time.Minute
	billingEmailRetryBackoffCoefficient       = 2
	billingEmailRetryMaximumAttempts    int32 = 12
)

func billingEmailRetryPolicy() *temporal.RetryPolicy {
	return &temporal.RetryPolicy{
		InitialInterval:    billingEmailRetryInitialInterval,
		MaximumInterval:    billingEmailRetryMaximumInterval,
		BackoffCoefficient: billingEmailRetryBackoffCoefficient,
		MaximumAttempts:    billingEmailRetryMaximumAttempts,
	}
}

type TemporalBillingEmailScheduler struct {
	TemporalEnv *tenvironment.Environment
}

var _ billingnotifications.BillingEmailScheduler = (*TemporalBillingEmailScheduler)(nil)

// enqueue starts one billing email workflow per durable event. The workflow id
// carries the event id, so a redelivered event never sends a second email.
func (s *TemporalBillingEmailScheduler) enqueue(ctx context.Context, workflowIDPrefix, eventID string, workflowFunc any, input any) error {
	if s == nil || s.TemporalEnv == nil {
		return fmt.Errorf("temporal environment is not configured")
	}
	_, err := s.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                                       fmt.Sprintf("%s:%s", workflowIDPrefix, eventID),
		TaskQueue:                                string(s.TemporalEnv.Queue()),
		WorkflowIDReusePolicy:                    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
		WorkflowExecutionErrorWhenAlreadyStarted: true,
		WorkflowRunTimeout:                       billingEmailWorkflowRunTimeout,
	}, workflowFunc, input)
	if err != nil {
		if _, ok := errors.AsType[*serviceerror.WorkflowExecutionAlreadyStarted](err); ok {
			return nil
		}
		return err
	}
	return nil
}
