package identityproviderconnections

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/okta"
)

// Connection statuses.
const (
	StatusPending  = "pending"
	StatusVerified = "verified"
	StatusDegraded = "degraded"
	StatusRevoked  = "revoked"
)

// Typed verification reasons, stored comma-joined in last_error.
const (
	ReasonMissingScope    = "missing_scope"
	ReasonMissingRole     = "missing_role"
	ReasonDPoPNotBound    = "dpop_not_bound"
	ReasonKeyNotFetched   = "key_not_fetched"
	ReasonReadFailedApps  = "read_failed:okta.apps.read"
	ReasonReadFailedUsers = "read_failed:okta.users.read"
	ReasonReadFailedGroup = "read_failed:okta.groups.read"
)

// Typed last_error values that are not verification reasons.
const (
	LastErrorCredentialRejected = "credential_rejected" //nolint:gosec // G101 false positive: a typed status value, not a credential.
	LastErrorOktaUnreachable    = "okta_unreachable"
)

var knownReasons = []string{ReasonMissingScope, ReasonMissingRole, ReasonDPoPNotBound, ReasonKeyNotFetched, ReasonReadFailedApps, ReasonReadFailedUsers, ReasonReadFailedGroup}

// ErrCredentialRejected means Okta refused the connection's private_key_jwt
// credential: the client id is wrong or the JWKS is not configured on the app.
var ErrCredentialRejected = errors.New("identityproviderconnections: okta rejected the connection credential")

// verificationOutcome is what one verification run learned from Okta.
type verificationOutcome struct {
	// CredentialProven means Okta minted a token, proving the submitted credential.
	CredentialProven bool
	Status           string
	Granted          []string
	Missing          []string
	DPoPBound        bool
	Reasons          []string
	VerifiedAt       time.Time
}

// lastError renders the outcome as the typed last_error column value.
func (o *verificationOutcome) lastError() string {
	return strings.Join(o.Reasons, ",")
}

// verifyConnection mints a token for the required scopes and confirms each
// granted scope with one cheap read. A credential rejection surfaces
// as ErrCredentialRejected; any other transport failure is returned as-is.
func verifyConnection(ctx context.Context, client okta.Client) (*verificationOutcome, error) {
	scopes, err := client.VerifyScopes(ctx, RequiredOktaScopes)
	if err != nil {
		if isCredentialRejection(err) {
			return nil, ErrCredentialRejected
		}
		return nil, fmt.Errorf("verify okta scopes: %w", err)
	}

	outcome := &verificationOutcome{
		CredentialProven: !scopes.ExpiresAt.IsZero(),
		Status:           StatusVerified,
		Granted:          slices.Clone(scopes.Granted),
		Missing:          slices.Clone(scopes.Missing),
		DPoPBound:        scopes.DPoPBound,
		Reasons:          []string{},
		VerifiedAt:       time.Now().UTC(),
	}
	if len(outcome.Missing) > 0 {
		outcome.Reasons = append(outcome.Reasons, ReasonMissingScope)
	}
	if !scopes.DPoPBound {
		outcome.Reasons = append(outcome.Reasons, ReasonDPoPNotBound)
	}

	if len(outcome.Granted) > 0 {
		failed, err := confirmReads(ctx, client, outcome.Granted)
		if err != nil {
			return nil, err
		}
		outcome.Reasons = append(outcome.Reasons, failed...)
	}

	if len(outcome.Reasons) > 0 {
		outcome.Status = StatusDegraded
	}
	return outcome, nil
}

// confirmReads performs one bounded read per granted scope and returns the
// per-scope reason for each read that failed. A page cap of one still proves
// the read was authorized, so hitting it is not a failure. The users read
// targets the directory independently of application IDs or apps access.
// Only a credential rejection is returned as an error.
func confirmReads(ctx context.Context, client okta.Client, granted []string) ([]string, error) {
	failed := []string{}
	if slices.Contains(granted, "okta.apps.read") {
		_, err := client.ListApps(ctx, okta.ListAppsRequest{Query: "", Status: "", Limit: 1, MaxPages: 0})
		switch {
		case isCredentialRejection(err):
			return nil, ErrCredentialRejected
		case err != nil && !errors.Is(err, okta.ErrTooManyPages):
			failed = append(failed, ReasonReadFailedApps)
			var apiErr *okta.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden && !slices.Contains(failed, ReasonMissingRole) {
				failed = append(failed, ReasonMissingRole)
			}
		}
	}
	if slices.Contains(granted, "okta.users.read") {
		_, err := client.ListUsers(ctx, okta.ListUsersRequest{Limit: 1})
		switch {
		case isCredentialRejection(err):
			return nil, ErrCredentialRejected
		case err != nil && !errors.Is(err, okta.ErrTooManyPages):
			failed = append(failed, ReasonReadFailedUsers)
			var apiErr *okta.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden && !slices.Contains(failed, ReasonMissingRole) {
				failed = append(failed, ReasonMissingRole)
			}
		}
	}
	if slices.Contains(granted, "okta.groups.read") {
		_, err := client.ListGroups(ctx, okta.ListGroupsRequest{Search: "", Limit: 1})
		switch {
		case isCredentialRejection(err):
			return nil, ErrCredentialRejected
		case err != nil && !errors.Is(err, okta.ErrTooManyPages):
			failed = append(failed, ReasonReadFailedGroup)
			var apiErr *okta.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden && !slices.Contains(failed, ReasonMissingRole) {
				failed = append(failed, ReasonMissingRole)
			}
		}
	}
	return failed, nil
}

// isCredentialRejection reports whether Okta refused the client credential
// itself (invalid_client from the token endpoint) rather than a particular
// scope or resource; a management-endpoint 401 is a failed read, not that.
func isCredentialRejection(err error) bool {
	var apiErr *okta.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.ErrorCode == oautherr.CodeInvalidClient
}

// parseLastError splits a stored last_error into its typed reasons and the
// failure marker, if any; unknown tokens are dropped.
func parseLastError(lastError string) (reasons []string, failure string) {
	reasons = []string{}
	if lastError == "" {
		return reasons, ""
	}
	for part := range strings.SplitSeq(lastError, ",") {
		switch {
		case slices.Contains(knownReasons, part):
			reasons = append(reasons, part)
		case part == LastErrorCredentialRejected || part == LastErrorOktaUnreachable:
			failure = part
		}
	}
	return reasons, failure
}

// failureLastError is the last_error a failed verification records: the
// marker, plus key_not_fetched when Okta refused the credential.
func failureLastError(cause error) string {
	if errors.Is(cause, ErrCredentialRejected) {
		return LastErrorCredentialRejected + "," + ReasonKeyNotFetched
	}
	return LastErrorOktaUnreachable
}
