package mcp

import (
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
)

// chainingMessage is a failed managed sign-in's text: named takes the service as %[1]s, neutral can't name it.
type chainingMessage struct {
	named   string
	neutral string
}

var (
	chainingIdentityProviderRefused = chainingMessage{
		"Your identity provider didn't allow access to %[1]s. Ask your administrator, or use your own %[1]s account.",
		"Your identity provider didn't allow access to the service. Ask your administrator, or use your own account for the service.",
	}
	chainingServiceRefused = chainingMessage{
		"%[1]s didn't accept your identity provider sign-in. Ask your administrator, or use your own %[1]s account.",
		"The service didn't accept your identity provider sign-in. Ask your administrator, or use your own account for the service.",
	}
	chainingSavedSignInTurnedDown = chainingMessage{
		"Your identity provider turned down your saved sign-in for %[1]s. Reconnect to sign in again. If it keeps happening, ask your administrator, or use your own %[1]s account.",
		"Your identity provider turned down your saved sign-in for the service. Reconnect to sign in again. If it keeps happening, ask your administrator, or use your own account for the service.",
	}
	chainingNotSetUp = chainingMessage{
		"Sign-in to %[1]s through your identity provider isn't set up correctly. Ask your administrator, or use your own %[1]s account.",
		"Sign-in to the service through your identity provider isn't set up correctly. Ask your administrator, or use your own account for the service.",
	}
	chainingMissingPermissions = chainingMessage{
		"Your identity provider sign-in didn't include the permissions %[1]s needs. Ask your administrator, or use your own %[1]s account.",
		"Your identity provider sign-in didn't include the permissions the service needs. Ask your administrator, or use your own account for the service.",
	}
	chainingServiceGrantedLess = chainingMessage{
		"%[1]s accepted your identity provider sign-in but granted fewer permissions than Speakeasy needs. Ask your administrator to check what %[1]s allows, or use your own %[1]s account.",
		"The service accepted your identity provider sign-in but granted fewer permissions than Speakeasy needs. Ask your administrator to check what the service allows, or use your own account for the service.",
	}
	chainingSignInMismatch = chainingMessage{
		"Your identity provider's sign-in for %[1]s didn't match the setup Speakeasy expects. Ask your administrator, or use your own %[1]s account.",
		"Your identity provider's sign-in for the service didn't match the setup Speakeasy expects. Ask your administrator, or use your own account for the service.",
	}
	chainingIdentityProviderUnreadable = chainingMessage{
		"Your identity provider sent an answer Speakeasy couldn't read. Ask your administrator, or use your own %[1]s account.",
		"Your identity provider sent an answer Speakeasy couldn't read. Ask your administrator, or use your own account for the service.",
	}
	chainingServiceUnreadable = chainingMessage{
		"%[1]s sent an answer Speakeasy couldn't read. Ask your administrator, or use your own %[1]s account.",
		"The service sent an answer Speakeasy couldn't read. Ask your administrator, or use your own account for the service.",
	}
	chainingSignInRequired = chainingMessage{
		"A current identity provider sign-in for %[1]s is required. Close this page and start connecting again, or use your own %[1]s account.",
		"A current identity provider sign-in for the service is required. Close this page and start connecting again, or use your own account for the service.",
	}
	chainingSignInPaused = chainingMessage{
		"Your saved company sign-in for %[1]s isn't working right now. Speakeasy keeps trying it every few minutes, so it may start working again by itself. If it doesn't, ask your administrator, or use your own %[1]s account.",
		"Your saved company sign-in for the service isn't working right now. Speakeasy keeps trying it every few minutes, so it may start working again by itself. If it doesn't, ask your administrator, or use your own account for the service.",
	}
	chainingIneligibleAccount = chainingMessage{
		"Your organization can't vouch for your account to %[1]s, for example because your email isn't on one of its verified domains. Ask your administrator, or use your own %[1]s account.",
		"Your organization can't vouch for your account to the service, for example because your email isn't on one of its verified domains. Ask your administrator, or use your own account for the service.",
	}
	chainingUnmanagedAccount = chainingMessage{
		"You're signed in to Speakeasy with a personal account, so Speakeasy can't sign you in to %[1]s. Accounts you created yourself with your work email count as personal.",
		"You're signed in to Speakeasy with a personal account, so Speakeasy can't sign you in to the service. Accounts you created yourself with your work email count as personal.",
	}
)

// text names service when it is set, else reads neutrally.
func (m chainingMessage) text(service string) string {
	if service == "" {
		return m.neutral
	}
	return fmt.Sprintf(m.named, service)
}

// description is an RFC 6750 section 3.1 error_description: named when valid, else neutral.
func (m chainingMessage) description(service string) string {
	if describable(service) {
		if named := m.text(service); oautherr.IsDescriptionText(named) {
			return named
		}
	}
	return m.neutral
}

// chainingVerdict is what a failed managed sign-in records and tells the person.
type chainingVerdict struct {
	// code is the Connection Logs reason; its RefusedBy says who refused, if anyone.
	code mcpconnectionattempts.Code

	// message is nil when the outcome is not definitive, so it reads as unknown.
	message *chainingMessage
}

// chainingKey is a stage and reason; an empty stage matches any stage.
type chainingKey struct {
	stage  identitychaining.Stage
	reason identitychaining.Reason
}

// chainingVerdicts is the one table of what each failed managed sign-in records and says.
var chainingVerdicts = map[chainingKey]chainingVerdict{
	// The identity provider's token exchange.
	{identitychaining.StageExchange, identitychaining.ReasonAccessDenied}:         {mcpconnectionattempts.CodeAccessDenied, &chainingIdentityProviderRefused},
	{identitychaining.StageExchange, identitychaining.ReasonScopePolicyDenied}:    {mcpconnectionattempts.CodeScopePolicyDenied, &chainingIdentityProviderRefused},
	{identitychaining.StageExchange, identitychaining.ReasonInvalidTarget}:        {mcpconnectionattempts.CodeInvalidTarget, &chainingIdentityProviderRefused},
	{identitychaining.StageExchange, identitychaining.ReasonInvalidGrant}:         {mcpconnectionattempts.CodeReauthenticationRequired, &chainingSavedSignInTurnedDown},
	{identitychaining.StageExchange, identitychaining.ReasonInvalidClient}:        {mcpconnectionattempts.CodeConfigurationRequired, &chainingNotSetUp},
	{identitychaining.StageExchange, identitychaining.ReasonMalformedResponse}:    {mcpconnectionattempts.CodeMalformedResponse, &chainingIdentityProviderUnreadable},
	{identitychaining.StageExchange, identitychaining.ReasonUnknownRejection}:     {mcpconnectionattempts.CodeInvalidRemoteSession, nil},
	{identitychaining.StageValidation, identitychaining.ReasonMalformedAssertion}: {mcpconnectionattempts.CodeMalformedAssertion, &chainingSignInMismatch},
	{identitychaining.StageValidation, identitychaining.ReasonInsufficientScope}:  {mcpconnectionattempts.CodeInsufficientScope, &chainingMissingPermissions},
	{identitychaining.StageRedemption, identitychaining.ReasonAccessDenied}:       {mcpconnectionattempts.CodeEMAProviderRejected, &chainingServiceRefused},
	{identitychaining.StageRedemption, identitychaining.ReasonScopePolicyDenied}:  {mcpconnectionattempts.CodeEMAProviderRejected, &chainingServiceRefused},
	{identitychaining.StageRedemption, identitychaining.ReasonInvalidTarget}:      {mcpconnectionattempts.CodeEMAProviderRejected, &chainingServiceRefused},
	{identitychaining.StageRedemption, identitychaining.ReasonInvalidGrant}:       {mcpconnectionattempts.CodeEMAProviderRejected, &chainingServiceRefused},
	{identitychaining.StageRedemption, identitychaining.ReasonUnknownRejection}:   {mcpconnectionattempts.CodeEMAProviderRejected, &chainingServiceRefused},
	{identitychaining.StageRedemption, identitychaining.ReasonInvalidClient}:      {mcpconnectionattempts.CodeConfigurationRequired, &chainingNotSetUp},
	{identitychaining.StageRedemption, identitychaining.ReasonInsufficientScope}:  {mcpconnectionattempts.CodeInsufficientScope, &chainingServiceGrantedLess},
	{identitychaining.StageRedemption, identitychaining.ReasonMalformedResponse}:  {mcpconnectionattempts.CodeMalformedResponse, &chainingServiceUnreadable},
	{"", identitychaining.ReasonConfigurationRequired}:                            {mcpconnectionattempts.CodeConfigurationRequired, &chainingNotSetUp},
	{"", identitychaining.ReasonReauthenticationRequired}:                         {mcpconnectionattempts.CodeReauthenticationRequired, &chainingSignInRequired},
	{"", identitychaining.ReasonIneligibleIdentity}:                               {mcpconnectionattempts.CodeIneligibleIdentity, &chainingIneligibleAccount},
	{"", identitychaining.ReasonUnmanagedAccount}:                                 {mcpconnectionattempts.CodeUnmanagedAccount, &chainingUnmanagedAccount},
}

// chainingVerdictOf looks outcome up in chainingVerdicts; retryable or unlisted outcomes have none.
func chainingVerdictOf(outcome identitychaining.Outcome) (chainingVerdict, bool) {
	if outcome.Retryable {
		return chainingVerdict{code: mcpconnectionattempts.Code{}, message: nil}, false
	}
	verdict, ok := chainingVerdicts[chainingKey{outcome.Stage, outcome.Reason}]
	if !ok {
		verdict, ok = chainingVerdicts[chainingKey{"", outcome.Reason}]
	}
	if ok && outcome.Reason == identitychaining.ReasonConfigurationRequired && outcome.Retained {
		verdict.message = &chainingSignInPaused
	}
	return verdict, ok
}

// memberChainingText prefixes a gateway member's slug, since a gateway can't name the service.
func memberChainingText(slug string, message chainingMessage) string {
	return fmt.Sprintf("Server %q: %s", slug, message.neutral)
}
