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
		excluded     bool
	}
	base := func() variant {
		return variant{
			readExpr:     s.Union(s.Rel("org:read"), s.Rel("org:admin")),
			readOpts:     []s.PermissionOption{s.AnswersScope("org:read")},
			readerDesc:   "Read the organization",
			alternatives: false,
			blockScope:   "org:blocked_read",
			excluded:     true,
		}
	}
	compile := func(t *testing.T, v variant) *s.Registry {
		t.Helper()
		readOpts := v.readOpts
		if v.excluded {
			readOpts = append(readOpts, s.ExcludedBy(v.blockScope))
		}
		opts := []s.TypeOption{
			s.StoredKind("org"), s.GrantSurface("access"),
			s.Grant("org:read", s.GrantSpec{Description: v.readerDesc}),
			s.Grant("org:admin", s.GrantSpec{Description: "Administer the organization"}),
			s.Block(v.blockScope, s.BlockSpec{}),
			s.Permission("read", v.readExpr, readOpts...),
			s.Permission("administer", s.Rel("org:admin"), s.AnswersScope("org:admin")),
		}
		if v.alternatives {
			opts = append(opts, s.Alternatives("read_or_administer", s.AlternativesSpec{Options: []s.PermissionRef{{Type: "organization", Permission: "read"}, {Type: "organization", Permission: "administer"}}, Blocks: s.BlocksApplyPerOption}))
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
		"permission expression": func(v *variant) { v.readExpr = s.Rel("org:read") },
		"guard":                 func(v *variant) { v.excluded = false },
		"stored block":          func(v *variant) { v.blockScope = "org:blocked_admin" },
		"grant metadata":        func(v *variant) { v.readerDesc = "View the organization" },
		"alternatives":          func(v *variant) { v.alternatives = true },
	}
	for name, change := range changes {
		v := base()
		change(&v)
		require.NotEqual(t, first.Fingerprint(), compile(t, v).Fingerprint(), "a changed %s changes the model version", name)
	}
}
