package mcptoolexecution

import (
	"errors"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
)

// ClassifyPrincipalCoverage maps one covered call's principal derivation onto
// the bounded identity-coverage classes. Inputs are the source handed to
// DeriveCandidates and its outcome; the class carries no identifier, so it is
// safe as a metric dimension. Unsupported identity stays visibly classified
// by provenance kind rather than disappearing into an unmatched bucket.
func ClassifyPrincipalCoverage(source any, result killswitches.PrincipalCandidateResult, err error) mcpmetrics.KillswitchIdentityClass {
	if err != nil {
		return mcpmetrics.KillswitchIdentityUnavailable
	}
	identity, ok := source.(mcpidentity.Identity)
	if !ok {
		return mcpmetrics.KillswitchIdentityUnattributed
	}
	if result.Kind() == killswitches.PrincipalCandidateResultCandidates {
		return mcpmetrics.KillswitchIdentityActiveUser
	}
	switch identity.Kind {
	case mcpidentity.KindUserSession:
		return mcpmetrics.KillswitchIdentityInactiveUser
	case mcpidentity.KindAnonymous:
		return mcpmetrics.KillswitchIdentityAnonymous
	case mcpidentity.KindAPIKey:
		return mcpmetrics.KillswitchIdentityAPIKey
	case mcpidentity.KindAssistant:
		return mcpmetrics.KillswitchIdentityAssistant
	case mcpidentity.KindChatSession:
		return mcpmetrics.KillswitchIdentityChatSession
	default:
		return mcpmetrics.KillswitchIdentityUnattributed
	}
}

// ClassifyResourceCoverage maps one covered call's resource derivation onto
// the bounded resource-coverage classes. Ownership rejections classify as
// invalid_owner, other failures as unavailable; neither carries an
// identifier or error text.
func ClassifyResourceCoverage(source any, result killswitches.CanonicalizationResult[killswitches.ResourceKey], err error) mcpmetrics.KillswitchResourceClass {
	if errors.Is(err, ErrServerNotInOrganization) {
		return mcpmetrics.KillswitchResourceInvalidOwner
	}
	if err != nil {
		return mcpmetrics.KillswitchResourceUnavailable
	}
	src, ok := source.(ServerSource)
	if !ok {
		return mcpmetrics.KillswitchResourceUnsupportedSurface
	}
	_, supported, keyErr := result.Key()
	if keyErr != nil {
		return mcpmetrics.KillswitchResourceUnavailable
	}
	if supported {
		return mcpmetrics.KillswitchResourceCanonicalServer
	}
	if !src.FrontingServerID.Valid {
		return mcpmetrics.KillswitchResourceLegacyNoServer
	}
	return mcpmetrics.KillswitchResourceUnsupportedSurface
}
