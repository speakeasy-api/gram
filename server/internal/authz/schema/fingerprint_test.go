package schema_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	s "github.com/speakeasy-api/gram/server/internal/authz/schema"
)

func TestFingerprintTracksTheCompiledModel(t *testing.T) {
	t.Parallel()

	compile := func(t *testing.T, permission s.Expr) *s.Registry {
		t.Helper()
		reg, err := s.Compile(testConfig,
			s.NewType("organization", s.StoredKind("org"), s.WrittenBy("access"),
				s.Grant("reader", s.StoredAs("org:read"), s.Internal()),
				s.Grant("admin", s.StoredAs("org:admin"), s.Internal()),
				s.Permission("read", permission, s.Checks("org:read")),
				s.Permission("administer", s.Rel("admin"), s.Checks("org:admin")),
			),
		)
		require.NoError(t, err)
		return reg
	}

	first := compile(t, s.Union(s.Rel("reader"), s.Rel("admin")))
	again := compile(t, s.Union(s.Rel("reader"), s.Rel("admin")))
	changed := compile(t, s.Rel("reader"))

	require.Len(t, first.Fingerprint(), 16)
	require.Equal(t, first.Fingerprint(), again.Fingerprint(), "the same declarations give the same model version")
	require.NotEqual(t, first.Fingerprint(), changed.Fingerprint(), "a changed permission changes the model version")
}
