package gram

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/partition"
)

const partitionFlag = "partition"

// partitionCLIFlag selects the partition every command of this binary runs
// in. It is a global flag, so a command added later gets the partition on its
// logs without registering the flag; commands that set up OpenTelemetry still
// pass PullPartition to o11y.SetupOTelSDK, which rejects an empty partition.
func partitionCLIFlag() cli.Flag {
	return &cli.StringFlag{
		Name:    partitionFlag,
		Value:   partition.Default.String(),
		Usage:   fmt.Sprintf("Partition this deployment runs in, one of: %s. A partition is an independent production deployment with its own data stores and hosts. As a global flag it goes before the command name.", strings.Join(partition.Names(), ", ")),
		EnvVars: []string{"GRAM_PARTITION"},
	}
}

// partitionFromCLI parses the global partition flag. A GRAM_PARTITION that is
// set but empty is rejected rather than treated as the default.
func partitionFromCLI(c *cli.Context) (partition.Partition, error) {
	p, err := partition.Parse(c.String(partitionFlag))
	if err != nil {
		return "", fmt.Errorf("invalid --%s: %w", partitionFlag, err)
	}

	return p, nil
}

type partitionKey struct{}

// PushPartition records the partition the root command parsed, so commands
// read the validated value instead of parsing the flag again.
func PushPartition(ctx context.Context, p partition.Partition) context.Context {
	return context.WithValue(ctx, partitionKey{}, p)
}

// PullPartition returns the partition recorded by PushPartition. It panics
// when none was recorded, because the root command always records one before
// any command runs.
func PullPartition(ctx context.Context) partition.Partition {
	p, ok := ctx.Value(partitionKey{}).(partition.Partition)
	if !ok {
		panic("PullPartition: partition not found in context")
	}

	return p
}
