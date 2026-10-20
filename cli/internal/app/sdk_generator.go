package app

import (
	"fmt"
	"slices"
)

// sdkGeneratorCommands are top-level commands of the Speakeasy SDK generator
// CLI that this CLI does not have. The SDK generator also installs a binary
// named speakeasy, so one of these reaching this CLI means the caller, often a
// coding agent, expected the other CLI. auth, status and update exist in both
// CLIs, so they are not listed.
var sdkGeneratorCommands = []string{
	"agent",
	"ask",
	"billing",
	"bump",
	"ci",
	"clean",
	"configure",
	"default-code-samples",
	"diff",
	"generate",
	"lint",
	"mcp",
	"merge",
	"migrate",
	"openapi",
	"orphaned",
	"overlay",
	"patches",
	"pull",
	"quickstart",
	"repro",
	"run",
	"suggest",
	"tag",
	"test",
}

// sdkGeneratorInstallCommand installs the SDK generator as speakeasy-sdks, so
// it no longer competes with this CLI for the speakeasy name on PATH.
const sdkGeneratorInstallCommand = "curl -fsSL https://go.speakeasy.com/cli-install.sh | BINARY_NAME=speakeasy-sdks sh"

// unknownCommandExitCode is the exit code urfave/cli uses when a command has
// no help topic, kept so scripts see the same failure as for any unknown
// command.
const unknownCommandExitCode = 3

// sdkGeneratorCommandMessage returns the error for a top-level command that
// belongs to the SDK generator, and false for any other command.
func sdkGeneratorCommandMessage(command string) (string, bool) {
	if !slices.Contains(sdkGeneratorCommands, command) {
		return "", false
	}
	return fmt.Sprintf("%q is a command of the Speakeasy SDK generator, not of this CLI.\n"+
		"This speakeasy is the Speakeasy AI Control Plane CLI. Both CLIs install a binary named speakeasy.\n"+
		"To use the SDK generator alongside it, install it as speakeasy-sdks:\n"+
		"  %s\n"+
		"Then run: speakeasy-sdks %s", command, sdkGeneratorInstallCommand, command), true
}
