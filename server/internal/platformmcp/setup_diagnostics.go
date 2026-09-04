package platformmcp

import (
	"context"
	"errors"
	"net"
)

// SetupCategory is the privacy-safe reason an MCP setup needs attention. Values
// are server-authored from typed errors or closed evidence codes; raw provider,
// network, or protocol text must never be used as a category.
type SetupCategory string

const (
	SetupCategoryInvalidURL                     SetupCategory = "invalid_url"
	SetupCategoryUnsafeTargetOrRedirect         SetupCategory = "unsafe_target_or_redirect"
	SetupCategoryUnreachable                    SetupCategory = "unreachable"
	SetupCategoryTimeout                        SetupCategory = "timeout"
	SetupCategoryInvalidMCPResponse             SetupCategory = "invalid_mcp_response"
	SetupCategoryAuthenticationRequired         SetupCategory = "authentication_required"
	SetupCategoryConfigurationRequired          SetupCategory = "configuration_required"
	SetupCategoryOAuthMetadataIncomplete        SetupCategory = "oauth_metadata_incomplete"
	SetupCategoryDynamicRegistrationUnsupported SetupCategory = "dynamic_registration_unsupported"
	SetupCategoryProviderAuthorizationRejected  SetupCategory = "provider_authorization_rejected"
	SetupCategoryTemporarilyUnavailable         SetupCategory = "temporarily_unavailable"
)

var allSetupCategories = []SetupCategory{
	SetupCategoryInvalidURL,
	SetupCategoryUnsafeTargetOrRedirect,
	SetupCategoryUnreachable,
	SetupCategoryTimeout,
	SetupCategoryInvalidMCPResponse,
	SetupCategoryAuthenticationRequired,
	SetupCategoryConfigurationRequired,
	SetupCategoryOAuthMetadataIncomplete,
	SetupCategoryDynamicRegistrationUnsupported,
	SetupCategoryProviderAuthorizationRejected,
	SetupCategoryTemporarilyUnavailable,
}

func setupCategoryEnumValues() []any {
	values := make([]any, 0, len(allSetupCategories))
	for _, category := range allSetupCategories {
		values = append(values, string(category))
	}
	return values
}

type setupDiagnosticError struct {
	category SetupCategory
	cause    error
}

func (e *setupDiagnosticError) Error() string {
	if e == nil || e.cause == nil {
		return "platform mcp setup diagnostic unavailable"
	}
	return e.cause.Error()
}

func (e *setupDiagnosticError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func setupFailure(category SetupCategory, cause error) error {
	if cause == nil {
		return nil
	}
	return &setupDiagnosticError{category: category, cause: cause}
}

func setupCategoryFromError(err error) SetupCategory {
	if diagnostic, ok := errors.AsType[*setupDiagnosticError](err); ok && diagnostic != nil {
		return diagnostic.category
	}
	return ""
}

// sanitizedSetupFailure keeps only a category and the established broad
// sentinel when an HTTP client wraps a redirect or transport failure. This
// prevents target URLs and provider-specific text from crossing the boundary.
func sanitizedSetupFailure(err error) error {
	category := setupCategoryFromError(err)
	if errors.Is(err, ErrDirectRemoteRejected) {
		return setupFailure(category, ErrDirectRemoteRejected)
	}
	return setupFailure(category, ErrDirectRemoteUnavailable)
}

func setupCategoryFromInspection(inspection DirectRemoteInspection) SetupCategory {
	if inspection.Authentication != "authentication_required" {
		return ""
	}
	switch inspection.OAuthDiscovery {
	case "available_dcr":
		return SetupCategoryAuthenticationRequired
	case "available":
		return SetupCategoryDynamicRegistrationUnsupported
	default:
		return SetupCategoryOAuthMetadataIncomplete
	}
}

// ClassifyReadinessProbeFailure separates transport failures from invalid MCP
// responses without exposing the underlying network or protocol error. It is
// exported for provider adapters in child packages; callers receive only the
// closed state and evidence code, never the original error.
func ClassifyReadinessProbeFailure(err error) (ReadinessState, string) {
	if errors.Is(err, context.DeadlineExceeded) {
		return ReadinessUnreachable, "probe_timeout"
	}
	if networkError, ok := errors.AsType[net.Error](err); ok {
		if networkError.Timeout() {
			return ReadinessUnreachable, "probe_timeout"
		}
		return ReadinessUnreachable, "probe_unreachable"
	}
	// Preserve the established readiness state and evidence for MCP negotiation
	// failures that do not expose a typed network cause. More specific producers
	// such as response-size and redirect guards retain their existing evidence.
	return ReadinessUnreachable, "probe_failed"
}

func setupCategoryFromReadiness(readiness Readiness) SetupCategory {
	if readiness.State == ReadinessReady {
		return ""
	}
	switch readiness.EvidenceCode {
	case "invalid_url":
		return SetupCategoryInvalidURL
	case "redirect_rejected", "unsafe_target_or_redirect":
		return SetupCategoryUnsafeTargetOrRedirect
	case "probe_timeout":
		return SetupCategoryTimeout
	case "probe_failed", "probe_unreachable":
		return SetupCategoryUnreachable
	case "invalid_mcp_response", "response_too_large":
		return SetupCategoryInvalidMCPResponse
	case "upstream_authorization_required", "no_valid_authorization":
		return SetupCategoryAuthenticationRequired
	case "required_header_missing", "request_header_not_supported", "multiple_upstream_identity_providers", "upstream_identity_provider_not_configured", "no_reviewed_client":
		return SetupCategoryConfigurationRequired
	case "oauth_metadata_incomplete":
		return SetupCategoryOAuthMetadataIncomplete
	case "dynamic_registration_unsupported":
		return SetupCategoryDynamicRegistrationUnsupported
	case "upstream_authorization_rejected", "provider_authorization_rejected":
		return SetupCategoryProviderAuthorizationRejected
	case "readiness_unavailable":
		return SetupCategoryTemporarilyUnavailable
	}

	switch readiness.State {
	case ReadinessReady:
		return ""
	case ReadinessNeedsProviderSetup:
		return SetupCategoryOAuthMetadataIncomplete
	case ReadinessNeedsGramAuthorization:
		return SetupCategoryAuthenticationRequired
	case ReadinessAuthFailed, ReadinessUnauthorized:
		return SetupCategoryProviderAuthorizationRejected
	case ReadinessNeedsConfiguration:
		return SetupCategoryConfigurationRequired
	case ReadinessUnreachable:
		return SetupCategoryUnreachable
	case ReadinessUnsupported:
		return SetupCategoryInvalidMCPResponse
	case ReadinessGuideUnavailable:
		return ""
	default:
		return SetupCategoryTemporarilyUnavailable
	}
}

func inspectionFailureActions(category SetupCategory) []RepairAction {
	switch category {
	case SetupCategoryInvalidURL, SetupCategoryUnsafeTargetOrRedirect:
		return []RepairAction{{Kind: "review_remote_url", Label: "Use a public HTTPS MCP endpoint without credentials in the URL"}}
	case SetupCategoryInvalidMCPResponse:
		return []RepairAction{{Kind: "review_remote_endpoint", Label: "Check that the URL serves MCP over Streamable HTTP"}}
	case SetupCategoryUnreachable, SetupCategoryTimeout, SetupCategoryTemporarilyUnavailable:
		return []RepairAction{{Kind: "retry_inspection", Label: "Check this MCP server again shortly"}}
	default:
		return nil
	}
}

func inspectionResultActions(category SetupCategory) []RepairAction {
	switch category {
	case SetupCategoryAuthenticationRequired, SetupCategoryConfigurationRequired, SetupCategoryOAuthMetadataIncomplete, SetupCategoryDynamicRegistrationUnsupported, SetupCategoryProviderAuthorizationRejected:
		return []RepairAction{{Kind: "continue_registration", Label: "Choose a project and add this MCP server before finishing its sign-in setup"}}
	default:
		return nil
	}
}

func setupRepairActions(category SetupCategory, state ReadinessState) []RepairAction {
	if state == ReadinessReady && category == "" {
		return []RepairAction{}
	}

	switch category {
	case SetupCategoryInvalidURL, SetupCategoryUnsafeTargetOrRedirect:
		return []RepairAction{{Kind: "review_remote_url", Label: "Review this MCP server's HTTPS URL in the dashboard"}}
	case SetupCategoryInvalidMCPResponse:
		return []RepairAction{{Kind: "retry_readiness", Label: "Check the MCP server endpoint, then check again"}}
	case SetupCategoryAuthenticationRequired, SetupCategoryConfigurationRequired, SetupCategoryOAuthMetadataIncomplete, SetupCategoryDynamicRegistrationUnsupported, SetupCategoryProviderAuthorizationRejected:
		return []RepairAction{{Kind: "continue_dashboard_setup", Label: "Finish this MCP server's source and sign-in setup in the dashboard"}}
	case SetupCategoryUnreachable, SetupCategoryTimeout, SetupCategoryTemporarilyUnavailable:
		return []RepairAction{{Kind: "retry_readiness", Label: "Check again whether this MCP server is working"}}
	}

	return repairActions(state)
}
