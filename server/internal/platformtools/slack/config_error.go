package slack

import (
	"errors"
	"fmt"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/oops"
	slackapi "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/api"
)

// projectAdminMust says who fixes a Slack token read from the server's
// environment, and where.
func projectAdminMust(action string) string {
	return fmt.Sprintf("An admin of the server's project must %s in the server's environment.", action)
}

// slackTokenRejectedCodes are the envelope codes Slack answers with when the
// token itself is missing, invalid, revoked, or of a kind the method refuses.
var slackTokenRejectedCodes = map[string]struct{}{
	"not_authed":             {},
	"invalid_auth":           {},
	"token_revoked":          {},
	"token_expired":          {},
	"account_inactive":       {},
	"not_allowed_token_type": {},
}

// configError turns a failure the server's Slack configuration causes (no
// token, a token Slack rejects, or a token without the scope a method needs)
// into a failed precondition that says what is missing and who can fix it.
// Callers see that message instead of a generic failure. Any other error is
// returned unchanged.
func configError(err error) error {
	if err == nil {
		return nil
	}

	if missing, ok := errors.AsType[*slackapi.TokenNotConfiguredError](err); ok {
		return oops.E(oops.CodeFailedPrecondition, configFaultError{err}, "server not configured: missing environment variable %s. %s", missingTokenVariables(missing), projectAdminMust("add it"))
	}

	slackErr, ok := errors.AsType[*slackapi.Error](err)
	if !ok {
		return err
	}
	if slackErr.Code == "missing_scope" {
		scope := "a scope"
		if slackErr.Needed != "" {
			scope = "the " + slackErr.Needed + " scope"
		}
		return oops.E(oops.CodeFailedPrecondition, configFaultError{err}, "server not configured: its Slack token lacks %s, which %s needs. A Slack workspace admin must add the scope to the Slack app and reinstall the app. %s", scope, slackErr.Method, projectAdminMust("then update the token"))
	}
	if _, rejected := slackTokenRejectedCodes[slackErr.Code]; rejected {
		return oops.E(oops.CodeFailedPrecondition, configFaultError{err}, "server not configured: Slack rejected its token (%s). %s", slackErr.Code, projectAdminMust("replace it"))
	}
	return err
}

// missingTokenVariables names the variable to set first, then the ones also
// read in its place.
func missingTokenVariables(missing *slackapi.TokenNotConfiguredError) string {
	if len(missing.Variables) == 0 {
		return "for the Slack token"
	}
	names := missing.Variables[0]
	if len(missing.Variables) > 1 {
		names = fmt.Sprintf("%s (or %s)", names, strings.Join(missing.Variables[1:], " or "))
	}
	if missing.Kind == slackapi.TokenRequireUser {
		names += ", a Slack user token (search needs its search:read scope)"
	}
	return names
}

// configFaultError marks a configuration failure as the caller's to fix, so error
// boundaries answer it with a 4xx at warn rather than as a Speakeasy fault.
type configFaultError struct {
	err error
}

func (f configFaultError) Error() string { return f.err.Error() }

func (f configFaultError) Unwrap() error { return f.err }

func (configFaultError) ClientFault() bool { return true }
