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
// admin can allow or block it by name.
func TestUnannotatedToolCall(t *testing.T) {
	t.Parallel()

	server := uuid.New()
	ref := MCPServer(MCPServerIDOf(server), ProjectIDOf(uuid.New()))
	limited := func(scope Scope, disposition string) Grant {
		selector := NewSelector(scope, server.String())
		selector[SelectorKeyDisposition] = disposition
		return NewGrantWithSelector(scope, selector)
	}
	unannotated := map[string]Check{
		"generated ref":         ref.ToolCall("mystery", ""),
		"constructed tool call": MCPToolCallCheck(server.String(), MCPToolCallDimensions{Tool: "mystery", Disposition: "", ProjectID: uuid.NewString()}),
	}

	for name, call := range unannotated {
		require.Equal(t, DispositionUnannotated, call.Dimensions[SelectorKeyDisposition], name)

		for grants, want := range map[string]bool{
			"unlimited":           true,
			"read-only":           false,
			"unannotated by name": true,
			"unannotated blocked": false,
		} {
			var set []Grant
			switch grants {
			case "unlimited":
				set = []Grant{NewGrant(ScopeMCPConnect, server.String())}
			case "read-only":
				set = []Grant{limited(ScopeMCPConnect, DispositionReadOnly)}
			case "unannotated by name":
				set = []Grant{limited(ScopeMCPConnect, DispositionUnannotated)}
			case "unannotated blocked":
				set = []Grant{NewGrant(ScopeMCPConnect, WildcardResource), limited(ScopeMCPBlockedConnect, DispositionUnannotated)}
			}
			allowed, err := GrantsAuthorize(set, call)
			require.NoError(t, err, name+", "+grants)
			require.Equal(t, want, allowed, name+", "+grants)
		}
	}

	require.NoError(t, ValidateSelector(ScopeMCPConnect, limited(ScopeMCPConnect, DispositionUnannotated).Selector))
}
