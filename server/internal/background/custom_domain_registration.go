package background

import (
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/internal/background/activities"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type CustomDomainRegistrationParams struct {
	OrgID  string
	Domain string
}

func CustomDomainRegistrationWorkflow(ctx workflow.Context, params CustomDomainRegistrationParams) error {
	logger := workflow.GetLogger(ctx)
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 60 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	})

	var a *Activities
	err := workflow.ExecuteActivity(
		ctx,
		a.VerifyCustomDomain,
		activities.VerifyCustomDomainArgs{OrgID: params.OrgID, Domain: params.Domain},
	).Get(ctx, nil)
	if err != nil {
		logger.Error("failed to verify custom domain", "error", err.Error(), "org_id", params.OrgID, "domain", params.Domain)
		return fmt.Errorf("failed to verify custom domain: %w", err)
	}

	ingressCreateCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 180 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1,
		},
	})

	err = workflow.ExecuteActivity(
		ingressCreateCtx,
		a.CustomDomainIngress,
		activities.CustomDomainIngressArgs{OrgID: params.OrgID, Domain: params.Domain, Action: activities.CustomDomainIngressActionSetup},
	).Get(ingressCreateCtx, nil)
	if err != nil {
		logger.Error("failed to create custom domain ingress", "error", err.Error(), "org_id", params.OrgID, "domain", params.Domain)
		return fmt.Errorf("failed to create custom domain ingress: %w", err)
	}

	return nil
}
