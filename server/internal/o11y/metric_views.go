package o11y

import (
	"slices"

	"go.opentelemetry.io/otel/sdk/metric"
)

// unqueriedInstruments names metrics that our instrumentation libraries
// register by default and that no Datadog dashboard, monitor or notebook had
// queried in the 30 days before they were listed here. Datadog bills each tag
// combination of these as a custom metric, and together they made up about 40%
// of our custom metric series, so the meter provider drops them instead of
// exporting them. Delete a name to export that metric again.
var unqueriedInstruments = []string{
	// otelhttp payload size histograms. http.server.request.duration and
	// http.client.request.duration are still exported.
	"http.client.request.body.size",
	"http.server.request.body.size",
	"http.server.response.body.size",

	// Temporal SDK client RPC metrics.
	"temporal_request",
	"temporal_request_attempt",
	"temporal_request_failure",
	"temporal_request_failure_attempt",
	"temporal_request_latency",
	"temporal_request_latency_attempt",
	"temporal_long_request_latency",
	"temporal_long_request_latency_attempt",

	// Temporal SDK worker internals. Workflow and activity outcome counters,
	// temporal_workflow_endtoend_latency and temporal_activity_execution_latency
	// are still exported.
	"temporal_activity_poll_no_task",
	"temporal_activity_schedule_to_start_latency",
	"temporal_activity_succeed_endtoend_latency",
	"temporal_num_pollers",
	"temporal_poller_start",
	"temporal_sticky_cache_hit",
	"temporal_sticky_cache_size",
	"temporal_worker_start",
	"temporal_worker_task_slots_available",
	"temporal_worker_task_slots_used",
	"temporal_workflow_continue_as_new",
	"temporal_workflow_task_execution_latency",
	"temporal_workflow_task_queue_poll_empty",
	"temporal_workflow_task_queue_poll_succeed",
	"temporal_workflow_task_replay_latency",
	"temporal_workflow_task_schedule_to_start_latency",
}

// dropUnqueriedInstruments is a metric view that discards every measurement
// recorded by an instrument named in unqueriedInstruments and leaves other
// instruments to the default view.
func dropUnqueriedInstruments(instrument metric.Instrument) (stream metric.Stream, ok bool) {
	if !slices.Contains(unqueriedInstruments, instrument.Name) {
		return stream, false
	}

	return metric.Stream{
		Name:                              instrument.Name,
		Description:                       instrument.Description,
		Unit:                              instrument.Unit,
		Aggregation:                       metric.AggregationDrop{},
		AttributeFilter:                   nil,
		ExemplarReservoirProviderSelector: nil,
	}, true
}
