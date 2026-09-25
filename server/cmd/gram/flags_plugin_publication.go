package gram

import "github.com/urfave/cli/v2"

const (
	pluginPublicationEmitFlagName    = "plugin-publication-emit-enabled"
	pluginPublicationConsumeFlagName = "plugin-publication-consume-enabled"
)

func pluginPublicationEmitFlag() cli.Flag {
	return &cli.BoolFlag{
		Name:    pluginPublicationEmitFlagName,
		Usage:   "Write durable plugin publication requests when existing marketplace state changes",
		EnvVars: []string{"GRAM_PLUGIN_PUBLICATION_EMIT_ENABLED"},
	}
}

func pluginPublicationConsumeFlag() cli.Flag {
	return &cli.BoolFlag{
		Name:    pluginPublicationConsumeFlagName,
		Usage:   "Consume durable plugin publication requests",
		EnvVars: []string{"GRAM_PLUGIN_PUBLICATION_CONSUME_ENABLED"},
	}
}
