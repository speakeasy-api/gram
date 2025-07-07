//go:build inv.debug

package inv_test

import (
	"errors"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/stretchr/testify/require"
)

func TestDebug(t *testing.T) {
	t.Parallel()
	var err error
	inv.Debug("valid",
		"err function", func() error { return nil },
		"err value", err,
		"bool function", func() bool { return true },
		"bool value", true,
		"nil value", nil,
	)
}

func TestDebugPanic(t *testing.T) {
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
		require.Panics(t, func() { inv.Debug("expected failure", tc.id, tc.value) })
	}
}
