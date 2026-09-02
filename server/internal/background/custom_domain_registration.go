package background

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/oops"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	customDomainReconcileWorkflowRunTimeout            = 15 * time.Minute
	signalCustomDomainReconcileStartToCloseTimeout     = 12 * time.Minute
	signalCustomDomainReconcileActivityMaximumAttempts = 2

	// DNS propagation for customer domains can take hours (some providers ship
	// record changes through code review and staged rollouts). Verification
	// polls until the deadline; the run timeout leaves room for reconciliation
	// after a last-minute success.
	customDomainVerificationDeadline           = 24 * time.Hour
	customDomainRegistrationWorkflowRunTimeout = 26 * time.Hour

	// Verification poll backoff bounds. A reverify signal (dashboard button or
	// re-registration) wakes the loop immediately.
	customDomainVerifyInitialInterval = 30 * time.Second
	customDomainVerifyMaxInterval     = 5 * time.Minute

	// CustomDomainReverifySignalName wakes a pending registration workflow for
	// an immediate re-check instead of waiting out the current poll interval.
	CustomDomainReverifySignalName = "reverify"
)

type CustomDomainRegistrationParams struct {
	OrgID  string
	Domain string
	// CustomDomainID pins the workflow to the row created at registration
	// time, so a delete + re-register of the same hostname cannot be verified
	// by a stale workflow. Zero on workflows started before this field.
	CustomDomainID  uuid.UUID
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

// GetWorkflowInfo reports the row-scoped registration workflow when one
// exists, falling back to the legacy hostname-scoped identifier so runs
// started before the v2 IDs still surface as in-progress.
func (c *CustomDomainRegistrationClient) GetWorkflowInfo(ctx context.Context, orgID string, domain string, customDomainID uuid.UUID) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
	if customDomainID != uuid.Nil {
		info, err := c.TemporalEnv.Client().DescribeWorkflowExecution(ctx, c.GetRegistrationID(customDomainID), "")
		if err == nil {
			if info.GetWorkflowExecutionInfo().GetStatus() == enums.WORKFLOW_EXECUTION_STATUS_RUNNING {
				return info, nil
			}
		} else {
			if _, ok := errors.AsType[*serviceerror.NotFound](err); !ok {
				return nil, fmt.Errorf("describe workflow execution: %w", err)
			}
		}
	}

	info, err := c.TemporalEnv.Client().DescribeWorkflowExecution(ctx, c.GetLegacyID(orgID, domain), "")
	if err != nil {
		return nil, fmt.Errorf("describe workflow execution: %w", err)
	}

	return info, nil
}

// GetRegistrationID scopes the registration workflow to the row it was
// started for, so hostname reuse (delete + re-register) can never signal or
// terminate a successor row's workflow.
func (c *CustomDomainRegistrationClient) GetRegistrationID(customDomainID uuid.UUID) string {
	return fmt.Sprintf("v2:custom-domain-registration:%s", customDomainID.String())
}

// GetLegacyID is the pre-v2, hostname-scoped registration workflow identifier;
// retained to observe and stop runs started before the cutover.
func (c *CustomDomainRegistrationClient) GetLegacyID(orgID string, domain string) string {
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

// ExecuteCustomDomainRegistration starts the row-scoped registration
// workflow, or wakes the already-running one with a reverify signal so the
// dashboard's "Reverify" re-checks immediately instead of erroring against the
// stable workflow ID during a long DNS-propagation wait. The start is
// decoupled from request cancellation: the caller has already committed the
// row, and a client hangup must not strand it without a workflow.
func (c *CustomDomainRegistrationClient) ExecuteCustomDomainRegistration(ctx context.Context, orgID string, domain string, customDomainID uuid.UUID, createdBy urn.Principal, createdByName *string, provisionerKind k8s.ProvisionerKind, ipAllowlist []string) (client.WorkflowRun, error) {
	startCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	id := c.GetRegistrationID(customDomainID)
	run, err := c.TemporalEnv.Client().SignalWithStartWorkflow(startCtx, id, CustomDomainReverifySignalName, nil, client.StartWorkflowOptions{
		ID:                       id,
		TaskQueue:                string(c.TemporalEnv.Queue()),
		WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
		WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		WorkflowRunTimeout:       customDomainRegistrationWorkflowRunTimeout,
	}, CustomDomainRegistrationWorkflow, CustomDomainRegistrationParams{
		OrgID:           orgID,
		Domain:          domain,
		CustomDomainID:  customDomainID,
		CreatedBy:       createdBy,
		CreatedByName:   createdByName,
		ProvisionerKind: provisionerKind,
		IPAllowlist:     ipAllowlist,
	})
	if err != nil {
		return nil, fmt.Errorf("signal with start custom domain registration workflow: %w", err)
	}
	return run, nil
}

// TerminateCustomDomainRegistration best-effort stops the given row's pending
// registration workflow along with any legacy hostname-scoped run. The row
// scoping means a late terminate after delete can never hit a successor row's
// workflow — that successor runs under its own row ID.
func (c *CustomDomainRegistrationClient) TerminateCustomDomainRegistration(ctx context.Context, orgID string, domain string, customDomainID uuid.UUID, reason string) error {
	terminateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	ids := make([]string, 0, 2)
	if customDomainID != uuid.Nil {
		ids = append(ids, c.GetRegistrationID(customDomainID))
	}
	ids = append(ids, c.GetLegacyID(orgID, domain))
	var errs []error
	for _, id := range ids {
		err := c.TemporalEnv.Client().TerminateWorkflow(terminateCtx, id, "", reason)
		if err != nil {
			if _, ok := errors.AsType[*serviceerror.NotFound](err); ok {
				continue
			}
			errs = append(errs, fmt.Errorf("terminate custom domain registration workflow %s: %w", id, err))
		}
	}
	return errors.Join(errs...)
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
	verifyArgs := activities.VerifyCustomDomainArgs{
		OrgID:           params.OrgID,
		Domain:          params.Domain,
		CustomDomainID:  params.CustomDomainID,
		CreatedBy:       params.CreatedBy,
		CreatedByName:   params.CreatedByName,
		ProvisionerKind: params.ProvisionerKind,
		IPAllowlist:     params.IPAllowlist,
	}

	const verifyLoopVersion = 1
	if workflow.GetVersion(ctx, "custom-domain-verify-loop", workflow.DefaultVersion, verifyLoopVersion) == workflow.DefaultVersion {
		// Replay compatibility: single-shot verification for workflows started
		// before slow DNS propagation was tolerated.
		err := workflow.ExecuteActivity(ctx, a.VerifyCustomDomain, verifyArgs).Get(ctx, nil)
		if err != nil {
			logger.Error("failed to verify custom domain", "error", err.Error(), "org_id", params.OrgID, "domain", params.Domain)
			return fmt.Errorf("failed to verify custom domain: %w", err)
		}
	} else if err := verifyCustomDomainUntilOwned(ctx, verifyArgs); err != nil {
		logger.Error("failed to verify custom domain", "error", err.Error(), "org_id", params.OrgID, "domain", params.Domain)
		return fmt.Errorf("failed to verify custom domain: %w", err)
	}

	var err error
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
			OrgID:          params.OrgID,
			Domain:         params.Domain,
			CustomDomainID: params.CustomDomainID,
		}).Get(reconcileCtx, nil)
	}
	if err != nil {
		logger.Error("failed to reconcile custom domain route", "error", err.Error(), "org_id", params.OrgID, "domain", params.Domain)
		return fmt.Errorf("failed to reconcile custom domain route: %w", err)
	}

	return nil
}

// verifyCustomDomainUntilOwned polls ownership verification until it succeeds,
// fails terminally, or the propagation deadline lapses. Between pending
// results it sleeps with capped backoff; a reverify signal wakes it for an
// immediate re-check. DNS record changes can take hours to ship at some
// providers, so patience here is the product behavior, not a fallback: a
// transient infrastructure failure that exhausts one pass's activity retries
// counts as another pending tick rather than killing the day-long wait — only
// non-retryable failures (invalid domain, vanished row) end the workflow.
func verifyCustomDomainUntilOwned(ctx workflow.Context, verifyArgs activities.VerifyCustomDomainArgs) error {
	logger := workflow.GetLogger(ctx)
	var a *Activities
	reverify := workflow.GetSignalChannel(ctx, CustomDomainReverifySignalName)
	deadline := workflow.Now(ctx).Add(customDomainVerificationDeadline)
	interval := customDomainVerifyInitialInterval

	for {
		// Collapse queued wake-ups so a burst causes one re-check.
		for reverify.ReceiveAsync(nil) {
		}

		var result activities.VerifyCustomDomainResult
		lastReason := ""
		if err := workflow.ExecuteActivity(ctx, a.VerifyCustomDomainV2, verifyArgs).Get(ctx, &result); err != nil {
			var appErr *temporal.ApplicationError
			if errors.As(err, &appErr) && appErr.NonRetryable() {
				return err
			}
			logger.Warn("custom domain verification pass failed, will retry", "error", err.Error(), "domain", verifyArgs.Domain)
			lastReason = "verification check failed"
		} else {
			switch result.Status {
			case activities.VerifyStatusVerified:
				return nil
			case activities.VerifyStatusDNSPending:
				lastReason = result.Reason
			default:
				return temporal.NewNonRetryableApplicationError(
					fmt.Sprintf("unexpected custom domain verification status %q", result.Status),
					"CustomDomainVerificationContract",
					nil,
				)
			}
		}
		remaining := deadline.Sub(workflow.Now(ctx))
		if remaining <= 0 {
			// A reverify that raced the deadline earns one more pass instead
			// of being acknowledged and then dropped with the closing run.
			if reverify.ReceiveAsync(nil) {
				remaining = customDomainVerifyInitialInterval
				deadline = workflow.Now(ctx).Add(remaining)
			} else {
				return temporal.NewNonRetryableApplicationError(
					fmt.Sprintf("custom domain verification timed out waiting for DNS: %s", lastReason),
					"CustomDomainVerificationTimeout",
					nil,
				)
			}
		}

		timerCtx, cancelTimer := workflow.WithCancel(ctx)
		selector := workflow.NewSelector(ctx)
		selector.AddFuture(workflow.NewTimer(timerCtx, min(interval, remaining)), func(workflow.Future) {})
		selector.AddReceive(reverify, func(c workflow.ReceiveChannel, _ bool) {
			c.Receive(ctx, nil)
		})
		selector.Select(ctx)
		cancelTimer()

		interval = min(interval*2, customDomainVerifyMaxInterval)
	}
}

type SignalCustomDomainReconcileArgs struct {
	OrgID  string
	Domain string
	// CustomDomainID pins reconciliation to the row this registration was
	// started for; zero on legacy workflows, which fall back to hostname
	// lookup.
	CustomDomainID uuid.UUID
}

// SignalCustomDomainReconcile bridges the registration workflow to the
// UUID-scoped reconcile workflow after verification succeeds. It resolves the
// row by the ID the registration was started for, so a delete + re-register
// of the same hostname can never route a stale workflow's reconcile at the
// successor row; legacy workflows without an ID fall back to hostname lookup.
func (a *Activities) SignalCustomDomainReconcile(ctx context.Context, args SignalCustomDomainReconcileArgs) error {
	var domain customdomainsrepo.CustomDomain
	var err error
	if args.CustomDomainID != uuid.Nil {
		domain, err = customdomainsrepo.New(a.db).GetCustomDomainByID(ctx, args.CustomDomainID)
	} else {
		domain, err = customdomainsrepo.New(a.db).GetCustomDomainByDomain(ctx, args.Domain)
	}
	if err != nil {
		return fmt.Errorf("load custom domain for reconciliation: %w", err)
	}
	if domain.OrganizationID != args.OrgID || domain.Domain != args.Domain {
		return fmt.Errorf("custom domain does not match this registration")
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
	if _, ok := errors.AsType[*workflow.ContinueAsNewError](err); ok {
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

// ReconcileCustomDomains starts a reconcile per domain; call only after the
// transaction that changed their roots committed.
func ReconcileCustomDomains(ctx context.Context, logger *slog.Logger, env *tenv.Environment, customDomainIDs []uuid.UUID) error {
	if env == nil {
		return nil
	}
	var errs []error
	for _, id := range customDomainIDs {
		if _, err := (&CustomDomainRegistrationClient{TemporalEnv: env}).ExecuteCustomDomainReconcile(ctx, id); err != nil {
			errs = append(errs, oops.E(oops.CodeUnexpected, err, "start custom domain reconciliation").LogError(ctx, logger))
		}
	}
	return errors.Join(errs...)
}
