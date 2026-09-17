package mcp

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
)

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

// The rollout gate hides the endpoint from a workload whose token is fine, so a
// fresh grant would meet the same gate.
func TestIssuerGateFailureReason_WorkloadRolloutDisabled(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("%w: %w", errWorkloadRolloutDisabled, oops.C(oops.CodeNotFound))
	require.Equal(t, "workload_rollout_disabled", issuerGateFailureReason(err))
	require.NotErrorIs(t, err, errCredentialRejected)
}

// The admission-load failure keeps its own label and, like the two above, stays
// off the invalid_token path.
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
