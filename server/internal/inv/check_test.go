package inv_test

import (
	"errors"
	"testing"

	"github.com/speakeasy-api/gram/internal/inv"
	"github.com/stretchr/testify/require"
)

func TestCheck(t *testing.T) {
	t.Parallel()

	var nilerr error

	err := inv.Check("valid",
		"err function", func() error { return nil },
		"err value", nilerr,
		"bool function", func() bool { return true },
		"bool value", true,
		"nil value", nil,
	)
	require.NoError(t, err)
}

func TestCheckFail(t *testing.T) {
	t.Parallel()

	cases := []struct {
		id    string
		value any
	}{
		{"err function", func() error { return errors.New("simulated") }},
		{"err value", errors.New("simulated")},
		{"bool function", func() bool { return false }},
		{"bool value", false},
	}

	for _, tc := range cases {
		err := inv.Check("expected failure", tc.id, tc.value)
		require.Error(t, err)

		var invErr *inv.InvariantError
		require.ErrorAs(t, err, &invErr)
	}
}

type JoinedError interface {
	Unwrap() []error
}

func TestCheckFailMixed(t *testing.T) {
	t.Parallel()

	cerr := inv.Check("trigger values",
		"err value", errors.New("simulated"),
		"failing check", func() bool { return false },
		"passing check", true,
		"nil value", nil,
	)
	require.Error(t, cerr)

	var err *inv.InvariantError
	require.ErrorAs(t, cerr, &err)

	require.Len(t, err.Failures, 2)
	require.ErrorContains(t, err.Failures[0], "invariant mismatch: trigger values: err value: simulated")
	require.ErrorContains(t, err.Failures[1], "invariant mismatch: trigger values: failing check")
}

func TestCheckWrapsErrors(t *testing.T) {
	t.Parallel()

	simErr := errors.New("simulated")

	cerr := inv.Check("wraps underlying errors", "err value", simErr)
	require.Error(t, cerr)

	var err *inv.InvariantError
	require.ErrorAs(t, cerr, &err)
	require.Len(t, err.Failures, 1)

	require.ErrorIs(t, err.Failures[0], simErr)
}
