package authz

import (
	"testing"

	"github.com/google/uuid"
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

// A tool with no annotations is a value of its own. A grant with no
// disposition limit covers it, a grant limited to read-only does not, and an
// admin can allow or block it by name. A tool whose annotations are unknown
// carries no disposition, so only a grant with no disposition limit covers it.
func TestUnannotatedToolCall(t *testing.T) {
	t.Parallel()

	server := uuid.New()
	project := uuid.New()
	ref := MCPServer(MCPServerIDOf(server), ProjectIDOf(project))
	limited := func(scope Scope, disposition string) Grant {
		selector := NewSelector(scope, server.String())
		selector[SelectorKeyDisposition] = disposition
		return NewGrantWithSelector(scope, selector)
	}
	grantSets := map[string][]Grant{
		"unlimited":           {NewGrant(ScopeMCPConnect, server.String())},
		"read-only":           {limited(ScopeMCPConnect, DispositionReadOnly)},
		"unannotated by name": {limited(ScopeMCPConnect, DispositionUnannotated)},
		"unannotated blocked": {NewGrant(ScopeMCPConnect, WildcardResource), limited(ScopeMCPBlockedConnect, DispositionUnannotated)},
	}
	cases := map[string]struct {
		call Check
		want map[string]bool
	}{
		"unannotated, generated ref": {
			call: ref.ToolCall("mystery", DispositionUnannotated),
			want: map[string]bool{"unlimited": true, "read-only": false, "unannotated by name": true, "unannotated blocked": false},
		},
		"unannotated, constructed": {
			call: MCPToolCallCheck(server.String(), MCPToolCallDimensions{Tool: "mystery", Disposition: DispositionUnannotated, ProjectID: project.String()}),
			want: map[string]bool{"unlimited": true, "read-only": false, "unannotated by name": true, "unannotated blocked": false},
		},
		"unknown, generated ref": {
			call: ref.ToolCall("uncatalogued", ""),
			want: map[string]bool{"unlimited": true, "read-only": false, "unannotated by name": false, "unannotated blocked": true},
		},
		"unknown, constructed": {
			call: MCPToolCallCheck(server.String(), MCPToolCallDimensions{Tool: "uncatalogued", Disposition: "", ProjectID: project.String()}),
			want: map[string]bool{"unlimited": true, "read-only": false, "unannotated by name": false, "unannotated blocked": true},
		},
	}

	for name, tc := range cases {
		for grants, want := range tc.want {
			allowed, err := GrantsAuthorize(grantSets[grants], tc.call)
			require.NoError(t, err, name+", "+grants)
			require.Equal(t, want, allowed, name+", "+grants)
		}
	}

	require.NoError(t, ValidateSelector(ScopeMCPConnect, limited(ScopeMCPConnect, DispositionUnannotated).Selector))
}
