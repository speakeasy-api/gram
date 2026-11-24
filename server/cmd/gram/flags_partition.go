package gram

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/partition"
)

const partitionFlag = "partition"

// partitionCLIFlag selects the partition every command of this binary runs
// in. It has no default: a deployment that forgets it fails at startup rather
// than running as the wrong partition. It is a global flag, so a command added
// later requires it and gets the partition on its logs without registering the
// flag; commands that set up OpenTelemetry still pass PullPartition to
// o11y.SetupOTelSDK, which rejects an empty partition.
func partitionCLIFlag() cli.Flag {
	return &cli.StringFlag{
		Name:    partitionFlag,
		Usage:   fmt.Sprintf("Partition this deployment runs in, one of: %s. Required. A partition is an independent production deployment with its own data stores and hosts. As a global flag it goes before the command name.", strings.Join(partition.Names(), ", ")),
		EnvVars: []string{"GRAM_PARTITION"},
	}
}

// partitionFromCLI parses the global partition flag. It returns an empty
// partition and no error only for invocations that print help or the version,
// which must work without deployment configuration. A GRAM_PARTITION that is
// set but empty is rejected.
func partitionFromCLI(c *cli.Context) (partition.Partition, error) {
	if !c.IsSet(partitionFlag) {
		if printsHelpOrVersion(c.Args().Slice()) {
			return "", nil
		}
		return "", fmt.Errorf("--%s (GRAM_PARTITION) is required: set it to one of %s", partitionFlag, strings.Join(partition.Names(), ", "))
	}

	p, err := partition.Parse(c.String(partitionFlag))
	if err != nil {
		return "", fmt.Errorf("invalid --%s: %w", partitionFlag, err)
	}

	return p, nil
}

// printsHelpOrVersion reports whether the arguments after the global flags
// only print help or the version: no command, the help or version command, or
// a help flag. Arguments after a "--" terminator are positional, never flags,
// so a --help there does not exempt a command from requiring a partition.
func printsHelpOrVersion(args []string) bool {
	if len(args) == 0 {
		return true
	}

	switch args[0] {
	case "help", "h", "version":
		return true
	}

	if end := slices.Index(args, "--"); end >= 0 {
		args = args[:end]
	}

	return slices.ContainsFunc(args, func(arg string) bool {
		return arg == "--help" || arg == "-h"
	})
}

type partitionKey struct{}

// PushPartition records the partition the root command parsed, so commands
// read the validated value instead of parsing the flag again.
func PushPartition(ctx context.Context, p partition.Partition) context.Context {
	return context.WithValue(ctx, partitionKey{}, p)
}

// PullPartition returns the partition recorded by PushPartition. It panics
// when none was recorded, because the root command records one before any
// command other than help or version runs.
func PullPartition(ctx context.Context) partition.Partition {
	p, ok := ctx.Value(partitionKey{}).(partition.Partition)
	if !ok {
		panic("PullPartition: partition not found in context")
	}

	return p
}
