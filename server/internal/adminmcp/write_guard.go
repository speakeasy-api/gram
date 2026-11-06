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

// WriteOperation names one staff mutation. The runtime never dispatches a
// mutation that is not declared here.
type WriteOperation string

const (
	// OperationCurateRegistryPromotionCandidate changes staff review status for an unpublished candidate.
	OperationCurateRegistryPromotionCandidate     WriteOperation = "curate_registry_promotion_candidate"
	OperationRetryRegistryPromotionDocs           WriteOperation = "retry_registry_promotion_docs"
	OperationPromoteRegistryCandidate             WriteOperation = "promote_registry_candidate"
	OperationSetOrganizationFeature               WriteOperation = "set_organization_feature"
	OperationSetOrganizationWhitelist             WriteOperation = "set_organization_whitelist"
	OperationAssignOrganizationOnboardingPlaybook WriteOperation = "assign_organization_onboarding_playbook"
	OperationSetChatAnalysisSettings              WriteOperation = "set_organization_chat_analysis_settings"
	OperationExtendOrganizationTrial              WriteOperation = "extend_organization_trial"
	OperationEnableOrganization                   WriteOperation = "enable_organization"
	OperationDisableOrganization                  WriteOperation = "disable_organization"
	OperationUpdateSupportMatrix                  WriteOperation = "update_support_matrix"
	OperationCreateRegistryEntry                  WriteOperation = "create_registry_entry"
	OperationSetRegistryIcons                     WriteOperation = "set_registry_icons"
	OperationUploadPlatformImage                  WriteOperation = "upload_platform_image"
)

// AllWriteOperations lists every write operation, in a stable order.
var AllWriteOperations = []WriteOperation{
	OperationCurateRegistryPromotionCandidate,
	OperationRetryRegistryPromotionDocs,
	OperationPromoteRegistryCandidate,
	OperationSetOrganizationFeature,
	OperationSetOrganizationWhitelist,
	OperationAssignOrganizationOnboardingPlaybook,
	OperationSetChatAnalysisSettings,
	OperationExtendOrganizationTrial,
	OperationEnableOrganization,
	OperationDisableOrganization,
	OperationUpdateSupportMatrix,
	OperationCreateRegistryEntry,
	OperationSetRegistryIcons,
	OperationUploadPlatformImage,
}

func (op WriteOperation) known() bool {
	return slices.Contains(AllWriteOperations, op)
}

// WriteConfig is the server-side write switch. The zero value disables every
// write. It is checked at consent and on every write.
type WriteConfig struct {
	Enabled bool
}

var (
	ErrWriteDisabled  = errors.New("admin writes are disabled on this server")
	ErrWriteScope     = errors.New("this connection was not granted admin:write; reconnect and approve write access")
	ErrWriteIdentity  = errors.New("verified staff context is unavailable")
	errUnknownWriteOp = errors.New("unknown admin write operation")
)

// writeAuthority is the verified identity making a write. It is derived only from the authenticated principal and verifier-produced staff
// context, never from tool arguments.
type writeAuthority struct {
	Principal Principal
	Staff     *contextvalues.AdminAuthContext
	Operation WriteOperation
}

// requireWriteAuthority is the single gate every write tool calls. It checks the operation is known, the write switch, admin:write and the
// live staff context the authenticator established for this request.
func requireWriteAuthority(ctx context.Context, config WriteConfig, op WriteOperation) (writeAuthority, error) {
	if !op.known() {
		return writeAuthority{}, errUnknownWriteOp
	}
	if !config.Enabled {
		return writeAuthority{}, ErrWriteDisabled
	}
	authority, err := requireWriteIdentity(ctx)
	if err != nil {
		return writeAuthority{}, err
	}
	authority.Operation = op
	return authority, nil
}

// requireWriteIdentity checks admin:write and the live staff context.
func requireWriteIdentity(ctx context.Context) (writeAuthority, error) {
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
	return writeAuthority{Principal: principal, Staff: staff, Operation: ""}, nil
}

// normalizeRequestedScopes turns an OAuth scope parameter into the stored
// grant. Empty means read-only. admin:write implies admin:read, and is only
// grantable when writes are switched on.
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
