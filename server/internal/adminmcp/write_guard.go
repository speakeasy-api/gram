package adminmcp

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

const (
	ScopeRead  = "admin:read"
	ScopeWrite = "admin:write"
)

// WriteOperation names one allowlisted staff mutation. The runtime never
// dispatches a mutation that is not declared here.
type WriteOperation string

const (
	OperationSetOrganizationFeature    WriteOperation = "set_organization_feature"
	OperationSetOrganizationOnboarding WriteOperation = "set_organization_onboarding"
	OperationSetChatAnalysisSettings   WriteOperation = "set_organization_chat_analysis_settings"
	OperationExtendOrganizationTrial   WriteOperation = "extend_organization_trial"
	OperationEnableOrganization        WriteOperation = "enable_organization"
	OperationDisableOrganization       WriteOperation = "disable_organization"
	OperationCreateGlobalIssuer        WriteOperation = "create_global_issuer"
	OperationUpdateGlobalIssuer        WriteOperation = "update_global_issuer"
	OperationUpdateSupportMatrix       WriteOperation = "update_support_matrix"
)

// AllWriteOperations is the complete allowlist, in a stable order.
var AllWriteOperations = []WriteOperation{
	OperationSetOrganizationFeature,
	OperationSetOrganizationOnboarding,
	OperationSetChatAnalysisSettings,
	OperationExtendOrganizationTrial,
	OperationEnableOrganization,
	OperationDisableOrganization,
	OperationCreateGlobalIssuer,
	OperationUpdateGlobalIssuer,
	OperationUpdateSupportMatrix,
}

// PlatformGlobal reports whether an operation has no tenant target. Global
// operations have no customer audit row, so the staff event trail is their
// only durable record.
func (op WriteOperation) PlatformGlobal() bool {
	switch op {
	case OperationCreateGlobalIssuer, OperationUpdateGlobalIssuer, OperationUpdateSupportMatrix:
		return true
	default:
		return false
	}
}

func (op WriteOperation) known() bool {
	return slices.Contains(AllWriteOperations, op)
}

// WriteConfig is the server-side write switch. The zero value disables every
// write: Enabled is the global switch and each operation needs its own switch.
// Both are checked at consent, prepare, approve and execute.
type WriteConfig struct {
	Enabled    bool
	Operations map[WriteOperation]bool
}

// ParseWriteOperations reads a comma-separated operation list. Unknown names
// are an error so a typo cannot silently leave an operation disabled or enabled.
func ParseWriteOperations(raw string) (map[WriteOperation]bool, error) {
	operations := map[WriteOperation]bool{}
	for name := range strings.SplitSeq(raw, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		op := WriteOperation(name)
		if !op.known() {
			return nil, errors.New("unknown admin MCP write operation: " + name)
		}
		operations[op] = true
	}
	return operations, nil
}

// WritesAvailable reports whether consent may offer admin:write at all.
func (c WriteConfig) WritesAvailable() bool {
	if !c.Enabled {
		return false
	}
	for _, op := range AllWriteOperations {
		if c.Operations[op] {
			return true
		}
	}
	return false
}

func (c WriteConfig) OperationEnabled(op WriteOperation) bool {
	return c.Enabled && op.known() && c.Operations[op]
}

// EnabledOperations lists enabled operations in allowlist order.
func (c WriteConfig) EnabledOperations() []WriteOperation {
	enabled := []WriteOperation{}
	for _, op := range AllWriteOperations {
		if c.OperationEnabled(op) {
			enabled = append(enabled, op)
		}
	}
	return enabled
}

var (
	ErrWriteDisabled  = errors.New("this admin write operation is disabled")
	ErrWriteScope     = errors.New("this connection was not granted admin:write; reconnect and approve write access")
	ErrWriteIdentity  = errors.New("verified staff context is unavailable")
	errUnknownWriteOp = errors.New("unknown admin write operation")
)

// writeAuthority is the verified identity a write proposal is bound to. It is
// derived only from the authenticated principal and verifier-produced staff
// context, never from tool arguments.
type writeAuthority struct {
	Principal Principal
	Staff     *contextvalues.AdminAuthContext
	Operation WriteOperation
}

// requireWriteAuthority is the single gate every write tool and approval path
// calls. It checks the operation allowlist, both switches, admin:write and the
// live staff context the authenticator established for this request.
func requireWriteAuthority(ctx context.Context, config WriteConfig, op WriteOperation) (writeAuthority, error) {
	if !op.known() {
		return writeAuthority{}, errUnknownWriteOp
	}
	if !config.OperationEnabled(op) {
		return writeAuthority{}, ErrWriteDisabled
	}
	principal, ok := principalFromContext(ctx)
	if !ok || principal.Subject == "" || principal.ClientID == "" || principal.ClientRowID == "" || principal.ConnectionID == "" || principal.Generation == "" {
		return writeAuthority{}, ErrWriteIdentity
	}
	if !hasReadScope(principal.Scopes) || !slices.Contains(principal.Scopes, ScopeWrite) {
		return writeAuthority{}, ErrWriteScope
	}
	staff, verified := contextvalues.GetAdminAuthContext(ctx)
	if !verified || staff == nil || principal.staff != staff || staff.SessionID == "" || staff.OIDCSubject == "" || staff.Email == "" || staff.Email != principal.Email {
		return writeAuthority{}, ErrWriteIdentity
	}
	return writeAuthority{Principal: principal, Staff: staff, Operation: op}, nil
}

// normalizeRequestedScopes turns an OAuth scope parameter into the stored
// grant. Empty means read-only. admin:write implies admin:read, and is only
// grantable when the server has at least one write operation switched on.
func normalizeRequestedScopes(raw string, writesAvailable bool) ([]string, bool) {
	wantWrite := false
	for scope := range strings.FieldsSeq(raw) {
		switch scope {
		case ScopeRead:
		case ScopeWrite:
			wantWrite = true
		default:
			return nil, false
		}
	}
	if !wantWrite {
		return []string{ScopeRead}, true
	}
	if !writesAvailable {
		return nil, false
	}
	return []string{ScopeRead, ScopeWrite}, true
}
