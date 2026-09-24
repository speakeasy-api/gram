package platformmcp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"regexp"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// jsonschema-go reads the whole jsonschema tag as the field description and
// rejects one that begins with "word=", so a tag written as if it took
// key=value directives panics the process during schema inference. Inference
// runs when a tool registers, and most tools register only once their
// dependencies are configured, so a bad tag boots fine in tests with nil
// dependencies and crash-loops the deployment. Scan the source instead, which
// sees every tag regardless of which branch registered its tool.
func TestJSONSchemaTagsAreDescriptions(t *testing.T) {
	t.Parallel()

	directive := regexp.MustCompile(`^\w+=`)

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	require.NoError(t, err)
	require.NotEmpty(t, pkgs)

	tags := 0
	for _, pkg := range pkgs {
		ast.Inspect(pkg, func(node ast.Node) bool {
			field, ok := node.(*ast.Field)
			if !ok || field.Tag == nil {
				return true
			}
			raw, err := strconv.Unquote(field.Tag.Value)
			if err != nil {
				return true
			}
			tag, ok := reflect.StructTag(raw).Lookup("jsonschema")
			if !ok {
				return true
			}
			tags++
			position := fset.Position(field.Tag.Pos())
			require.NotEmpty(t, tag, "%s: empty jsonschema tag", position)
			require.False(t, directive.MatchString(tag),
				"%s: jsonschema tag is a plain description, not key=value directives: %q", position, tag)
			return true
		})
	}
	require.NotZero(t, tags, "the scan found no jsonschema tags, so it is not guarding anything")
}
