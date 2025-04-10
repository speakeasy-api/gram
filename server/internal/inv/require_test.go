package inv_test

import (
	"errors"
	"testing"

	"github.com/speakeasy-api/gram/internal/inv"
	"github.com/stretchr/testify/require"
)

func TestRequire(t *testing.T) {
	t.Parallel()

	var err error
	inv.Require("valid",
		"err function", func() error { return nil },
		"err value", err,
		"bool function", func() bool { return true },
		"bool value", true,
		"nil value", nil,
	)
}

func TestRequirePanic(t *testing.T) {
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
		require.Panics(t, func() { inv.Require("expected failure", tc.id, tc.value) })
	}
}
