package gram

import "github.com/urfave/cli/v2"

func pluginPublicationEmitFlag() cli.Flag {
	return &cli.BoolFlag{
		Name:    "plugin-publication-emit-enabled",
		Usage:   "Write durable plugin publication requests when existing marketplace state changes",
		EnvVars: []string{"GRAM_PLUGIN_PUBLICATION_EMIT_ENABLED"},
	}
}

func pluginPublicationConsumeFlag() cli.Flag {
	return &cli.BoolFlag{
		Name:    "plugin-publication-consume-enabled",
		Usage:   "Consume durable plugin publication requests",
		EnvVars: []string{"GRAM_PLUGIN_PUBLICATION_CONSUME_ENABLED"},
	}
}
