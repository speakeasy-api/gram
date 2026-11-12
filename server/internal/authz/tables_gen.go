// Code generated from registry.Definitions by authzgen. DO NOT EDIT.
// Regenerate with `mise run gen:authz`.

package authz

// The tables below are computed from the registry by authzgen, so the
// evaluator reads plain values and holds no derivation logic.

// scopeExpansions maps a checked scope to the other scopes that also satisfy
// it. For allow scopes, these are higher-privilege scopes. For blocklist
// scopes, these are broader blocklist scopes. Expansion is non-transitive: the
// table lists every satisfying scope directly, since Check.expand only walks
// scopeExpansions[c.Scope] one step. Built from Implies and Exclusions; the
// comments on each resource type in the registry say why its permissions
// relate the way they do.
var scopeExpansions = map[Scope][]Scope{
	"agent:authorize":           nil,
	"agent:read":                nil,
	"agent:transfer":            nil,
	"agent:write":               nil,
	"assistant:blocked_read":    nil,
	"assistant:blocked_write":   {"assistant:blocked_read"},
	"assistant:read":            {"assistant:write"},
	"assistant:write":           nil,
	"chat:read":                 {"chat:write"},
	"chat:write":                nil,
	"environment:blocked_read":  nil,
	"environment:blocked_write": {"environment:blocked_read"},
	"environment:read":          {"environment:write"},
	"environment:write":         nil,
	"inference:execute":         nil,
	"log:read":                  nil,
	"mcp:auth":                  nil,
	"mcp:blocked_auth":          nil,
	"mcp:blocked_connect":       nil,
	"mcp:blocked_read":          nil,
	"mcp:blocked_write":         {"mcp:blocked_read"},
	"mcp:connect":               nil,
	"mcp:read":                  {"mcp:write"},
	"mcp:write":                 nil,
	"org:admin":                 nil,
	"org:blocked_admin":         {"org:blocked_read"},
	"org:blocked_read":          nil,
	"org:device_agent_sync":     {"org:admin"},
	"org:hooks_ingest":          {"org:admin"},
	"org:read":                  {"org:admin"},
	"plugin:blocked_write":      nil,
	"plugin:write":              nil,
	"project:blocked_read":      nil,
	"project:blocked_write":     {"project:blocked_read"},
	"project:read":              {"project:write"},
	"project:write":             nil,
	"risk_policy:block":         nil,
	"risk_policy:bypass":        nil,
	"risk_policy:evaluate":      nil,
	"root":                      nil,
	"skill:blocked_read":        nil,
	"skill:blocked_write":       {"skill:blocked_read"},
	"skill:read":                {"skill:write"},
	"skill:write":               nil,
	"workload:blocked_read":     nil,
	"workload:blocked_write":    {"workload:blocked_read"},
	"workload:read":             {"workload:write"},
	"workload:write":            nil,
}

// scopeExclusions maps a checked base scope to the direct blocklist scope that
// stores exception grants for it, or "" when it has none. Broader blocklist
// scopes are handled by scopeExpansions on the blocklist scope itself.
var scopeExclusions = map[Scope]Scope{
	"agent:authorize":           "",
	"agent:read":                "",
	"agent:transfer":            "",
	"agent:write":               "",
	"assistant:blocked_read":    "",
	"assistant:blocked_write":   "",
	"assistant:read":            "assistant:blocked_read",
	"assistant:write":           "assistant:blocked_write",
	"chat:read":                 "",
	"chat:write":                "",
	"environment:blocked_read":  "",
	"environment:blocked_write": "",
	"environment:read":          "environment:blocked_read",
	"environment:write":         "environment:blocked_write",
	"inference:execute":         "",
	"log:read":                  "",
	"mcp:auth":                  "mcp:blocked_auth",
	"mcp:blocked_auth":          "",
	"mcp:blocked_connect":       "",
	"mcp:blocked_read":          "",
	"mcp:blocked_write":         "",
	"mcp:connect":               "mcp:blocked_connect",
	"mcp:read":                  "mcp:blocked_read",
	"mcp:write":                 "mcp:blocked_write",
	"org:admin":                 "org:blocked_admin",
	"org:blocked_admin":         "",
	"org:blocked_read":          "",
	"org:device_agent_sync":     "",
	"org:hooks_ingest":          "",
	"org:read":                  "org:blocked_read",
	"plugin:blocked_write":      "",
	"plugin:write":              "plugin:blocked_write",
	"project:blocked_read":      "",
	"project:blocked_write":     "",
	"project:read":              "project:blocked_read",
	"project:write":             "project:blocked_write",
	"risk_policy:block":         "",
	"risk_policy:bypass":        "",
	"risk_policy:evaluate":      "risk_policy:bypass",
	"root":                      "",
	"skill:blocked_read":        "",
	"skill:blocked_write":       "",
	"skill:read":                "skill:blocked_read",
	"skill:write":               "skill:blocked_write",
	"workload:blocked_read":     "",
	"workload:blocked_write":    "",
	"workload:read":             "workload:blocked_read",
	"workload:write":            "workload:blocked_write",
}

// scopeGrantSource maps every scope to where its grants are set. A block
// is set where its permission is.
var scopeGrantSource = map[Scope]GrantSource{
	"agent:authorize":           "access",
	"agent:read":                "access",
	"agent:transfer":            "access",
	"agent:write":               "access",
	"assistant:blocked_read":    "access",
	"assistant:blocked_write":   "access",
	"assistant:read":            "access",
	"assistant:write":           "access",
	"chat:read":                 "access",
	"chat:write":                "access",
	"environment:blocked_read":  "access",
	"environment:blocked_write": "access",
	"environment:read":          "access",
	"environment:write":         "access",
	"inference:execute":         "access",
	"log:read":                  "access",
	"mcp:auth":                  "access",
	"mcp:blocked_auth":          "access",
	"mcp:blocked_connect":       "access",
	"mcp:blocked_read":          "access",
	"mcp:blocked_write":         "access",
	"mcp:connect":               "access",
	"mcp:read":                  "access",
	"mcp:write":                 "access",
	"org:admin":                 "access",
	"org:blocked_admin":         "access",
	"org:blocked_read":          "access",
	"org:device_agent_sync":     "access",
	"org:hooks_ingest":          "access",
	"org:read":                  "access",
	"plugin:blocked_write":      "access",
	"plugin:write":              "access",
	"project:blocked_read":      "access",
	"project:blocked_write":     "access",
	"project:read":              "access",
	"project:write":             "access",
	"risk_policy:block":         "risk_policy",
	"risk_policy:bypass":        "risk_policy",
	"risk_policy:evaluate":      "risk_policy",
	"root":                      "access",
	"skill:blocked_read":        "access",
	"skill:blocked_write":       "access",
	"skill:read":                "access",
	"skill:write":               "access",
	"workload:blocked_read":     "access",
	"workload:blocked_write":    "access",
	"workload:read":             "access",
	"workload:write":            "access",
}

// scopeResourceKinds maps every scope the registry declares to the resource
// kind its checks and grant selectors name.
var scopeResourceKinds = map[Scope]string{
	"agent:authorize":           "agent",
	"agent:read":                "agent",
	"agent:transfer":            "agent",
	"agent:write":               "agent",
	"assistant:blocked_read":    "assistant",
	"assistant:blocked_write":   "assistant",
	"assistant:read":            "assistant",
	"assistant:write":           "assistant",
	"chat:read":                 "chat",
	"chat:write":                "chat",
	"environment:blocked_read":  "environment",
	"environment:blocked_write": "environment",
	"environment:read":          "environment",
	"environment:write":         "environment",
	"inference:execute":         "inference",
	"log:read":                  "project",
	"mcp:auth":                  "mcp_auth_client",
	"mcp:blocked_auth":          "mcp_auth_client",
	"mcp:blocked_connect":       "mcp",
	"mcp:blocked_read":          "mcp",
	"mcp:blocked_write":         "mcp",
	"mcp:connect":               "mcp",
	"mcp:read":                  "mcp",
	"mcp:write":                 "mcp",
	"org:admin":                 "org",
	"org:blocked_admin":         "org",
	"org:blocked_read":          "org",
	"org:device_agent_sync":     "org",
	"org:hooks_ingest":          "org",
	"org:read":                  "org",
	"plugin:blocked_write":      "project",
	"plugin:write":              "project",
	"project:blocked_read":      "project",
	"project:blocked_write":     "project",
	"project:read":              "project",
	"project:write":             "project",
	"risk_policy:block":         "risk_policy",
	"risk_policy:bypass":        "risk_policy",
	"risk_policy:evaluate":      "risk_policy",
	"root":                      "*",
	"skill:blocked_read":        "skill",
	"skill:blocked_write":       "skill",
	"skill:read":                "skill",
	"skill:write":               "skill",
	"workload:blocked_read":     "workload",
	"workload:blocked_write":    "workload",
	"workload:read":             "workload",
	"workload:write":            "workload",
}

// declaredResourceKinds is the set of resource kinds the registry declares.
var declaredResourceKinds = map[string]bool{
	"agent":           true,
	"assistant":       true,
	"chat":            true,
	"environment":     true,
	"inference":       true,
	"mcp":             true,
	"mcp_auth_client": true,
	"org":             true,
	"project":         true,
	"risk_policy":     true,
	"skill":           true,
	"workload":        true,
}

// requiredCheckDimensions lists, per resource kind, the dimensions every check
// of that kind must carry: the Required dimensions in the registry. Selector
// matching skips grant keys a check omits, so a check without a dimension that
// grants of its kind can be bounded by would pass such a grant whatever value
// the grant names. MCP grants can be bounded to a project
// ({"resource_id":"*","project_id":P}), so every MCP check must name the
// project of the resource it targets.
var requiredCheckDimensions = map[string][]string{
	"mcp": {"project_id"},
}

// unknownableDimensions lists, per scope, the dimensions a check may carry as
// SelectorValueUnknown.
var unknownableDimensions = map[Scope]map[string]bool{
	"mcp:blocked_connect": {"disposition": true, "tool": true},
	"mcp:blocked_read":    {"disposition": true, "tool": true},
	"mcp:blocked_write":   {"disposition": true, "tool": true},
	"mcp:connect":         {"disposition": true, "tool": true},
	"mcp:read":            {"disposition": true, "tool": true},
	"mcp:write":           {"disposition": true, "tool": true},
	"risk_policy:block":   {"server_identity": true, "server_url": true},
}

// allowedSelectorKeys defines which extra keys (beyond resource_kind and
// resource_id) are valid for each resource kind: the Dimensions each type
// declares. Kinds not listed here allow no extra keys, except where
// scopeAllowsSelectorKey admits one for a single scope.
var allowedSelectorKeys = map[string]map[string]bool{
	"assistant":       {"project_id": true},
	"environment":     {"project_id": true},
	"inference":       {"project_id": true},
	"mcp":             {"disposition": true, "project_id": true, "tool": true},
	"mcp_auth_client": {"client_type": true, "connection_owner": true, "project_id": true, "upstream_client": true},
	"risk_policy":     {"server_identity": true, "server_url": true},
}

// scopeSelectorKeys holds the keys single permissions declare beyond their
// type's.
var scopeSelectorKeys = map[Scope]map[string]bool{
	"org:hooks_ingest": {"project_id": true},
}

// validAuthClientTypes is the closed value set of client_type.
var validAuthClientTypes = map[string]bool{
	"ema":               true,
	"pass_through":      true,
	"remote_session":    true,
	"service_account":   true,
	"static_credential": true,
}

// validDispositions is the closed value set of disposition.
var validDispositions = map[string]bool{"destructive": true, "idempotent": true, "open_world": true, "read_only": true}

// systemRoleScopes lists the scopes each built-in role is seeded with.
var systemRoleScopes = map[string][]Scope{
	"admin":  {"org:read", "org:admin", "project:read", "project:write", "mcp:read", "mcp:write", "mcp:connect", "mcp:auth", "environment:read", "environment:write", "skill:read", "skill:write", "assistant:read", "assistant:write", "plugin:write", "agent:read", "agent:write", "agent:authorize", "agent:transfer", "workload:read", "workload:write"},
	"member": {"org:read", "project:read", "mcp:read", "mcp:connect", "mcp:auth", "skill:read", "assistant:read", "workload:read"},
}

// agentEligibilityByScope is the agent eligibility of every scope the registry
// knows, retired tombstones included.
var agentEligibilityByScope = map[Scope]DerivedAgentEligibility{
	"agent:authorize":           {SafeSince: 0, Retired: false},
	"agent:read":                {SafeSince: 0, Retired: false},
	"agent:transfer":            {SafeSince: 0, Retired: false},
	"agent:write":               {SafeSince: 0, Retired: false},
	"assistant:blocked_read":    {SafeSince: 4, Retired: false},
	"assistant:blocked_write":   {SafeSince: 4, Retired: false},
	"assistant:read":            {SafeSince: 3, Retired: false},
	"assistant:write":           {SafeSince: 3, Retired: false},
	"chat:read":                 {SafeSince: 0, Retired: false},
	"chat:write":                {SafeSince: 0, Retired: false},
	"environment:blocked_read":  {SafeSince: 4, Retired: false},
	"environment:blocked_write": {SafeSince: 4, Retired: false},
	"environment:read":          {SafeSince: 1, Retired: false},
	"environment:write":         {SafeSince: 1, Retired: false},
	"inference:execute":         {SafeSince: 6, Retired: false},
	"log:read":                  {SafeSince: 0, Retired: false},
	"mcp:auth":                  {SafeSince: 5, Retired: false},
	"mcp:blocked_auth":          {SafeSince: 5, Retired: false},
	"mcp:blocked_connect":       {SafeSince: 4, Retired: false},
	"mcp:blocked_read":          {SafeSince: 4, Retired: false},
	"mcp:blocked_write":         {SafeSince: 4, Retired: false},
	"mcp:connect":               {SafeSince: 1, Retired: false},
	"mcp:read":                  {SafeSince: 1, Retired: false},
	"mcp:write":                 {SafeSince: 1, Retired: false},
	"mcp_approval:decide":       {SafeSince: 0, Retired: true},
	"mcp_approval:read":         {SafeSince: 0, Retired: true},
	"org:admin":                 {SafeSince: 0, Retired: false},
	"org:blocked_admin":         {SafeSince: 7, Retired: false},
	"org:blocked_read":          {SafeSince: 7, Retired: false},
	"org:device_agent_sync":     {SafeSince: 2, Retired: false},
	"org:hooks_ingest":          {SafeSince: 2, Retired: false},
	"org:read":                  {SafeSince: 7, Retired: false},
	"plugin:blocked_write":      {SafeSince: 0, Retired: false},
	"plugin:write":              {SafeSince: 0, Retired: false},
	"project:blocked_read":      {SafeSince: 4, Retired: false},
	"project:blocked_write":     {SafeSince: 4, Retired: false},
	"project:read":              {SafeSince: 1, Retired: false},
	"project:write":             {SafeSince: 1, Retired: false},
	"risk_policy:block":         {SafeSince: 0, Retired: false},
	"risk_policy:bypass":        {SafeSince: 0, Retired: false},
	"risk_policy:evaluate":      {SafeSince: 1, Retired: false},
	"root":                      {SafeSince: 0, Retired: false},
	"skill:blocked_read":        {SafeSince: 4, Retired: false},
	"skill:blocked_write":       {SafeSince: 4, Retired: false},
	"skill:read":                {SafeSince: 1, Retired: false},
	"skill:write":               {SafeSince: 1, Retired: false},
	"workload:blocked_read":     {SafeSince: 0, Retired: false},
	"workload:blocked_write":    {SafeSince: 0, Retired: false},
	"workload:read":             {SafeSince: 0, Retired: false},
	"workload:write":            {SafeSince: 0, Retired: false},
}

// scopeCatalog lists every scope a role can hold, in the order the catalogue
// presents them: by category, then as declared, each block after its
// permission. ScopeRoot is deliberately absent: it is the engine's own bypass,
// never a grant a person reads about.
var scopeCatalog = []ScopeEntry{
	{Scope: "org:read", Label: "View organization", Category: "org", ResourceType: "org", Description: "View organization settings and members.", Sensitivity: ""},
	{Scope: "org:blocked_read", Label: "Exception: view organization", Category: "org", ResourceType: "org", Description: "Store exceptions for organization read access.", Sensitivity: ""},
	{Scope: "org:admin", Label: "Manage organization", Category: "org", ResourceType: "org", Description: "Manage organization settings, members, roles and access. Includes removing members and changing what every role can do.", Sensitivity: "destructive_admin"},
	{Scope: "org:blocked_admin", Label: "Exception: manage organization", Category: "org", ResourceType: "org", Description: "Store exceptions for organization admin access.", Sensitivity: ""},
	{Scope: "org:device_agent_sync", Label: "Sync device agent plugins", Category: "org", ResourceType: "org", Description: "Let an agent's device agent sync the plugins assigned to it.", Sensitivity: ""},
	{Scope: "org:hooks_ingest", Label: "Send hook events", Category: "org", ResourceType: "org", Description: "Let an agent send AI-tool hook events and telemetry.", Sensitivity: ""},
	{Scope: "project:read", Label: "View projects", Category: "project", ResourceType: "project", Description: "View projects and project-related resources.", Sensitivity: ""},
	{Scope: "project:blocked_read", Label: "Exception: view projects", Category: "project", ResourceType: "project", Description: "Store exceptions for project read access.", Sensitivity: ""},
	{Scope: "project:write", Label: "Manage projects", Category: "project", ResourceType: "project", Description: "Create and modify projects and project-related resources.", Sensitivity: ""},
	{Scope: "project:blocked_write", Label: "Exception: manage projects", Category: "project", ResourceType: "project", Description: "Store exceptions for project write access.", Sensitivity: ""},
	{Scope: "mcp:read", Label: "View MCP servers", Category: "mcp", ResourceType: "mcp", Description: "View MCP servers and their configuration.", Sensitivity: ""},
	{Scope: "mcp:blocked_read", Label: "Exception: view MCP servers", Category: "mcp", ResourceType: "mcp", Description: "Store exceptions for MCP read access.", Sensitivity: ""},
	{Scope: "mcp:write", Label: "Manage MCP servers", Category: "mcp", ResourceType: "mcp", Description: "Create, change and delete MCP servers and their configuration.", Sensitivity: ""},
	{Scope: "mcp:blocked_write", Label: "Exception: manage MCP servers", Category: "mcp", ResourceType: "mcp", Description: "Store exceptions for MCP write access.", Sensitivity: ""},
	{Scope: "mcp:connect", Label: "Connect to MCP servers", Category: "mcp", ResourceType: "mcp", Description: "Connect to MCP servers and call their tools.", Sensitivity: ""},
	{Scope: "mcp:blocked_connect", Label: "Exception: connect to MCP servers", Category: "mcp", ResourceType: "mcp", Description: "Store exceptions for MCP connect access.", Sensitivity: ""},
	{Scope: "mcp:auth", Label: "Use shared auth clients", Category: "mcp_auth_client", ResourceType: "mcp_auth_client", Description: "Use an upstream auth client to reach MCP servers. Applies on every server that shares the client.", Sensitivity: ""},
	{Scope: "mcp:blocked_auth", Label: "Exception: use shared auth clients", Category: "mcp_auth_client", ResourceType: "mcp_auth_client", Description: "Store exceptions for auth client use.", Sensitivity: ""},
	{Scope: "environment:read", Label: "View environments", Category: "environment", ResourceType: "environment", Description: "View environments and their entries within the project, including secret values.", Sensitivity: ""},
	{Scope: "environment:blocked_read", Label: "Exception: view environments", Category: "environment", ResourceType: "environment", Description: "Store exceptions for environment read access.", Sensitivity: ""},
	{Scope: "environment:write", Label: "Manage environments", Category: "environment", ResourceType: "environment", Description: "Add, edit, clone, and remove environments within the project.", Sensitivity: ""},
	{Scope: "environment:blocked_write", Label: "Exception: manage environments", Category: "environment", ResourceType: "environment", Description: "Store exceptions for environment write access.", Sensitivity: ""},
	{Scope: "skill:read", Label: "View skills", Category: "skill", ResourceType: "skill", Description: "View skills within the project.", Sensitivity: ""},
	{Scope: "skill:blocked_read", Label: "Exception: view skills", Category: "skill", ResourceType: "skill", Description: "Store exceptions for skill read access.", Sensitivity: ""},
	{Scope: "skill:write", Label: "Manage skills", Category: "skill", ResourceType: "skill", Description: "Create and modify skills within the project.", Sensitivity: ""},
	{Scope: "skill:blocked_write", Label: "Exception: manage skills", Category: "skill", ResourceType: "skill", Description: "Store exceptions for skill write access.", Sensitivity: ""},
	{Scope: "plugin:write", Label: "Publish plugins", Category: "plugin", ResourceType: "project", Description: "Manage plugin contents and publish plugins within the project, without editing referenced skills or MCP servers.", Sensitivity: ""},
	{Scope: "plugin:blocked_write", Label: "Exception: publish plugins", Category: "plugin", ResourceType: "project", Description: "Store exceptions for plugin write access.", Sensitivity: ""},
	{Scope: "assistant:read", Label: "View assistants", Category: "assistant", ResourceType: "assistant", Description: "View and interact with assistants within the project.", Sensitivity: ""},
	{Scope: "assistant:blocked_read", Label: "Exception: view assistants", Category: "assistant", ResourceType: "assistant", Description: "Store exceptions for assistant read access.", Sensitivity: ""},
	{Scope: "assistant:write", Label: "Manage assistants", Category: "assistant", ResourceType: "assistant", Description: "Create and modify assistants within the project.", Sensitivity: ""},
	{Scope: "assistant:blocked_write", Label: "Exception: manage assistants", Category: "assistant", ResourceType: "assistant", Description: "Store exceptions for assistant write access.", Sensitivity: ""},
	{Scope: "risk_policy:evaluate", Label: "Evaluate risk policies", Category: "risk_policy", ResourceType: "risk_policy", Description: "Put the holder in the named risk policy's audience, so the policy is evaluated against their requests unless a bypass grant for the same policy exempts the request.", Sensitivity: ""},
	{Scope: "risk_policy:bypass", Label: "Bypass risk policies", Category: "risk_policy", ResourceType: "risk_policy", Description: "Exempt the holder from the named risk policy for the exact server the grant names, by URL or identity. It only subtracts from a matching evaluate grant and never puts anyone in a policy's audience.", Sensitivity: ""},
	{Scope: "risk_policy:block", Label: "Block shadow MCP servers", Category: "risk_policy", ResourceType: "risk_policy", Description: "Block specific shadow MCP servers under allow-by-default risk policies.", Sensitivity: ""},
	{Scope: "chat:read", Label: "View agent sessions", Category: "session", ResourceType: "chat", Description: "Read and pin other members' agent session transcripts, and reveal secrets flagged in Risk Events. Everyone keeps their own sessions.", Sensitivity: "conversations"},
	{Scope: "chat:write", Label: "Manage agent sessions", Category: "session", ResourceType: "chat", Description: "Rename, delete, and give feedback on other members' agent sessions, and read their transcripts. Everyone keeps their own.", Sensitivity: "conversations"},
	{Scope: "inference:execute", Label: "Run inference", Category: "inference", ResourceType: "inference", Description: "Run model inference without accessing chat transcripts.", Sensitivity: ""},
	{Scope: "log:read", Label: "View tool logs", Category: "log", ResourceType: "project", Description: "View the project's tool-call logs and traces, including tool arguments, results, and prompt text from every member. Not implied by project or organization admin access.", Sensitivity: "tool_logs"},
	{Scope: "agent:read", Label: "View agents", Category: "agent", ResourceType: "agent", Description: "View agents and their settings.", Sensitivity: ""},
	{Scope: "agent:write", Label: "Manage agents", Category: "agent", ResourceType: "agent", Description: "Create, configure, and manage agents.", Sensitivity: ""},
	{Scope: "agent:authorize", Label: "Authorize agent credentials", Category: "agent", ResourceType: "agent", Description: "Authorize agents and manage their credentials.", Sensitivity: ""},
	{Scope: "agent:transfer", Label: "Transfer agent ownership", Category: "agent", ResourceType: "agent", Description: "Hand an agent to another owner. The previous owner loses control of it.", Sensitivity: ""},
	{Scope: "workload:read", Label: "View workload identities", Category: "workload", ResourceType: "workload", Description: "View workload identity trust policy: issuers, admitted subjects, and assigned agents.", Sensitivity: ""},
	{Scope: "workload:blocked_read", Label: "Exception: view workload identities", Category: "workload", ResourceType: "workload", Description: "Store exceptions for workload read access.", Sensitivity: ""},
	{Scope: "workload:write", Label: "Manage workload identities", Category: "workload", ResourceType: "workload", Description: "Register workload issuers, admit and withdraw subjects, and assign agents to workloads.", Sensitivity: ""},
	{Scope: "workload:blocked_write", Label: "Exception: manage workload identities", Category: "workload", ResourceType: "workload", Description: "Store exceptions for workload write access.", Sensitivity: ""},
}

// scopeVisibilityByScope says whether a scope is exposed to clients as a
// first-class permission or only used internally for storage and evaluation:
// declared permissions are user-visible, generated blocks and root are internal.
var scopeVisibilityByScope = map[Scope]scopeVisibility{
	"agent:authorize":           scopeVisibilityUserVisible,
	"agent:read":                scopeVisibilityUserVisible,
	"agent:transfer":            scopeVisibilityUserVisible,
	"agent:write":               scopeVisibilityUserVisible,
	"assistant:blocked_read":    scopeVisibilityInternal,
	"assistant:blocked_write":   scopeVisibilityInternal,
	"assistant:read":            scopeVisibilityUserVisible,
	"assistant:write":           scopeVisibilityUserVisible,
	"chat:read":                 scopeVisibilityUserVisible,
	"chat:write":                scopeVisibilityUserVisible,
	"environment:blocked_read":  scopeVisibilityInternal,
	"environment:blocked_write": scopeVisibilityInternal,
	"environment:read":          scopeVisibilityUserVisible,
	"environment:write":         scopeVisibilityUserVisible,
	"inference:execute":         scopeVisibilityUserVisible,
	"log:read":                  scopeVisibilityUserVisible,
	"mcp:auth":                  scopeVisibilityUserVisible,
	"mcp:blocked_auth":          scopeVisibilityInternal,
	"mcp:blocked_connect":       scopeVisibilityInternal,
	"mcp:blocked_read":          scopeVisibilityInternal,
	"mcp:blocked_write":         scopeVisibilityInternal,
	"mcp:connect":               scopeVisibilityUserVisible,
	"mcp:read":                  scopeVisibilityUserVisible,
	"mcp:write":                 scopeVisibilityUserVisible,
	"org:admin":                 scopeVisibilityUserVisible,
	"org:blocked_admin":         scopeVisibilityInternal,
	"org:blocked_read":          scopeVisibilityInternal,
	"org:device_agent_sync":     scopeVisibilityUserVisible,
	"org:hooks_ingest":          scopeVisibilityUserVisible,
	"org:read":                  scopeVisibilityUserVisible,
	"plugin:blocked_write":      scopeVisibilityInternal,
	"plugin:write":              scopeVisibilityUserVisible,
	"project:blocked_read":      scopeVisibilityInternal,
	"project:blocked_write":     scopeVisibilityInternal,
	"project:read":              scopeVisibilityUserVisible,
	"project:write":             scopeVisibilityUserVisible,
	"risk_policy:block":         scopeVisibilityUserVisible,
	"risk_policy:bypass":        scopeVisibilityUserVisible,
	"risk_policy:evaluate":      scopeVisibilityUserVisible,
	"root":                      scopeVisibilityInternal,
	"skill:blocked_read":        scopeVisibilityInternal,
	"skill:blocked_write":       scopeVisibilityInternal,
	"skill:read":                scopeVisibilityUserVisible,
	"skill:write":               scopeVisibilityUserVisible,
	"workload:blocked_read":     scopeVisibilityInternal,
	"workload:blocked_write":    scopeVisibilityInternal,
	"workload:read":             scopeVisibilityUserVisible,
	"workload:write":            scopeVisibilityUserVisible,
}
