package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func unannotatedToolCall() Check {
	return MCPToolCallCheck("server-1", MCPToolCallDimensions{Tool: "lookup", Disposition: "", ProjectID: "project-1"})
}

func TestMCPToolCallCheck_CarriesUndeterminedDimensionsAsUnknown(t *testing.T) {
	t.Parallel()

	check := MCPToolCallCheck("server-1", MCPToolCallDimensions{Tool: "", Disposition: "", ProjectID: "project-1"})
	require.Equal(t, SelectorValueUnknown, check.Dimensions[SelectorKeyTool])
	require.Equal(t, SelectorValueUnknown, check.Dimensions[SelectorKeyDisposition])
	require.Equal(t, "project-1", check.Dimensions[SelectorKeyProjectID])
}

// A grant limited to read-only tools does not allow a tool whose disposition
// cannot be determined.
func TestUnknownDisposition_DoesNotSatisfyAConstrainedAllow(t *testing.T) {
	t.Parallel()

	readOnly := NewSelector(ScopeMCPConnect, "server-1")
	readOnly[SelectorKeyDisposition] = DispositionReadOnly
	allowed, err := GrantsAuthorize([]Grant{NewGrantWithSelector(ScopeMCPConnect, readOnly)}, unannotatedToolCall())
	require.NoError(t, err)
	require.False(t, allowed)

	// An unconstrained grant, or one wildcarding the dimension, still does.
	allowed, err = GrantsAuthorize([]Grant{NewGrant(ScopeMCPConnect, "server-1")}, unannotatedToolCall())
	require.NoError(t, err)
	require.True(t, allowed)

	anyDisposition := NewSelector(ScopeMCPConnect, "server-1")
	anyDisposition[SelectorKeyDisposition] = WildcardResource
	allowed, err = GrantsAuthorize([]Grant{NewGrantWithSelector(ScopeMCPConnect, anyDisposition)}, unannotatedToolCall())
	require.NoError(t, err)
	require.True(t, allowed)
}

// A block limited to destructive tools applies to a tool whose disposition
// cannot be determined, since it might be destructive.
func TestUnknownDisposition_IsCoveredByAConstrainedBlock(t *testing.T) {
	t.Parallel()

	destructive := NewSelector(ScopeMCPBlockedConnect, "server-1")
	destructive[SelectorKeyDisposition] = DispositionDestructive
	grants := []Grant{
		NewGrant(ScopeMCPConnect, "server-1"),
		NewGrantWithSelector(ScopeMCPBlockedConnect, destructive),
	}

	allowed, err := GrantsAuthorize(grants, unannotatedToolCall())
	require.NoError(t, err)
	require.False(t, allowed)

	// A known read-only tool is untouched by the block.
	allowed, err = GrantsAuthorize(grants, MCPToolCallCheck("server-1", MCPToolCallDimensions{Tool: "lookup", Disposition: DispositionReadOnly, ProjectID: "project-1"}))
	require.NoError(t, err)
	require.True(t, allowed)
}

func TestValidateSelector_RejectsTheUnknownMarker(t *testing.T) {
	t.Parallel()

	selector := NewSelector(ScopeMCPConnect, "server-1")
	selector[SelectorKeyTool] = SelectorValueUnknown
	require.Error(t, ValidateSelector(ScopeMCPConnect, selector))
}

func TestCheckDimensions_RejectValuesNoRequestCarries(t *testing.T) {
	t.Parallel()

	withDimension := func(check Check, key, value string) Check {
		dimensions := map[string]string{}
		for k, v := range check.Dimensions {
			dimensions[k] = v
		}
		dimensions[key] = value
		check.Dimensions = dimensions
		return check
	}
	grants := []Grant{NewGrant(ScopeMCPConnect, WildcardResource)}
	cases := map[string]Check{
		"empty tool":              withDimension(unannotatedToolCall(), SelectorKeyTool, ""),
		"wildcard tool":           withDimension(unannotatedToolCall(), SelectorKeyTool, WildcardResource),
		"represented user":        withDimension(unannotatedToolCall(), SelectorKeyTool, SelectorValueRepresentedUser),
		"unknown project":         withDimension(unannotatedToolCall(), SelectorKeyProjectID, SelectorValueUnknown),
		"unknown on an exemption": withDimension(RiskPolicyBypassCheck("policy-1", RiskPolicyDimensions{ServerURL: "https://example.com", ServerIdentity: ""}), SelectorKeyServerIdentity, SelectorValueUnknown),
	}
	for name, check := range cases {
		_, err := GrantsAuthorize(grants, check)
		require.ErrorIs(t, err, ErrInvalidCheck, name)
	}
}
