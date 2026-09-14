package gram

import "github.com/urfave/cli/v2"

// agentEventsIngestFlags are the flags the streams process consumes for the
// agent_events write path.
func agentEventsIngestFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{
			Name:    "disable-clickhouse-agent-event-writes",
			Usage:   "Disable the ClickHouse agent_events subscribers (kill switch for the agent session write path)",
			EnvVars: []string{"GRAM_DISABLE_CLICKHOUSE_AGENT_EVENT_WRITES"},
			Value:   false,
		},
	}
}
