// Package attemptlog reads connection attempts for one organization and
// project: the Connection Logs list, its counts, one attempt, and the sign-in
// and denied-client summaries. Every read is bounded by organization, project
// and a range of attempt start times inside the 30-day retention.
package attemptlog

import (
	"maps"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
)

// Outcome is how an attempt ended, as these reads report it.
type Outcome string

const (
	OutcomeConnected   Outcome = "connected"
	OutcomeFailed      Outcome = "failed"
	OutcomeBlocked     Outcome = "blocked"
	OutcomeDeclined    Outcome = "declined"
	OutcomeDidntFinish Outcome = "didnt_finish"

	// OutcomeInProgress has no final step and is still inside the live window.
	OutcomeInProgress Outcome = "in_progress"
)

// StepGroup is one of the plain step names the pages show.
type StepGroup string

const (
	StepConnect        StepGroup = "connect"
	StepSignIn         StepGroup = "sign_in"
	StepServiceSignIn  StepGroup = "service_sign_in"
	StepSignInRenewal  StepGroup = "sign_in_renewal"
	StepServiceRenewal StepGroup = "service_renewal"
	StepListingTools   StepGroup = "listing_tools"
	StepRequest        StepGroup = "request"
	StepOther          StepGroup = "other"
)

// signInGroups are the steps the sign-in summary counts as first sign-ins.
var signInGroups = []StepGroup{StepSignIn, StepServiceSignIn}

// renewalGroups are the steps the sign-in summary counts as renewals.
var renewalGroups = []StepGroup{StepSignInRenewal, StepServiceRenewal}

// deniedGroups are the steps where refusing an app counts as denying a client.
var deniedGroups = []StepGroup{StepConnect, StepSignIn, StepSignInRenewal}

// deniedReasons are the refusals of the app itself, by client access rules or an AI app block.
var deniedReasons = []string{
	mcpconnectionattempts.CodeCIMDAdmissionDenied.String(),
	mcpconnectionattempts.CodeAIToolBlocked.String(),
	mcpconnectionattempts.CodeClientNotCIMD.String(),
}

// groupSQL names the StepGroup of a step and grant type expression. A token step with a refresh grant renews a sign-in.
func groupSQL(step, grant string) string {
	return `multiIf(` +
		step + ` = '` + mcpconnectionattempts.StepInboundAuthorize.String() + `', '` + string(StepConnect) + `', ` +
		step + ` IN ('` + mcpconnectionattempts.StepInboundSignIn.String() + `', '` + mcpconnectionattempts.StepInboundConsent.String() + `'), '` + string(StepSignIn) + `', ` +
		step + ` = '` + mcpconnectionattempts.StepInboundToken.String() + `' AND ` + grant + ` = '` + oauthwire.GrantTypeRefreshToken + `', '` + string(StepSignInRenewal) + `', ` +
		step + ` = '` + mcpconnectionattempts.StepInboundToken.String() + `', '` + string(StepSignIn) + `', ` +
		step + ` = '` + mcpconnectionattempts.StepSignInRenewal.String() + `', '` + string(StepSignInRenewal) + `', ` +
		step + ` IN ('` + mcpconnectionattempts.StepProviderSignIn.String() + `', '` + mcpconnectionattempts.StepProviderToken.String() + `', '` + mcpconnectionattempts.StepUpstreamEMA.String() + `'), '` + string(StepServiceSignIn) + `', ` +
		step + ` = '` + mcpconnectionattempts.StepProviderRefresh.String() + `', '` + string(StepServiceRenewal) + `', ` +
		step + ` IN ('` + mcpconnectionattempts.StepDiscoveryListing.String() + `', '` + mcpconnectionattempts.StepDiscoveryMember.String() + `'), '` + string(StepListingTools) + `', ` +
		step + ` = '` + mcpconnectionattempts.StepMCPRequest.String() + `', '` + string(StepRequest) + `', ` +
		`'` + string(StepOther) + `')`
}

// Source is the party that returned a failure.
type Source string

const (
	SourceService Source = "service"

	// SourceServer is the MCP server itself, rejecting a credential Speakeasy sent it.
	SourceServer           Source = "server"
	SourceApp              Source = "app"
	SourceIdentityProvider Source = "identity_provider"
	SourceSpeakeasySetting Source = "speakeasy_setting"
	SourceSpeakeasy        Source = "speakeasy"
)

// unattributedReasons are recorded for more than one party, so they name none.
var unattributedReasons = []string{
	mcpconnectionattempts.CodeSharedCredentialRejected.String(),
	mcpconnectionattempts.CodeSignInProviderError.String(),
	mcpconnectionattempts.CodeProviderDenied.String(),
	mcpconnectionattempts.CodeIssuerMismatch.String(),
	mcpconnectionattempts.CodeProviderRateLimited.String(),
	mcpconnectionattempts.CodeProviderServerError.String(),
	mcpconnectionattempts.CodeStaticKeyRejected.String(),
}

// serverReasons come back from the MCP server Speakeasy connected to.
var serverReasons = []string{
	mcpconnectionattempts.CodeRejectedByMember.String(),
}

// identityProviderReasons come back from the organization's sign-in service.
var identityProviderReasons = []string{
	mcpconnectionattempts.CodeIDPUnavailable.String(),
	mcpconnectionattempts.CodeAccessDenied.String(),
	mcpconnectionattempts.CodeScopePolicyDenied.String(),
	mcpconnectionattempts.CodeInvalidTarget.String(),
	mcpconnectionattempts.CodeIDPTransientFailure.String(),
	mcpconnectionattempts.CodeIneligibleIdentity.String(),
	mcpconnectionattempts.CodeOfflineUnsupported.String(),
	mcpconnectionattempts.CodeRenewalRefused.String(),
	mcpconnectionattempts.CodeRenewalRetryable.String(),
	mcpconnectionattempts.CodeInvalidIdentity.String(),
}

// serviceReasons come back from the outside service.
var serviceReasons = []string{
	mcpconnectionattempts.CodeProviderOAuthInvalidClient.String(),
	mcpconnectionattempts.CodeProviderOAuthInvalidScope.String(),
	mcpconnectionattempts.CodeProviderOAuthInvalidGrant.String(),
	mcpconnectionattempts.CodeUnrenewableToken.String(),
	mcpconnectionattempts.CodeProviderUnavailable.String(),
	mcpconnectionattempts.CodeProviderUnreachable.String(),
	mcpconnectionattempts.CodeEMAProviderRejected.String(),
	mcpconnectionattempts.CodeProviderTransientFailure.String(),
	mcpconnectionattempts.CodeRemoteSessionUnavailable.String(),
	mcpconnectionattempts.CodeProviderRejected.String(),
	mcpconnectionattempts.CodeUpstreamEMARefused.String(),
	mcpconnectionattempts.CodeInvalidGrant.String(),
	mcpconnectionattempts.CodeRefreshInvalidClient.String(),
	mcpconnectionattempts.CodeRefreshRejected.String(),
}

// SourceOf names who returned a failure with this reason, derived from the code
// since rows do not store it. Successes, live attempts and the person's own doing have none.
func SourceOf(reason string, outcome Outcome) Source {
	if reason == "" || outcome == OutcomeConnected || outcome == OutcomeInProgress {
		return ""
	}
	if slices.Contains(unattributedReasons, reason) {
		return ""
	}
	if slices.Contains(identityProviderReasons, reason) {
		return SourceIdentityProvider
	}
	if slices.Contains(serverReasons, reason) {
		return SourceServer
	}
	if slices.Contains(serviceReasons, reason) {
		return SourceService
	}
	meaning, ok := mcpconnectionattempts.MeaningOf(reason)
	if !ok {
		return SourceSpeakeasy
	}
	switch meaning.WhoFixes {
	case mcpconnectionattempts.WhoApp:
		return SourceApp
	case mcpconnectionattempts.WhoProvider:
		return SourceService
	case mcpconnectionattempts.WhoAdmin:
		return SourceSpeakeasySetting
	case mcpconnectionattempts.WhoSpeakeasy:
		return SourceSpeakeasy
	case mcpconnectionattempts.WhoPerson, mcpconnectionattempts.WhoNobody:
		return ""
	default:
		return SourceSpeakeasy
	}
}

// MessageOf is the administrator's sentence for a reason, empty when the reason is unknown or empty.
func MessageOf(reason string) string {
	meaning, ok := mcpconnectionattempts.MeaningOf(reason)
	if !ok {
		return ""
	}
	return meaning.WhatHappenedForAdmin
}

// TechnicalOf is the precise technical fact for a reason, empty when there is none.
func TechnicalOf(reason string) string {
	meaning, ok := mcpconnectionattempts.MeaningOf(reason)
	if !ok {
		return ""
	}
	return meaning.Technical
}

// UnlistedAppName is shown for an app that is not on the family list.
const UnlistedAppName = "Unlisted app"

// appNames are the display names of the client families.
var appNames = map[string]string{
	mcpconnectionattempts.FamilyCodexCLI:        "Codex",
	mcpconnectionattempts.FamilyClaudeCode:      "Claude Code",
	mcpconnectionattempts.FamilyClaudeAI:        "Claude",
	mcpconnectionattempts.FamilyOpenAIConnector: "ChatGPT",
	mcpconnectionattempts.FamilyCursor:          "Cursor",
	mcpconnectionattempts.FamilyVSCode:          "VS Code",
	mcpconnectionattempts.FamilyPerplexity:      "Perplexity",
	mcpconnectionattempts.FamilyScript:          "Script",
	mcpconnectionattempts.FamilyBrowser:         "Browser",
	mcpconnectionattempts.FamilyCopilot:         "GitHub Copilot",
	mcpconnectionattempts.FamilyZed:             "Zed",
	mcpconnectionattempts.FamilyKiro:            "Kiro",
	mcpconnectionattempts.FamilyReplit:          "Replit",
	mcpconnectionattempts.FamilyNotion:          "Notion",
}

// AppFamily is the family a filter and a row use: empty for an app not on the list.
func AppFamily(family string) string {
	if _, ok := appNames[family]; ok {
		return family
	}
	return ""
}

// AppName is the display name of a family.
func AppName(family string) string {
	if name, ok := appNames[family]; ok {
		return name
	}
	return UnlistedAppName
}

// listedFamilies are the families with a name, for the SQL that maps others to empty.
func listedFamilies() []string {
	return slices.Sorted(maps.Keys(appNames))
}

// NetworkLabel is the short form of a network key shown on the pages.
func NetworkLabel(key string) string {
	const labelChars = 4
	if len(key) <= labelChars {
		return key
	}
	return key[:labelChars]
}

// reasonsWithOutcome are the catalog codes that mean outcome when they end an attempt.
func reasonsWithOutcome(outcome mcpconnectionattempts.Outcome) []string {
	var codes []string
	for _, code := range mcpconnectionattempts.Codes() {
		if meaning, ok := mcpconnectionattempts.MeaningOf(code); ok && meaning.Outcome == outcome {
			codes = append(codes, code)
		}
	}
	return codes
}
