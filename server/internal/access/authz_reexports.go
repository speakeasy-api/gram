package access

import "github.com/speakeasy-api/gram/server/internal/authz"

// Type aliases — identical to authz originals, no conversion needed.
type (
	Scope    = authz.Scope
	Grant    = authz.Grant
	Selector = authz.Selector
)

// Re-exported constants.
const WildcardResource = authz.WildcardResource

// Scope constants re-exported for convenience. ScopeBuildRead/Write map to the
// underlying project:read/project:write scope values since "build" is the
// user-facing name for project-scoped access.
const (
	ScopeRoot         = authz.ScopeRoot
	ScopeOrgRead      = authz.ScopeOrgRead
	ScopeOrgAdmin     = authz.ScopeOrgAdmin
	ScopeProjectRead  = authz.ScopeProjectRead
	ScopeProjectWrite = authz.ScopeProjectWrite
	ScopeBuildRead    = authz.ScopeProjectRead
	ScopeBuildWrite   = authz.ScopeProjectWrite
	ScopeMCPRead      = authz.ScopeMCPRead
	ScopeMCPWrite     = authz.ScopeMCPWrite
	ScopeMCPConnect   = authz.ScopeMCPConnect
)

// Re-exported constructors and helpers.
var (
	NewSelector          = authz.NewSelector
	NewGrant             = authz.NewGrant
	NewGrantWithSelector = authz.NewGrantWithSelector
	ResourceKindForScope = authz.ResourceKindForScope
	ValidateSelector     = authz.ValidateSelector
)

// selectorFromRow wraps authz.SelectorFromRow for internal test use.
var selectorFromRow = authz.SelectorFromRow
