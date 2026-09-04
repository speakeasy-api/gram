package background

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	domainskills "github.com/speakeasy-api/gram/server/internal/skills"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

// pluginPublishForceSignal is the signal payload that upgrades a pending run to
// a forced publish. Any other payload leaves the run's SkipIfUnchanged as the
// starter set it, so a burst of ordinary change signals stays a cheap
// fingerprint check while a first publish (which has no fingerprint to compare
// against) still republishes unconditionally.
const pluginPublishForceSignal = "force"

// pluginPublishEnqueueTimeout bounds the signal-with-start RPC. Enqueuing runs
// inline on a request that has already committed, so it must fail fast rather
// than hold the response open through a Temporal outage.
const pluginPublishEnqueueTimeout = 10 * time.Second

// PluginPublishParams identifies the project to publish. One debounce key per
// project: every publish trigger for a project — a new plugin, a server or
// skill added to one, a project's first publish — lands on the same execution,
// so concurrent triggers can never race two publishes against the same GitHub
// repo.
type PluginPublishParams struct {
	ProjectID       uuid.UUID
	CreatedByUserID string
	CommitMessage   string
	// SkipIfUnchanged short-circuits the publish when the project's fingerprint
	// matches what was last published. Change triggers set it (the publish is
	// only worth doing if the generated output actually moved); a project's
	// first publish clears it.
	SkipIfUnchanged bool
	// AllowFirstPublish lets this publish create the project's marketplace repo
	// when it has none yet. Opt-in on purpose: a params payload encoded before
	// this field existed decodes as false, which defers the repo to the rollout
	// sweep rather than creating one a change signal never intended.
	AllowFirstPublish bool
}

func pluginPublishWorkflowID(params PluginPublishParams) string {
	return fmt.Sprintf("v1:plugin-publish:%s", params.ProjectID)
}

func pluginPublishDebounceSignal(params PluginPublishParams) string {
	return fmt.Sprintf("%s/signal", pluginPublishWorkflowID(params))
}

// ExecutePluginPublishWorkflowDebounced is the entry point for every
// change-driven publish. It replaces waiting on the rollout sweep (see
// plugin_generator_rollout.go, now an hourly safety net): a plugin or plugin
// membership change signals the publish for its own project directly.
//
// Must only be called after the triggering transaction has committed —
// enqueuing before commit risks Temporal publishing state that a later failure
// in the same transaction rolls back.
func ExecutePluginPublishWorkflowDebounced(ctx context.Context, temporalEnv *tenv.Environment, params PluginPublishParams) (client.WorkflowRun, error) {
	// Callers enqueue after commit on a non-cancelable context so the request
	// returning can't drop the signal, which leaves this RPC with no deadline
	// of its own: bound it here, or a Temporal outage blocks the mutation
	// request indefinitely instead of failing the best-effort enqueue.
	ctx, cancel := context.WithTimeout(ctx, pluginPublishEnqueueTimeout)
	defer cancel()

	id := pluginPublishWorkflowID(params)

	message := "enqueue"
	if !params.SkipIfUnchanged {
		message = pluginPublishForceSignal
	}

	run, err := temporalEnv.Client().SignalWithStartWorkflow(
		ctx,
		id,
		pluginPublishDebounceSignal(params),
		message,
		client.StartWorkflowOptions{
			ID:                       id,
			TaskQueue:                string(temporalEnv.Queue()),
			WorkflowIDReusePolicy:    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
			WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
			// Must exceed the activity's worst-case retry budget (3 attempts x
			// 5m StartToCloseTimeout + backoff ~= 16m), or the workflow can
			// expire mid-retry and drop the final result.
			WorkflowRunTimeout: 20 * time.Minute,
			// StartDelay is the debounce coalescing window. A single dashboard
			// interaction fans out into several writes (create a plugin, add a
			// server, add a skill), and each publish is a GitHub commit plus a
			// fresh API key, so batching them is worth a few seconds of latency.
			StartDelay: 10 * time.Second,
		},
		PluginPublishWorkflowDebounced,
		params,
	)
	if err != nil {
		return nil, fmt.Errorf("signal with start plugin publish workflow: %w", err)
	}
	return run, nil
}

// PluginPublishWorkflowDebounced is the registered workflow. Signals that
// arrive while a publish is in flight (another change landing mid-publish)
// coalesce into exactly one follow-up run rather than being dropped, so the
// repo always converges on the state that exists after the last change.
func PluginPublishWorkflowDebounced(ctx workflow.Context, params PluginPublishParams) (*plugins.PublishProjectResult, error) {
	return DebounceWithSignalCoalescing(
		PluginPublishWorkflow,
		PluginPublishWorkflowDebounced,
		pluginPublishDebounceSignal,
		func(PluginPublishParams, *plugins.PublishProjectResult) bool { return false },
		func(params PluginPublishParams, message string) PluginPublishParams {
			// A forced trigger folded into a pending run wins: skipping it would
			// strand the publish that needed to happen regardless of fingerprint.
			if message == pluginPublishForceSignal {
				params.SkipIfUnchanged = false
				params.AllowFirstPublish = true
			}
			return params
		},
	)(ctx, params)
}

// PluginPublishWorkflow runs one publish. It must not issue its own
// ContinueAsNew — continuation is owned by the Debounce wrapper.
func PluginPublishWorkflow(ctx workflow.Context, params PluginPublishParams) (*plugins.PublishProjectResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    1 * time.Minute,
		},
	})

	var a *Activities
	var result plugins.PublishProjectResult
	if err := workflow.ExecuteActivity(ctx, a.PublishPluginProject, plugins.PublishProjectInput{
		ProjectID:         params.ProjectID,
		CreatedByUserID:   params.CreatedByUserID,
		CommitMessage:     params.CommitMessage,
		SkipIfUnchanged:   params.SkipIfUnchanged,
		AllowFirstPublish: params.AllowFirstPublish,
	}).Get(ctx, &result); err != nil {
		return nil, fmt.Errorf("publish plugin project: %w", err)
	}
	return &result, nil
}

// TemporalPluginPublisher implements plugins.PluginPublishSignaler (and the
// identical skills.PluginPublishSignaler — skills cannot import plugins) by
// signal-with-starting the debounced per-project publish workflow.
type TemporalPluginPublisher struct {
	TemporalEnv *tenv.Environment
}

var _ plugins.PluginPublishSignaler = (*TemporalPluginPublisher)(nil)
var _ domainskills.PluginPublishSignaler = (*TemporalPluginPublisher)(nil)

// SignalPluginPublish enqueues a fingerprint-gated republish: the caller
// changed plugin state, so the publish is worth attempting, but it does no
// GitHub work if the generated output turns out to be unchanged.
func (p *TemporalPluginPublisher) SignalPluginPublish(ctx context.Context, projectID uuid.UUID, createdByUserID string) error {
	if _, err := ExecutePluginPublishWorkflowDebounced(ctx, p.TemporalEnv, PluginPublishParams{
		ProjectID:       projectID,
		CreatedByUserID: createdByUserID,
		CommitMessage:   "Update plugin packages",
		SkipIfUnchanged: true,
		// A plain plugin edit never brings a marketplace into existence.
		AllowFirstPublish: false,
	}); err != nil {
		return fmt.Errorf("signal plugin publish: %w", err)
	}
	return nil
}

// TriggerPluginPublish enqueues the marketplace publish for a project whose
// plugin membership just changed, shared by the mutation services (toolsets,
// mcpservers, mcpendpoints, projects) so those paths cannot drift apart in how
// they publish.
//
// allowFirstPublish says the caller attached something to a plugin, so the
// project may get its marketplace repo now rather than on the next sweep — a
// legacy project whose Default plugin already existed (lazily provisioned by a
// dashboard read) reaches its first attach with force false and still needs the
// repo. force additionally republishes regardless of fingerprint, which only a
// freshly created Default plugin or a brand new project needs: there is no
// prior fingerprint to compare against. Otherwise the publish is
// fingerprint-gated and does no GitHub work when the output is unchanged.
//
// Must only be called after the triggering transaction has committed —
// enqueuing before commit risks publishing state that a later failure in the
// same transaction rolls back. Best-effort: the enqueue is logged and never
// fails the request, since the rollout sweep still picks the project up on its
// next tick.
func TriggerPluginPublish(ctx context.Context, temporalEnv *tenv.Environment, logger *slog.Logger, projectID uuid.UUID, createdByUserID string, allowFirstPublish, force bool) {
	commitMessage := "Update plugin packages"
	if force {
		commitMessage = "Initial marketplace publish"
	}

	// The request returning shouldn't drop the enqueue.
	if _, err := ExecutePluginPublishWorkflowDebounced(context.WithoutCancel(ctx), temporalEnv, PluginPublishParams{
		ProjectID:         projectID,
		CreatedByUserID:   createdByUserID,
		CommitMessage:     commitMessage,
		SkipIfUnchanged:   !force,
		AllowFirstPublish: allowFirstPublish,
	}); err != nil {
		logger.WarnContext(ctx, "failed to enqueue plugin publish",
			attr.SlogProjectID(projectID.String()), attr.SlogError(err))
	}
}
