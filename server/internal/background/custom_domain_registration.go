package background

import (
	"context"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type CustomDomainRegistrationParams struct {
	OrgID  string
	Domain string
}

type CustomDomainDeletionParams struct {
	OrgID          string
	Domain         string
	IngressName    string
	CertSecretName string
}

type CustomDomainRegistrationClient struct {
	TemporalEnv *tenv.Environment
}

func (c *CustomDomainRegistrationClient) GetWorkflowInfo(ctx context.Context, orgID string, domain string) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
	id := c.GetID(orgID, domain)
	info, err := c.TemporalEnv.Client().DescribeWorkflowExecution(ctx, id, "")
	if err != nil {
		return nil, fmt.Errorf("describe workflow execution: %w", err)
	}

	return info, nil
}

func (c *CustomDomainRegistrationClient) GetID(orgID string, domain string) string {
	return fmt.Sprintf("v1:custom-domain-registration:%s:%s", orgID, domain)
}

func (c *CustomDomainRegistrationClient) GetDeletionID(orgID string, domain string) string {
	return fmt.Sprintf("v1:custom-domain-deletion:%s:%s", orgID, domain)
}

func (c *CustomDomainRegistrationClient) ExecuteCustomDomainDeletion(ctx context.Context, orgID, domain, ingressName, certSecretName string) (client.WorkflowRun, error) {
	id := c.GetDeletionID(orgID, domain)
	return c.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    id,
		TaskQueue:             string(c.TemporalEnv.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowRunTimeout:    5 * time.Minute,
	}, CustomDomainDeletionWorkflow, CustomDomainDeletionParams{
		OrgID:          orgID,
		Domain:         domain,
		IngressName:    ingressName,
		CertSecretName: certSecretName,
	})
}

func (c *CustomDomainRegistrationClient) ExecuteCustomDomainRegistration(ctx context.Context, orgID string, domain string) (client.WorkflowRun, error) {
	id := c.GetID(orgID, domain)
	return c.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    id,
		TaskQueue:             string(c.TemporalEnv.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowRunTimeout:    5 * time.Minute,
	}, CustomDomainRegistrationWorkflow, CustomDomainRegistrationParams{
		OrgID:  orgID,
		Domain: domain,
	})
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
		activities.CustomDomainIngressArgs{OrgID: params.OrgID, Domain: params.Domain, Action: activities.CustomDomainIngressActionSetup, IngressName: "", CertSecretName: ""},
	).Get(ingressCreateCtx, nil)
	if err != nil {
		logger.Error("failed to create custom domain ingress", "error", err.Error(), "org_id", params.OrgID, "domain", params.Domain)
		return fmt.Errorf("failed to create custom domain ingress: %w", err)
	}

	return nil
}

func CustomDomainDeletionWorkflow(ctx workflow.Context, params CustomDomainDeletionParams) error {
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
		a.CustomDomainIngress,
		activities.CustomDomainIngressArgs{
			OrgID:          params.OrgID,
			Domain:         params.Domain,
			Action:         activities.CustomDomainIngressActionDelete,
			IngressName:    params.IngressName,
			CertSecretName: params.CertSecretName,
		},
	).Get(ctx, nil)
	if err != nil {
		logger.Error("failed to delete custom domain ingress", "error", err.Error(), "org_id", params.OrgID, "domain", params.Domain)
		return fmt.Errorf("failed to delete custom domain ingress: %w", err)
	}

	return nil
}
