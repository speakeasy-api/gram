package k8s

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTLSSecretNameForDomainLowercasesLikeSanitizedNames(t *testing.T) {
	t.Parallel()

	require.Equal(t, "example-com-tls", TLSSecretNameForDomain("Example.Com"))
	require.Equal(t, "mcp-example-com-tls", TLSSecretNameForDomain("mcp.example.com"))
}

func TestWellKnownRootIngressNameIsDeterministicAndLengthSafe(t *testing.T) {
	t.Parallel()

	primaryName := strings.Repeat("a", 63)
	first, err := WellKnownRootIngressName(primaryName)
	require.NoError(t, err)
	second, err := WellKnownRootIngressName(primaryName)
	require.NoError(t, err)

	require.Equal(t, first, second)
	require.LessOrEqual(t, len(first), 63)
	require.Contains(t, first, "-wellknown-root-")
}
