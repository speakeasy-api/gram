package oautherr

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsIANARegisteredCode(t *testing.T) {
	t.Parallel()

	for _, code := range []string{
		CodeInvalidGrant,
		CodeInvalidClient,
		CodeAccessDenied,
		CodeInvalidToken,
		CodeUnsupportedTokenType,
		CodeInvalidClientMetadata,
		CodeExpiredToken,
		CodeInvalidTarget,
		CodeUnsupportedPoPKey,
		CodeInvalidAuthorizationDetails,
		CodeUseDPoPNonce,
		CodeInsufficientUserAuthentication,
	} {
		require.True(t, IsIANARegisteredCode(code), code)
	}

	for _, code := range []string{
		"",
		"Invalid_Grant",
		"invalid_grant ",
		"invalid_grant_extra",
		"unauthorized",
		"login_required",
		"request_denied",
	} {
		require.False(t, IsIANARegisteredCode(code), code)
	}
}

// Every Code constant declared in this package must be in registeredCodes,
// and registeredCodes must hold nothing else, so adding a constant without
// registering it (or the reverse) fails here instead of silently narrowing
// what ParseTokenError recognizes.
func TestIANARegistryIsComplete(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	fset := token.NewFileSet()
	declared := map[string]string{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		require.NoError(t, err, name)
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, ident := range vs.Names {
					if !strings.HasPrefix(ident.Name, "Code") {
						continue
					}
					require.Len(t, vs.Values, len(vs.Names), "%s: Code constants must have explicit values", ident.Name)
					lit, ok := vs.Values[i].(*ast.BasicLit)
					require.True(t, ok, "%s: Code constants must be string literals", ident.Name)
					value, err := strconv.Unquote(lit.Value)
					require.NoError(t, err, ident.Name)
					declared[ident.Name] = value
				}
			}
		}
	}
	require.NotEmpty(t, declared)

	for name, value := range declared {
		require.True(t, IsIANARegisteredCode(value), "%s (%q) is declared but not in registeredCodes", name, value)
	}
	require.Len(t, registeredCodes, len(declared), "registeredCodes holds entries with no Code constant")
}
