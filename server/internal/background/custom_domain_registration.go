package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	customDomainReconcileWorkflowRunTimeout            = 15 * time.Minute
	customDomainRegistrationWorkflowRunTimeout         = 30 * time.Minute
	signalCustomDomainReconcileStartToCloseTimeout     = 12 * time.Minute
	signalCustomDomainReconcileActivityMaximumAttempts = 2
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

type CustomDomainReconcileParams struct {
	CustomDomainID uuid.UUID
}

type CustomDomainReconcileResult struct{}

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

func CustomDomainReconcileWorkflowID(customDomainID uuid.UUID) string {
	return fmt.Sprintf("v1:custom-domain-reconcile:%s", customDomainID.String())
}

func customDomainReconcileSignal(params CustomDomainReconcileParams) string {
	return CustomDomainReconcileWorkflowID(params.CustomDomainID) + "/signal"
}

// ExecuteCustomDomainReconcile signals the domain-scoped desired-state
// workflow, starting it when no run is active. Signals arriving during Apply
// cause a follow-up pass against the latest committed database state.
func (c *CustomDomainRegistrationClient) ExecuteCustomDomainReconcile(ctx context.Context, customDomainID uuid.UUID) (client.WorkflowRun, error) {
	params := CustomDomainReconcileParams{CustomDomainID: customDomainID}
	id := CustomDomainReconcileWorkflowID(customDomainID)
	signalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	run, err := c.TemporalEnv.Client().SignalWithStartWorkflow(
		signalCtx,
		id,
		customDomainReconcileSignal(params),
		"reconcile",
		client.StartWorkflowOptions{
			ID:                       id,
			TaskQueue:                string(c.TemporalEnv.Queue()),
			WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
			WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
			WorkflowRunTimeout:       customDomainReconcileWorkflowRunTimeout,
		},
		CustomDomainReconcileWorkflow,
		params,
	)
	if err != nil {
		return nil, fmt.Errorf("signal with start custom domain reconcile workflow: %w", err)
	}
	return run, nil
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
		WorkflowRunTimeout:    customDomainRegistrationWorkflowRunTimeout,
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
		if activities.IsDNSNotFound(err) {
			logger.Info("custom domain DNS not found, skipping verification retries", "error", err.Error(), "org_id", params.OrgID, "domain", params.Domain)
			return asBenignWorkflowError(err)
		}
		logger.Error("failed to verify custom domain", "error", err.Error(), "org_id", params.OrgID, "domain", params.Domain)
		return fmt.Errorf("failed to verify custom domain: %w", err)
	}

	const reconcileWorkflowVersion = 1
	version := workflow.GetVersion(ctx, "custom-domain-reconcile-workflow", workflow.DefaultVersion, reconcileWorkflowVersion)
	if version == workflow.DefaultVersion {
		// Preserve replay compatibility for registration workflows started
		// before domain-scoped reconciliation was introduced.
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
				IPAllowlist:     nil,
			},
		).Get(ingressCreateCtx, nil)
	} else {
		reconcileCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: signalCustomDomainReconcileStartToCloseTimeout,
			RetryPolicy: &temporal.RetryPolicy{
				MaximumAttempts: signalCustomDomainReconcileActivityMaximumAttempts,
			},
		})
		err = workflow.ExecuteActivity(reconcileCtx, a.SignalCustomDomainReconcile, SignalCustomDomainReconcileArgs{
			OrgID:  params.OrgID,
			Domain: params.Domain,
		}).Get(reconcileCtx, nil)
	}
	if err != nil {
		logger.Error("failed to reconcile custom domain route", "error", err.Error(), "org_id", params.OrgID, "domain", params.Domain)
		return fmt.Errorf("failed to reconcile custom domain route: %w", err)
	}

	return nil
}

type SignalCustomDomainReconcileArgs struct {
	OrgID  string
	Domain string
}

// SignalCustomDomainReconcile bridges the registration workflow—whose stable
// identifier predates the custom-domain row—to the UUID-scoped reconcile
// workflow after verification has committed that row.
func (a *Activities) SignalCustomDomainReconcile(ctx context.Context, args SignalCustomDomainReconcileArgs) error {
	domain, err := customdomainsrepo.New(a.db).GetCustomDomainByDomain(ctx, args.Domain)
	if err != nil {
		return fmt.Errorf("load custom domain for reconciliation: %w", err)
	}
	if domain.OrganizationID != args.OrgID {
		return fmt.Errorf("custom domain does not belong to organization")
	}

	run, err := (&CustomDomainRegistrationClient{TemporalEnv: a.temporalEnv}).ExecuteCustomDomainReconcile(ctx, domain.ID)
	if err != nil {
		return err
	}
	if err := waitForCurrentCustomDomainReconcileRun(ctx, run); err != nil {
		return fmt.Errorf("custom domain reconcile workflow: %w", err)
	}
	return nil
}

func waitForCurrentCustomDomainReconcileRun(ctx context.Context, run client.WorkflowRun) error {
	err := run.GetWithOptions(ctx, nil, client.WorkflowRunGetOptions{DisableFollowingRuns: true})
	if err == nil {
		return nil
	}
	var continueAsNewErr *workflow.ContinueAsNewError
	if errors.As(err, &continueAsNewErr) {
		return nil
	}
	return fmt.Errorf("get current custom domain reconcile run: %w", err)
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

// CustomDomainReconcileWorkflow coalesces every signal received during an
// Apply into another pass. Continue-as-new keeps the stable workflow ID while
// bounding history growth; WorkflowRun.Get follows the run chain.
func CustomDomainReconcileWorkflow(ctx workflow.Context, params CustomDomainReconcileParams) (CustomDomainReconcileResult, error) {
	return Debounce(
		customDomainReconcilePass,
		CustomDomainReconcileWorkflow,
		customDomainReconcileSignal,
		func(CustomDomainReconcileParams, CustomDomainReconcileResult) bool { return false },
	)(ctx, params)
}

func customDomainReconcilePass(ctx workflow.Context, params CustomDomainReconcileParams) (CustomDomainReconcileResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 3 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    10 * time.Second,
		},
	})

	var a *Activities
	if err := workflow.ExecuteActivity(ctx, a.ReconcileCustomDomain, activities.ReconcileCustomDomainArgs{
		CustomDomainID: params.CustomDomainID,
	}).Get(ctx, nil); err != nil {
		return CustomDomainReconcileResult{}, fmt.Errorf("reconcile custom domain: %w", err)
	}
	// Yield one workflow task after Apply. A signal accepted alongside the
	// activity-completion event is then visible to Debounce's final drain
	// instead of racing the workflow-completion command.
	if err := workflow.Sleep(ctx, time.Millisecond); err != nil {
		return CustomDomainReconcileResult{}, fmt.Errorf("yield after custom domain reconcile: %w", err)
	}
	return CustomDomainReconcileResult{}, nil
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
