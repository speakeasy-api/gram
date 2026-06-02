package background

import (
	"context"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type CustomDomainRegistrationParams struct {
	OrgID           string
	Domain          string
	CreatedBy       urn.Principal
	CreatedByName   *string
	ProvisionerKind k8s.ProvisionerKind
	IPAllowlist     []string
}

type CustomDomainDeletionParams struct {
	OrgID           string
	Domain          string
	IngressName     string
	CertSecretName  string
	ProvisionerKind k8s.ProvisionerKind
}

type CustomDomainUpdateParams struct {
	OrgID           string
	Domain          string
	ProvisionerKind k8s.ProvisionerKind
	IPAllowlist     []string
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

func (c *CustomDomainRegistrationClient) GetUpdateID(orgID string, domain string) string {
	return fmt.Sprintf("v1:custom-domain-update:%s:%s", orgID, domain)
}

// ExecuteCustomDomainUpdate re-applies the persisted IP allowlist to an
// already-provisioned custom domain. Used by the edit flow.
func (c *CustomDomainRegistrationClient) ExecuteCustomDomainUpdate(ctx context.Context, orgID, domain string, provisionerKind k8s.ProvisionerKind, ipAllowlist []string) (client.WorkflowRun, error) {
	id := c.GetUpdateID(orgID, domain)
	return c.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    id,
		TaskQueue:             string(c.TemporalEnv.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowRunTimeout:    5 * time.Minute,
	}, CustomDomainUpdateWorkflow, CustomDomainUpdateParams{
		OrgID:           orgID,
		Domain:          domain,
		ProvisionerKind: provisionerKind,
		IPAllowlist:     ipAllowlist,
	})
}

func (c *CustomDomainRegistrationClient) ExecuteCustomDomainDeletion(ctx context.Context, orgID, domain, ingressName, certSecretName string, provisionerKind k8s.ProvisionerKind) (client.WorkflowRun, error) {
	id := c.GetDeletionID(orgID, domain)
	return c.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    id,
		TaskQueue:             string(c.TemporalEnv.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowRunTimeout:    5 * time.Minute,
	}, CustomDomainDeletionWorkflow, CustomDomainDeletionParams{
		OrgID:           orgID,
		Domain:          domain,
		IngressName:     ingressName,
		CertSecretName:  certSecretName,
		ProvisionerKind: provisionerKind,
	})
}

func (c *CustomDomainRegistrationClient) ExecuteCustomDomainRegistration(ctx context.Context, orgID string, domain string, createdBy urn.Principal, createdByName *string, provisionerKind k8s.ProvisionerKind, ipAllowlist []string) (client.WorkflowRun, error) {
	id := c.GetID(orgID, domain)
	return c.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    id,
		TaskQueue:             string(c.TemporalEnv.Queue()),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowRunTimeout:    5 * time.Minute,
	}, CustomDomainRegistrationWorkflow, CustomDomainRegistrationParams{
		OrgID:           orgID,
		Domain:          domain,
		CreatedBy:       createdBy,
		CreatedByName:   createdByName,
		ProvisionerKind: provisionerKind,
		IPAllowlist:     ipAllowlist,
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
		activities.VerifyCustomDomainArgs{
			OrgID:           params.OrgID,
			Domain:          params.Domain,
			CreatedBy:       params.CreatedBy,
			CreatedByName:   params.CreatedByName,
			ProvisionerKind: params.ProvisionerKind,
			IPAllowlist:     params.IPAllowlist,
		},
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
		activities.CustomDomainIngressArgs{
			OrgID:           params.OrgID,
			Domain:          params.Domain,
			Action:          activities.CustomDomainIngressActionSetup,
			IngressName:     "",
			ResourceName:    "",
			CertSecretName:  "",
			ProvisionerKind: params.ProvisionerKind,
			IPAllowlist:     nil, // Setup reads the persisted allowlist from the DB.
		},
	).Get(ingressCreateCtx, nil)
	if err != nil {
		logger.Error("failed to create custom domain ingress", "error", err.Error(), "org_id", params.OrgID, "domain", params.Domain)
		return fmt.Errorf("failed to create custom domain ingress: %w", err)
	}

	return nil
}

func CustomDomainUpdateWorkflow(ctx workflow.Context, params CustomDomainUpdateParams) error {
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
			OrgID:           params.OrgID,
			Domain:          params.Domain,
			Action:          activities.CustomDomainIngressActionReapply,
			IngressName:     "",
			ResourceName:    "",
			CertSecretName:  "",
			ProvisionerKind: params.ProvisionerKind,
			IPAllowlist:     params.IPAllowlist,
		},
	).Get(ctx, nil)
	if err != nil {
		logger.Error("failed to re-apply custom domain ip allowlist", "error", err.Error(), "org_id", params.OrgID, "domain", params.Domain)
		return fmt.Errorf("failed to re-apply custom domain ip allowlist: %w", err)
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
			OrgID:           params.OrgID,
			Domain:          params.Domain,
			Action:          activities.CustomDomainIngressActionDelete,
			IngressName:     params.IngressName,
			ResourceName:    "",
			CertSecretName:  params.CertSecretName,
			ProvisionerKind: params.ProvisionerKind,
			IPAllowlist:     nil, // Unused by Delete.
		},
	).Get(ctx, nil)
	if err != nil {
		logger.Error("failed to delete custom domain ingress", "error", err.Error(), "org_id", params.OrgID, "domain", params.Domain)
		return fmt.Errorf("failed to delete custom domain ingress: %w", err)
	}

	return nil
}
