package hooks

import (
	"os"
	"strings"
	"testing"

	ahp "github.com/agenthooksprotocol/go-sdk"
	"github.com/stretchr/testify/require"
)

func TestAHPREADMERegistration(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile("README.md")
	require.NoError(t, err)
	for block := range strings.SplitSeq(string(contents), "```json\n") {
		registration, _, found := strings.Cut(block, "```")
		if !found || !strings.Contains(registration, "\"protocolVersion\": \"draft\"") {
			continue
		}
		result := ahp.ParseRegistration([]byte(registration))
		require.True(t, result.OK, result.Diagnostics)
		require.Contains(t, registration, "\"tokenEnv\": \"SPEAKEASY_HOOKS_KEY\"")
		return
	}
	t.Fatal("AHP registration example not found")
}
