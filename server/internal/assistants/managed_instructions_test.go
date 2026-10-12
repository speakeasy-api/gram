package assistants

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// preRenameInstructions rebuilds the shipped prompt from before the product
// rename, which differed from the current one only in the product name.
func preRenameInstructions() string {
	return strings.ReplaceAll(managedAssistantInstructions, "Speakeasy", "Gram") // brand-ok: rebuilds the pre-rename prompt
}

func TestShippedManagedAssistantInstructionsIncludesCurrent(t *testing.T) {
	t.Parallel()

	sum := sha256.Sum256([]byte(managedAssistantInstructions))
	require.Contains(t, shippedManagedAssistantInstructions, hex.EncodeToString(sum[:]),
		"managed_assistant_instructions.txt changed: add its new hash to shippedManagedAssistantInstructions")
}

func TestCurrentAssistantInstructionsReplacesShippedVersion(t *testing.T) {
	t.Parallel()

	require.Equal(t, managedAssistantInstructions, currentAssistantInstructions(preRenameInstructions()))
	require.Equal(t, managedAssistantInstructions, currentAssistantInstructions(managedAssistantInstructions))
}

func TestCurrentAssistantInstructionsKeepsEditedPrompt(t *testing.T) {
	t.Parallel()

	for _, stored := range []string{"", "You are a custom assistant.", preRenameInstructions() + "\nAlways answer in French."} {
		require.Equal(t, stored, currentAssistantInstructions(stored))
	}
}
