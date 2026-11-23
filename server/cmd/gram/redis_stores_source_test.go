package gram

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// redisConstructionFiles may build Redis clients and role adapters. deps.go
// builds the shared pair every process passes around; accountstate_run.go is
// a local CLI that runs outside any process wiring.
var redisConstructionFiles = []string{"deps.go", "accountstate_run.go"}

// redisConstructors are the calls and literals that create a Redis client or
// a role adapter. Building one outside redisConstructionFiles would let a
// component use a private store beside the shared pair, or wrap the sessions
// client as evictable.
var redisConstructors = map[string]bool{
	"cache.NewRedisCacheAdapter":     true,
	"cache.NewEvictableRedisAdapter": true,
	"redis.NewClient":                true,
	"redisrole.EvictableClient":      true,
	"newRedisClient":                 true,
}

// TestRedisStoresAreBuiltOnlyInDeps keeps the role split from being bypassed
// by a second adapter or client built in a process's wiring.
func TestRedisStoresAreBuiltOnlyInDeps(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob("*.go")
	require.NoError(t, err)

	fset := token.NewFileSet()
	var violations []string
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || slices.Contains(redisConstructionFiles, name) {
			continue
		}
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		file, err := parser.ParseFile(fset, name, src, 0)
		require.NoError(t, err)

		ast.Inspect(file, func(node ast.Node) bool {
			var target ast.Expr
			switch n := node.(type) {
			case *ast.CallExpr:
				target = n.Fun
			case *ast.CompositeLit:
				target = n.Type
			default:
				return true
			}
			if callee := exprName(target); redisConstructors[callee] {
				violations = append(violations, fset.Position(node.Pos()).String()+": "+callee)
			}
			return true
		})
	}

	require.Empty(t, violations, "build Redis clients and adapters in deps.go and pass redisStores instead")
}

func exprName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		if pkg, ok := e.X.(*ast.Ident); ok {
			return pkg.Name + "." + e.Sel.Name
		}
	}
	return ""
}
