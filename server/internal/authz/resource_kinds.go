package authz

const (
	ResourceKindWildcard    = WildcardResource
	ResourceKindProject     = "project"
	ResourceKindMCP         = "mcp"
	ResourceKindOrg         = "org"
	ResourceKindEnvironment = "environment"
	ResourceKindSkill       = "skill"
	ResourceKindRiskPolicy  = "risk_policy"
	ResourceKindChat        = "chat"
	ResourceKindAgent       = "agent"
	// ResourceKindWorkload scopes managing an organization's workload identity
	// trust policy: its issuers, its admitted subjects, and which agent each
	// inherits its policy from.
	//
	// Not to be confused with urn.PrincipalTypeWorkload, which is also
	// "workload" and names the machine principal itself — the grantee. These
	// never meet: a selector's resource_kind is never compared against a
	// principal type. A grant of workload:write authorizes configuring the trust
	// policy, not acting as, or writing to, any one workload.
	ResourceKindWorkload = "workload"
)
