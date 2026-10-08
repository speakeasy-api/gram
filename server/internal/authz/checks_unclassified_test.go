package authz

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
)

const unclassifiedServer = "server-1"

func unclassifiedSelector(scope Scope, narrowing map[string]string) Selector {
	selector := NewSelector(scope, unclassifiedServer)
	maps.Copy(selector, narrowing)
	return selector
}

func roleGrant(scope Scope, narrowing map[string]string) Grant {
	return heldBy(precedenceRole, NewGrantWithSelector(scope, unclassifiedSelector(scope, narrowing)))
}

func TestMCPToolCallCheck_NamedToolWithoutDispositionIsUnclassified(t *testing.T) {
	t.Parallel()

	check := MCPToolCallCheck(unclassifiedServer, MCPToolCallDimensions{Tool: "ping", Disposition: "", ProjectID: "project-1"})
	require.Equal(t, DispositionUnclassified, check.Dimensions[SelectorKeyDisposition])

	classified := MCPToolCallCheck(unclassifiedServer, MCPToolCallDimensions{Tool: "list", Disposition: DispositionReadOnly, ProjectID: "project-1"})
	require.Equal(t, DispositionReadOnly, classified.Dimensions[SelectorKeyDisposition])

	// Without a tool the check names no tool to classify, so it stays a
	// server-level check.
	serverLevel := MCPToolCallCheck(unclassifiedServer, MCPToolCallDimensions{Tool: "", Disposition: "", ProjectID: "project-1"})
	require.NotContains(t, serverLevel.Dimensions, SelectorKeyDisposition)
}

// The decision for a tool with no annotations, against the policy shapes an
// administrator can author. Only grants that do not constrain disposition
// reach it; exclusions and principal precedence behave as for any tool.
func TestGrantsAuthorize_UnclassifiedToolMatrix(t *testing.T) {
	t.Parallel()

	readOnly := map[string]string{SelectorKeyDisposition: DispositionReadOnly}
	destructive := map[string]string{SelectorKeyDisposition: DispositionDestructive}
	namedPing := map[string]string{SelectorKeyTool: "ping"}
	namedPingReadOnly := map[string]string{SelectorKeyTool: "ping", SelectorKeyDisposition: DispositionReadOnly}
	projectWide := Selector{
		SelectorKeyResourceKind: ResourceKindMCP,
		SelectorKeyResourceID:   WildcardResource,
		SelectorKeyProjectID:    "project-1",
	}

	cases := []struct {
		name        string
		grants      []Grant
		disposition string
		allowed     bool
	}{
		{name: "read-only allow, unclassified tool", grants: []Grant{roleGrant(ScopeMCPConnect, readOnly)}, disposition: "", allowed: false},
		{name: "tool and read-only in one selector, unclassified tool", grants: []Grant{roleGrant(ScopeMCPConnect, namedPingReadOnly)}, disposition: "", allowed: false},
		{name: "name-only allow, unclassified tool", grants: []Grant{roleGrant(ScopeMCPConnect, namedPing)}, disposition: "", allowed: true},
		{name: "server allow, unclassified tool", grants: []Grant{roleGrant(ScopeMCPConnect, nil)}, disposition: "", allowed: true},
		{name: "project-wide allow, unclassified tool", grants: []Grant{heldBy(precedenceRole, NewGrantWithSelector(ScopeMCPConnect, projectWide))}, disposition: "", allowed: true},
		{name: "read-only allow plus name-only allow, unclassified tool", grants: []Grant{roleGrant(ScopeMCPConnect, readOnly), roleGrant(ScopeMCPConnect, namedPing)}, disposition: "", allowed: true},
		{name: "read-only allow, read-only tool", grants: []Grant{roleGrant(ScopeMCPConnect, readOnly)}, disposition: DispositionReadOnly, allowed: true},
		{name: "read-only allow, destructive tool", grants: []Grant{roleGrant(ScopeMCPConnect, readOnly)}, disposition: DispositionDestructive, allowed: false},
		{name: "server allow plus destructive block, unclassified tool", grants: []Grant{roleGrant(ScopeMCPConnect, nil), roleGrant(ScopeMCPBlockedConnect, destructive)}, disposition: "", allowed: true},
		{name: "server allow plus destructive block, destructive tool", grants: []Grant{roleGrant(ScopeMCPConnect, nil), roleGrant(ScopeMCPBlockedConnect, destructive)}, disposition: DispositionDestructive, allowed: false},
		{name: "server allow plus name block, unclassified tool", grants: []Grant{roleGrant(ScopeMCPConnect, nil), roleGrant(ScopeMCPBlockedConnect, namedPing)}, disposition: "", allowed: false},
		{name: "server allow plus server block, unclassified tool", grants: []Grant{roleGrant(ScopeMCPConnect, nil), roleGrant(ScopeMCPBlockedConnect, nil)}, disposition: "", allowed: false},
		{
			name:        "inherited server block yields to direct name-only grant",
			grants:      []Grant{roleGrant(ScopeMCPBlockedConnect, nil), heldBy(precedenceUser, NewGrantWithSelector(ScopeMCPConnect, unclassifiedSelector(ScopeMCPConnect, namedPing)))},
			disposition: "",
			allowed:     true,
		},
		{
			name:        "inherited server block does not yield to direct read-only grant",
			grants:      []Grant{roleGrant(ScopeMCPBlockedConnect, nil), heldBy(precedenceUser, NewGrantWithSelector(ScopeMCPConnect, unclassifiedSelector(ScopeMCPConnect, readOnly)))},
			disposition: "",
			allowed:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			allowed, err := GrantsAuthorize(tc.grants, MCPToolCallCheck(unclassifiedServer, MCPToolCallDimensions{Tool: "ping", Disposition: tc.disposition, ProjectID: "project-1"}))
			require.NoError(t, err)
			require.Equal(t, tc.allowed, allowed)
		})
	}
}

// Entering a server is a check with no tool, so a disposition-only grant
// still admits the connection itself.
func TestGrantsAuthorize_DispositionGrantStillEntersServer(t *testing.T) {
	t.Parallel()

	grants := []Grant{roleGrant(ScopeMCPConnect, map[string]string{SelectorKeyDisposition: DispositionReadOnly})}

	allowed, err := GrantsAuthorize(grants, MCPCheck(ScopeMCPConnect, unclassifiedServer, "project-1"))
	require.NoError(t, err)
	require.True(t, allowed)
}

// The sentinel exists only on checks. Neither it nor a wildcard can be
// authored as a grant's disposition, so no stored grant matches an
// unclassified tool by disposition.
func TestValidateSelector_RejectsUnauthorableDispositions(t *testing.T) {
	t.Parallel()

	for _, scope := range []Scope{ScopeMCPConnect, ScopeMCPBlockedConnect} {
		for _, disposition := range []string{DispositionUnclassified, WildcardResource, ""} {
			selector := unclassifiedSelector(scope, map[string]string{SelectorKeyDisposition: disposition})
			require.Error(t, ValidateSelector(scope, selector), "scope %s disposition %q", scope, disposition)
		}
	}
}
