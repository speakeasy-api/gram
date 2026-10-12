package background

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type stubIssuerKeySweeper struct {
	swept int
	err   error
}

func (s stubIssuerKeySweeper) SweepAllEMAIssuerKeys(context.Context) (int, error) {
	return s.swept, s.err
}

func TestInternalSessionIssuerKeySweep(t *testing.T) {
	t.Parallel()
	logger := testenv.NewLogger(t)

	swept, err := NewInternalSessionIssuerKeySweepActivities(logger, nil).Sweep(t.Context())
	require.NoError(t, err, "a worker without KMS provisioning sweeps nothing")
	require.Zero(t, swept)

	swept, err = NewInternalSessionIssuerKeySweepActivities(logger, stubIssuerKeySweeper{swept: 3, err: nil}).Sweep(t.Context())
	require.NoError(t, err)
	require.Equal(t, 3, swept)

	failure := errors.New("kms unavailable")
	_, err = NewInternalSessionIssuerKeySweepActivities(logger, stubIssuerKeySweeper{swept: 1, err: failure}).Sweep(t.Context())
	require.ErrorIs(t, err, failure, "a failed sweep surfaces so Temporal retries it")

}
