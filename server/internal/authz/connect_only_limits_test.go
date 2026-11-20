package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func limitedSelector(scope Scope, key, value string) Selector {
	selector := NewSelector(scope, "server-one")
	selector[key] = value
	return selector
}

// A tool or disposition limit is saved on connect rules and connect blocks
// only. Viewing or managing a server names no tool.
func TestValidateSelector_LimitsAreForConnectOnly(t *testing.T) {
	t.Parallel()

	limits := map[string]string{SelectorKeyTool: "search", SelectorKeyDisposition: DispositionReadOnly}
	for key, value := range limits {
		for _, scope := range []Scope{ScopeMCPConnect, ScopeMCPBlockedConnect} {
			require.NoError(t, ValidateSelector(scope, limitedSelector(scope, key, value)), "%s limited by %s", scope, key)
		}
		for _, scope := range []Scope{ScopeMCPRead, ScopeMCPWrite, ScopeMCPBlockedRead, ScopeMCPBlockedWrite} {
			require.ErrorContains(t, ValidateSelector(scope, limitedSelector(scope, key, value)), "is not allowed for scope", "%s limited by %s", scope, key)
		}
	}
}
