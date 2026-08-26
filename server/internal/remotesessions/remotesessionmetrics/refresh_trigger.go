package remotesessionmetrics

// RefreshTrigger names which caller initiated an upstream refresh attempt.
type RefreshTrigger string

const (
	// RefreshTriggerRequest: the lazy path, where an MCP request found the
	// stored access token expired and refreshed it inline. Every consumer of
	// the token resolver records here, so Platform MCP readiness probes and
	// the assistant runtime's token resolution count alongside MCP traffic.
	RefreshTriggerRequest RefreshTrigger = "request"

	// RefreshTriggerScheduled: the background worker's pre-emptive refresh
	// sweep.
	RefreshTriggerScheduled RefreshTrigger = "scheduled"

	// RefreshTriggerManual: a person clicked refresh, on the consent page or
	// in the organization admin view.
	RefreshTriggerManual RefreshTrigger = "manual"
)
