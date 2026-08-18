package background

import (
	"time"

	"github.com/speakeasy-api/gram/server/internal/billingnotifications"
	tenvironment "github.com/speakeasy-api/gram/server/internal/temporal"
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
