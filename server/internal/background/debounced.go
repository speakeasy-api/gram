package background

import (
	"go.temporal.io/sdk/workflow"
)

// Debounce wraps a temporal workflow definition with debounce semantics.
// It allows an execution model where only one workflow execution is allowed at
// a given time AND that any further execution request enqueue a subsequent run
// of the workflow.
// This complements the existing WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE which
// acts purely as a duplicate function call suppression mechanism, and doesn't
// allow for enqueuing subsequent runs of the workflow.
//
// `wrapped` is the workflow function whose body runs once per execution. It
// must return (result, nil) and must not issue its own ContinueAsNew —
// continuation is owned by this wrapper. Errors returned by `wrapped` are
// propagated unchanged.
//
// `continueAsSelf` is the function used as the ContinueAsNew target when the
// wrapper decides to enqueue another run. It MUST be the debounced wrapper
// itself, not `wrapped`, otherwise subsequent runs lose debounce semantics
// (the next run would not coalesce signals). Top-level workflow functions can
// reference themselves recursively in Go, so callers pass the function being
// defined here.
//
// `signalIDFunc` derives the signal channel name from the workflow params.
// It must produce the same string for the same params across all callers
// (workflow body, signal-with-start entrypoint, and any direct signal sender)
// so the debounce signal lands on the channel the workflow is listening on.
// Typical shape: `fmt.Sprintf("v1:my-workflow:%s/signal", params.Key)`.
//
// `reenqueue` lets callers say "another run is needed even without an extra
// signal" — e.g. when the activity reports HasMore. Treated additively with
// any signals received during this run.
func Debounce[Params any, Result any](
	wrapped func(ctx workflow.Context, params Params) (result Result, err error),
	continueAsSelf func(ctx workflow.Context, params Params) (result Result, err error),
	signalIDFunc func(params Params) string,
	reenqueue func(params Params, result Result) bool,
) func(ctx workflow.Context, params Params) (result Result, err error) {
	return func(ctx workflow.Context, params Params) (result Result, err error) {
		discard := ""
		signalCh := workflow.GetSignalChannel(ctx, signalIDFunc(params))
		// Drain the signal that triggered this run via signal-with-start so it
		// doesn't double-count toward the post-run reenqueue check.
		_ = signalCh.ReceiveAsync(&discard)

		res, err := wrapped(ctx, params)
		if err != nil {
			return res, err
		}

		recv := 0
		if reenqueue(params, res) {
			recv++
		}
		for {
			message := ""
			received := signalCh.ReceiveAsync(&message)
			if !received {
				break
			}
			recv++
		}

		if recv == 0 {
			return res, nil
		}

		return res, workflow.NewContinueAsNewError(ctx, continueAsSelf, params)
	}
}
