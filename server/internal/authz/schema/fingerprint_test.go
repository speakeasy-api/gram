package schema_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	s "github.com/speakeasy-api/gram/server/internal/authz/schema"
)

func TestFingerprintTracksTheCompiledModel(t *testing.T) {
	t.Parallel()

	type variant struct {
		readExpr     s.Expr
		readOpts     []s.PermissionOption
		readerDesc   string
		alternatives bool
		blockScope   string
	}
	base := func() variant {
		return variant{
			readExpr:     s.Union(s.Rel("reader"), s.Rel("admin")),
			readOpts:     []s.PermissionOption{s.Checks("org:read"), s.ExcludedBy("read_blocked")},
			readerDesc:   "Read the organization",
			alternatives: false,
			blockScope:   "org:blocked_read",
		}
	}
	compile := func(t *testing.T, v variant) *s.Registry {
		t.Helper()
		opts := []s.TypeOption{
			s.StoredKind("org"), s.WrittenBy("access"),
			s.Grant("reader", s.StoredAs("org:read"), s.Describe(v.readerDesc)),
			s.Grant("admin", s.StoredAs("org:admin"), s.Describe("Administer the organization")),
			s.Block("read_blocked", s.StoredAs(v.blockScope)),
			s.Permission("read", v.readExpr, v.readOpts...),
			s.Permission("administer", s.Rel("admin"), s.Checks("org:admin")),
		}
		if v.alternatives {
			opts = append(opts, s.Alternatives("read_or_administer",
				s.Alternative("organization", "read"), s.Alternative("organization", "administer")))
		}
		reg, err := s.Compile(testConfig, s.NewType("organization", opts...))
		require.NoError(t, err)
		return reg
	}

	first := compile(t, base())
	again := compile(t, base())
	require.Len(t, first.Fingerprint(), 16)
	require.Equal(t, first.Fingerprint(), again.Fingerprint(), "the same declarations give the same model version")

	changes := map[string]func(v *variant){
		"permission expression": func(v *variant) { v.readExpr = s.Rel("reader") },
		"guard": func(v *variant) {
			v.readOpts = []s.PermissionOption{s.Checks("org:read")}
		},
		"stored block":   func(v *variant) { v.blockScope = "org:blocked_admin" },
		"grant metadata": func(v *variant) { v.readerDesc = "View the organization" },
		"alternatives":   func(v *variant) { v.alternatives = true },
	}
	for name, change := range changes {
		v := base()
		change(&v)
		require.NotEqual(t, first.Fingerprint(), compile(t, v).Fingerprint(), "a changed %s changes the model version", name)
	}
}
