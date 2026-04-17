package background

import (
	"go.temporal.io/sdk/workflow"
)

func Debounce[Params any, Result any](
	signalName string,
	wfn func(ctx workflow.Context, params Params) (result Result, err error),
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
