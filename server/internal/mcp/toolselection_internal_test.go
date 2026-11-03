package mcp

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
)

func TestIssuerGateCheckUnavailable(t *testing.T) {
	t.Parallel()

	cause := &pgconn.PgError{Code: "57P01"}
	broken := &pgconn.PgError{Code: "42P01"}
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "revocation store down", err: fmt.Errorf("%w: %w", sessiontokens.ErrRevocationUnavailable, errors.New("dial")), want: true},
		{name: "organization lookup", err: fmt.Errorf("%w: %w", errIssuerGateOrgLookup, cause), want: true},
		{name: "issuer lookup", err: fmt.Errorf("%w: %w", errIssuerGateIssuerLookup, cause), want: true},
		{name: "caller profile", err: fmt.Errorf("%w: %w", errIssuerGateCallerProfile, cause), want: true},
		{name: "policy store down", err: fmt.Errorf("%w: %w", errToolSelectionLoad, cause), want: true},
		{name: "agent session load", err: fmt.Errorf("%w: %w", errAgentSessionCredentialLoad, cause), want: true},
		{name: "workload session load", err: fmt.Errorf("%w: %w", errWorkloadSessionCredentialLoad, cause), want: true},
		{name: "workload admission load", err: fmt.Errorf("%w: %w", errWorkloadSessionAdmissionLoad, cause), want: true},
		{name: "broken query", err: fmt.Errorf("%w: %w", errToolSelectionLoad, broken), want: false},
		{name: "malformed stored policy", err: fmt.Errorf("%w: %w", errToolSelectionLoad, errors.New("bad document")), want: false},
		{name: "selection for another endpoint", err: errToolSelectionResourceMismatch, want: false},
		{name: "missing profile row", err: fmt.Errorf("%w: %w", errIssuerGateCallerProfile, pgx.ErrNoRows), want: false},
		{name: "profile for another subject", err: fmt.Errorf("%w: %w", errCredentialRejected, errIssuerGateCallerProfile), want: false},
		{name: "revoked token", err: fmt.Errorf("%w: %w", errCredentialRejected, sessiontokens.ErrTokenRevoked), want: false},
		{name: "bad token", err: errors.New("validate token: bad signature"), want: false},
		{name: "no credentials", err: nil, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, issuerGateCheckUnavailable(tc.err))
		})
	}
}

func TestIssuerGateFailureReason_ToolSelectionLoad(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("%w: %w", errToolSelectionLoad, errors.New("stored policy malformed"))
	require.Equal(t, "tool_selection_load_failed", issuerGateFailureReason(err))
}

// An admission that never reached a decision is not a withdrawn credential, so
// it must not share the label that earns a workload the invalid_token challenge.
func TestIssuerGateFailureReason_WorkloadAdmissionLoad(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("prepare workload session authorization: %w", fmt.Errorf("%w: %w", errWorkloadSessionAdmissionLoad, errors.New("connection reset")))
	require.Equal(t, "workload_admission_load_failed", issuerGateFailureReason(err))
}

// An unreachable revocation store fails the request closed without judging the
// credential, so it neither reads as a bad token nor earns the invalid_token
// challenge that tells a client to discard what it holds.
func TestIssuerGateFailureReason_RevocationUnavailable(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("validate user-session bearer: %w", fmt.Errorf("%w: %w", sessiontokens.ErrRevocationUnavailable, errors.New("dial tcp: connection refused")))
	require.Equal(t, "revocation_check_unavailable", issuerGateFailureReason(err))
	require.NotErrorIs(t, err, errCredentialRejected)
}

// The admission-load failure keeps its own label and, like the revocation
// outage above, stays off the invalid_token path.
func TestIssuerGateFailureReason_WorkloadAdmissionLoadIsNotACredentialRejection(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("prepare workload session authorization: %w", fmt.Errorf("%w: %w", errWorkloadSessionAdmissionLoad, errors.New("connection reset")))
	require.NotErrorIs(t, err, errCredentialRejected)
}

// A withdrawn admission is the credential itself losing its standing, which is
// what invalid_token is for.
func TestIssuerGateFailureReason_WithdrawnAdmissionIsACredentialRejection(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("prepare workload session authorization: %w", fmt.Errorf("%w: %w", errCredentialRejected, oops.C(oops.CodeUnauthorized)))
	require.ErrorIs(t, err, errCredentialRejected)
	require.Equal(t, issuerGateReasonInvalidBearerToken, issuerGateFailureReason(err))
}

// An engine that answers CodeUnexpected has judged nothing, so the workload
// keeps its token even though the error is shareable.
func TestIsCredentialDenial_OnlyDurableDenialCodes(t *testing.T) {
	t.Parallel()

	require.True(t, isCredentialDenial(oops.C(oops.CodeForbidden)))
	require.True(t, isCredentialDenial(oops.C(oops.CodeUnauthorized)))
	require.False(t, isCredentialDenial(oops.C(oops.CodeUnexpected)))
	require.False(t, isCredentialDenial(fmt.Errorf("wrapped: %w", oops.C(oops.CodeUnexpected))))
	require.False(t, isCredentialDenial(errors.New("plain")))
	require.True(t, isCredentialDenial(fmt.Errorf("wrapped: %w", oops.C(oops.CodeForbidden))))
}

func TestCredentialCheckFault(t *testing.T) {
	t.Parallel()

	require.True(t, credentialCheckFault(&pgconn.PgError{Code: "42P01"}))
	require.True(t, credentialCheckFault(fmt.Errorf("wrapped: %w", oops.E(oops.CodeUnexpected, nil, "engine fault"))))
	require.True(t, credentialCheckFault(fmt.Errorf("%w: %w", errWorkloadSessionAdmissionLoad, errors.New("plain"))))
	require.Equal(t, "credential_check_failed", issuerGateFailureReason(&pgconn.PgError{Code: "42P01"}))

	require.False(t, credentialCheckFault(nil))
	require.False(t, credentialCheckFault(oops.C(oops.CodeForbidden)))
	require.False(t, credentialCheckFault(pgx.ErrNoRows))
	require.False(t, credentialCheckFault(fmt.Errorf("%w: %w", errCredentialRejected, oops.C(oops.CodeUnauthorized))))

	require.NotErrorIs(t, markCredentialDenial(&pgconn.PgError{Code: "57P01"}), errCredentialRejected)
	require.ErrorIs(t, markCredentialDenial(oops.C(oops.CodeForbidden)), errCredentialRejected)
}
