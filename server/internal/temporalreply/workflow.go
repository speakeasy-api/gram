// Package temporalreply implements request-reply rendezvous with one Temporal
// workflow per request. It is an experimental alternative to redisinbox and is
// not registered with production workers.
package temporalreply

import (
	"go.temporal.io/sdk/workflow"
)

const (
	// WorkflowName is the registration name expected by Requester.
	WorkflowName = "temporalreply.Request"

	// ReplySignalName carries serialized protobuf replies to request workflows.
	ReplySignalName = "temporalreply.Reply"
)

// Workflow waits for one reply signal and returns its serialized payload. If
// several signals are already queued, Receive consumes only the first and the
// workflow completes without observing the duplicates.
func Workflow(ctx workflow.Context) ([]byte, error) {
	var reply []byte
	workflow.GetSignalChannel(ctx, ReplySignalName).Receive(ctx, &reply)
	return reply, nil
}
