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
func Debounce[Params any, Result any](
	wfn func(ctx workflow.Context, params Params) (result Result, err error),
	signalName string,
	reenqueue func(params Params, result Result) bool,
) func(ctx workflow.Context, params Params) (result Result, err error) {
	return func(ctx workflow.Context, params Params) (result Result, err error) {
		discard := ""
		signalCh := workflow.GetSignalChannel(ctx, signalName)
		// Get the initial signal that triggered the workflow with signal-with-start
		_ = signalCh.ReceiveAsync(&discard)

		res, err := wfn(ctx, params)
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

		return res, workflow.NewContinueAsNewError(ctx, wfn, params)
	}
}
