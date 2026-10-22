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

// unknownCommandExitCode is the exit code urfave/cli uses when a command has
// no help topic, kept so scripts see the same failure as for any unknown
// command.
const unknownCommandExitCode = 3

// sdkGeneratorCommandMessage returns the error for a top-level command that
// belongs to the SDK generator, and false for any other command. invokedName
// is the name this CLI was run as: run as speakeasy, it is an older install
// that still holds the SDK generator's name.
func sdkGeneratorCommandMessage(command string, invokedName string) (string, bool) {
	if !slices.Contains(sdkGeneratorCommands, command) {
		return "", false
	}
	if invokedName != sharedCommandName {
		return fmt.Sprintf("%q is a command of the Speakeasy SDK generator, not of this CLI.\n"+
			"Run it with the SDK generator instead: speakeasy %s", command, command), true
	}
	return fmt.Sprintf("%q is a command of the Speakeasy SDK generator, not of this CLI.\n"+
		"This speakeasy is an older copy of the Speakeasy AI Control Plane CLI, which is now called speakeasy-ai.\n"+
		"Install speakeasy-ai and remove this copy, so speakeasy runs the SDK generator.", command), true
}
