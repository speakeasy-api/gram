package mcpversions

// Client-to-server JSON-RPC method names defined by the core protocol of at
// least one recognized revision. [DefinesMethod] reports which revisions
// define each.
const (
	// MethodCompletionComplete requests argument autocompletion suggestions.
	MethodCompletionComplete = "completion/complete"

	// MethodInitialize opens the handshake of the revisions before 2026-07-28.
	MethodInitialize = "initialize"

	// MethodLoggingSetLevel sets the minimum level of server log messages.
	MethodLoggingSetLevel = "logging/setLevel"

	// MethodNotificationsCancelled cancels an in-flight request.
	MethodNotificationsCancelled = "notifications/cancelled"

	// MethodNotificationsInitialized completes the handshake of the revisions
	// before 2026-07-28.
	MethodNotificationsInitialized = "notifications/initialized"

	// MethodNotificationsProgress reports progress on a long-running request.
	MethodNotificationsProgress = "notifications/progress"

	// MethodNotificationsRootsListChanged signals that the client's roots
	// changed.
	MethodNotificationsRootsListChanged = "notifications/roots/list_changed"

	// MethodNotificationsTasksStatus reports a task status change.
	MethodNotificationsTasksStatus = "notifications/tasks/status"

	// MethodPing checks that the other side is still responsive.
	MethodPing = "ping"

	// MethodPromptsGet retrieves one prompt.
	MethodPromptsGet = "prompts/get"

	// MethodPromptsList lists the available prompts.
	MethodPromptsList = "prompts/list"

	// MethodResourcesList lists the available resources.
	MethodResourcesList = "resources/list"

	// MethodResourcesRead reads the contents of one resource.
	MethodResourcesRead = "resources/read"

	// MethodResourcesSubscribe subscribes to updates of one resource.
	MethodResourcesSubscribe = "resources/subscribe"

	// MethodResourcesTemplatesList lists the available resource templates.
	MethodResourcesTemplatesList = "resources/templates/list"

	// MethodResourcesUnsubscribe cancels a resource subscription.
	MethodResourcesUnsubscribe = "resources/unsubscribe"

	// MethodServerDiscover asks a 2026-07-28 server for its supported
	// revisions, capabilities, and identity.
	MethodServerDiscover = "server/discover"

	// MethodSubscriptionsListen opens the 2026-07-28 stream of opted-in change
	// notifications.
	MethodSubscriptionsListen = "subscriptions/listen"

	// MethodTasksCancel cancels a task.
	MethodTasksCancel = "tasks/cancel"

	// MethodTasksGet retrieves a task's status.
	MethodTasksGet = "tasks/get"

	// MethodTasksList lists tasks.
	MethodTasksList = "tasks/list"

	// MethodTasksResult waits for and retrieves a task's result.
	MethodTasksResult = "tasks/result"

	// MethodToolsCall invokes one tool.
	MethodToolsCall = "tools/call"

	// MethodToolsList lists the available tools.
	MethodToolsList = "tools/list"
)

// methodRange bounds the revisions whose core protocol defines one method.
type methodRange struct {
	// Since is the first revision defining the method.
	Since string

	// Until is the first revision removing the method from the core protocol,
	// or empty while every recognized revision from Since onward defines it.
	Until string
}

// clientToServerMethods records every client-to-server method a published
// revision defines in its core protocol, and the revisions that define it. It
// is a statement about the specification, not about Speakeasy: each surface's
// dispatch decides which of these it implements and answers the rest as
// method not found. Server-to-client methods (such as the 2025-11-25
// `notifications/elicitation/complete`) never reach Speakeasy's MCP servers and do
// not belong here.
//
// Methods that 2026-07-28 moved into an extension (the tasks family) end at
// that revision here, because the extension is negotiated separately from the
// protocol revision.
var clientToServerMethods = map[string]methodRange{
	MethodCompletionComplete:            {Since: Version20241105, Until: ""},
	MethodInitialize:                    {Since: Version20241105, Until: Version20260728},
	MethodLoggingSetLevel:               {Since: Version20241105, Until: Version20260728},
	MethodNotificationsCancelled:        {Since: Version20241105, Until: ""},
	MethodNotificationsInitialized:      {Since: Version20241105, Until: Version20260728},
	MethodNotificationsProgress:         {Since: Version20241105, Until: ""},
	MethodNotificationsRootsListChanged: {Since: Version20241105, Until: Version20260728},
	MethodNotificationsTasksStatus:      {Since: Version20251125, Until: Version20260728},
	MethodPing:                          {Since: Version20241105, Until: Version20260728},
	MethodPromptsGet:                    {Since: Version20241105, Until: ""},
	MethodPromptsList:                   {Since: Version20241105, Until: ""},
	MethodResourcesList:                 {Since: Version20241105, Until: ""},
	MethodResourcesRead:                 {Since: Version20241105, Until: ""},
	MethodResourcesSubscribe:            {Since: Version20241105, Until: Version20260728},
	MethodResourcesTemplatesList:        {Since: Version20241105, Until: ""},
	MethodResourcesUnsubscribe:          {Since: Version20241105, Until: Version20260728},
	MethodServerDiscover:                {Since: Version20260728, Until: ""},
	MethodSubscriptionsListen:           {Since: Version20260728, Until: ""},
	MethodTasksCancel:                   {Since: Version20251125, Until: Version20260728},
	MethodTasksGet:                      {Since: Version20251125, Until: Version20260728},
	MethodTasksList:                     {Since: Version20251125, Until: Version20260728},
	MethodTasksResult:                   {Since: Version20251125, Until: Version20260728},
	MethodToolsCall:                     {Since: Version20241105, Until: ""},
	MethodToolsList:                     {Since: Version20241105, Until: ""},
}

// KnownMethod reports whether any recognized revision's core protocol defines
// the client-to-server method. A method outside this set is either an
// extension's or one a revision this package does not recognize yet added.
func KnownMethod(method string) bool {
	_, ok := clientToServerMethods[method]
	return ok
}

// DefinesMethod reports whether revision's core protocol defines the
// client-to-server method. An unrecognized method or revision is never
// defined, so callers gating dispatch on it fail closed.
//
// Callers deciding what governs a request pass the revision in effect
// ([Resolution.InEffect]), never the declared one.
func DefinesMethod(method, revision string) bool {
	r, ok := clientToServerMethods[method]
	return ok && AtLeast(revision, r.Since) && (r.Until == "" || !AtLeast(revision, r.Until))
}
