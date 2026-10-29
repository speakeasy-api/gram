package relschema

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func testDefinition() Definition {
	return Definition{
		Types: []*Type{
			NewType("org", StoredKind("*"),
				Grant("admin", StoredAs("root"), AnchoredAt("org")),
			),
			NewType("doc", Parent("org"), StoredKind("doc"),
				Grant("reader", StoredAs("doc:read"), AnchoredAt("doc", "org")),
				Block("blocked", StoredAs("doc:blocked_read"), AnchoredAt("doc")),
				Permission("read", Union(Rel("reader"), AncestorRel("org", "admin")), ExcludedBy("blocked")),
			),
		},
		Root:        "org",
		KnownScopes: []string{"root", "doc:read", "doc:blocked_read", "other:read"},
		OutOfSchema: []string{"other:read"},
	}
}

func TestCompile_ValidDefinition(t *testing.T) {
	t.Parallel()
	s, err := Compile(testDefinition())
	require.NoError(t, err)
	require.Equal(t, []string{"org"}, s.Types["doc"].Chain())
}

func TestCompile_RejectsInvalidDefinitions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*Definition)
		want   string
	}{
		{"scope neither mapped nor out of schema", func(d *Definition) { d.KnownScopes = append(d.KnownScopes, "new:scope") }, "neither mapped"},
		{"scope both mapped and out of schema", func(d *Definition) { d.OutOfSchema = append(d.OutOfSchema, "doc:read") }, "both mapped"},
		{"exclusion that is not a block", func(d *Definition) {
			Permission("read", Rel("blocked"), ExcludedBy("reader"))(d.Types[1])
		}, "not a block"},
		{"anchor that is not an ancestor", func(d *Definition) { d.Types[1].Grants["reader"].Anchors = []string{"elsewhere"} }, "not the type or an ancestor"},
		{"narrowing without a selector key", func(d *Definition) { d.Types[1].Grants["reader"].Narrowable = []string{"org"} }, "no selector key"},
		{"unknown relation", func(d *Definition) { Permission("read", Rel("writer"))(d.Types[1]) }, "unknown relation"},
		{"chain not ending at root", func(d *Definition) { d.Types[1].Parent = "" }, "does not end at"},
		{"scope mapped twice", func(d *Definition) { d.Types[1].Grants["blocked"].StoredScope = "doc:read" }, "mapped twice"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			def := testDefinition()
			tc.mutate(&def)
			_, err := Compile(def)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestDecode_ConcreteRowKeepsItsAncestorRestriction(t *testing.T) {
	t.Parallel()
	def := testDefinition()
	def.AncestorKeys = map[string]string{"org_id": "org"}
	def.Types[1].Grants["reader"].Narrowable = []string{"org"}
	s, err := Compile(def)
	require.NoError(t, err)

	rel, mapped, err := s.Decode("o1", Row{Principal: "user:u", Scope: "doc:read", Selector: map[string]string{
		"resource_kind": "doc", "resource_id": "d1", "org_id": "o2",
	}}, DecodeOptions{Strict: false})
	require.NoError(t, err)
	require.True(t, mapped)
	require.Equal(t, ObjectRef{Type: "doc", ID: "d1"}, rel.Anchor)
	require.Equal(t, map[string]string{"org": "o2"}, rel.Narrowing)

	obj := Object{Type: "doc", ID: "d1", Ancestors: []ObjectRef{{Type: "org", ID: "o1"}}}
	require.Equal(t, Denied, s.Evaluate("o1", []Relationship{rel}, obj, "read", Request{Facts: nil}).Outcome)
}

func TestDecode_OutOfSchemaAndUnknownScopes(t *testing.T) {
	t.Parallel()
	s, err := Compile(testDefinition())
	require.NoError(t, err)

	_, mapped, err := s.Decode("o1", Row{Principal: "user:u", Scope: "other:read", Selector: nil}, DecodeOptions{Strict: false})
	require.NoError(t, err)
	require.False(t, mapped)

	_, _, err = s.Decode("o1", Row{Principal: "user:u", Scope: "never:declared", Selector: nil}, DecodeOptions{Strict: false})
	require.ErrorContains(t, err, "no schema mapping")
}

func TestEvaluate_MalformedObjectIsUndetermined(t *testing.T) {
	t.Parallel()
	s, err := Compile(testDefinition())
	require.NoError(t, err)
	rels := []Relationship{{Type: "org", Grant: "admin", Anchor: ObjectRef{Type: "org", ID: "o1"}}}

	missingAncestor := Object{Type: "doc", ID: "d1", Ancestors: nil}
	require.Equal(t, Undetermined, s.Evaluate("o1", rels, missingAncestor, "read", Request{Facts: nil}).Outcome)

	ok := Object{Type: "doc", ID: "d1", Ancestors: []ObjectRef{{Type: "org", ID: "o1"}}}
	require.Equal(t, Allowed, s.Evaluate("o1", rels, ok, "read", Request{Facts: nil}).Outcome)
	require.Equal(t, Undetermined, s.Evaluate("", rels, ok, "read", Request{Facts: nil}).Outcome)
}
