package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDispositionFromHints(t *testing.T) {
	t.Parallel()

	yes, no := new(bool), new(bool)
	*yes = true

	for name, tc := range map[string]struct {
		readOnly, destructive, idempotent, openWorld *bool
		want                                         string
		conflicting                                  bool
	}{
		"no annotations":            {nil, nil, nil, nil, DispositionUnannotated, false},
		"every hint false":          {no, no, no, no, DispositionUnannotated, false},
		"read-only":                 {yes, no, no, no, DispositionReadOnly, false},
		"destructive":               {nil, yes, nil, nil, DispositionDestructive, false},
		"read-only and destructive": {yes, yes, nil, nil, DispositionDestructive, true},
		"read-only and idempotent":  {yes, nil, yes, nil, DispositionReadOnly, false},
		"idempotent":                {nil, no, yes, yes, DispositionIdempotent, false},
		"open-world":                {nil, nil, nil, yes, DispositionOpenWorld, false},
	} {
		got, conflicting := DispositionFromHints(tc.readOnly, tc.destructive, tc.idempotent, tc.openWorld)
		require.Equal(t, tc.want, got, name)
		require.Equal(t, tc.conflicting, conflicting, name)
	}
}
