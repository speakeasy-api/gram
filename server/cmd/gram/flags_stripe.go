package gram

import (
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

const (
	stripeMeterEventExportFlagName  = "stripe-meter-event-export-enabled"
	stripeTUMMeterStreamingFlagName = "stripe-tum-meter-streaming"
)

func stripeFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "stripe-api-key",
			Usage:   "The Stripe API key",
			EnvVars: []string{"STRIPE_API_KEY"},
		},
		&cli.StringFlag{
			Name:    "stripe-webhook-secret",
			Usage:   "The Stripe webhook signing secret",
			EnvVars: []string{"STRIPE_WEBHOOK_SECRET"},
		},
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "stripe-price-id-tum",
			Aliases: []string{"stripe.price_id_tum"},
			Usage:   "The Stripe metered TUM price ID",
			EnvVars: []string{"STRIPE_PRICE_ID_TUM"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "stripe-meter-id-tum",
			Aliases: []string{"stripe.meter_id_tum"},
			Usage:   "The Stripe TUM billing meter ID",
			EnvVars: []string{"STRIPE_METER_ID_TUM"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "stripe-meter-event-name",
			Aliases: []string{"stripe.meter_event_name"},
			Usage:   "The Stripe TUM meter event name",
			EnvVars: []string{"STRIPE_METER_EVENT_NAME", "STRIPE_METER_EVENT_NAME_TUM"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "stripe-meter-event-name-mcp-bandwidth-ingress",
			Aliases: []string{"stripe.meter_event_name_mcp_bandwidth_ingress"},
			Usage:   "The Stripe MCP bandwidth ingress meter event name",
			EnvVars: []string{"STRIPE_METER_EVENT_NAME_MCP_BANDWIDTH_INGRESS"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "stripe-meter-event-name-mcp-bandwidth-egress",
			Aliases: []string{"stripe.meter_event_name_mcp_bandwidth_egress"},
			Usage:   "The Stripe MCP bandwidth egress meter event name",
			EnvVars: []string{"STRIPE_METER_EVENT_NAME_MCP_BANDWIDTH_EGRESS"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "stripe-meter-event-name-risk-gitleaks",
			Aliases: []string{"stripe.meter_event_name_risk_gitleaks"},
			Usage:   "The Stripe Gitleaks risk meter event name; empty disables export for this meter",
			EnvVars: []string{"STRIPE_METER_EVENT_NAME_RISK_GITLEAKS"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "stripe-meter-event-name-risk-presidio",
			Aliases: []string{"stripe.meter_event_name_risk_presidio"},
			Usage:   "The Stripe Presidio risk meter event name; empty disables export for this meter",
			EnvVars: []string{"STRIPE_METER_EVENT_NAME_RISK_PRESIDIO"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "stripe-meter-event-name-risk-prompt-injection",
			Aliases: []string{"stripe.meter_event_name_risk_prompt_injection"},
			Usage:   "The Stripe prompt-injection risk meter event name; empty disables export for this meter",
			EnvVars: []string{"STRIPE_METER_EVENT_NAME_RISK_PROMPT_INJECTION"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "stripe-meter-event-name-risk-prompt-policy",
			Aliases: []string{"stripe.meter_event_name_risk_prompt_policy"},
			Usage:   "The Stripe prompt-policy risk meter event name; empty disables export for this meter",
			EnvVars: []string{"STRIPE_METER_EVENT_NAME_RISK_PROMPT_POLICY"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "stripe-meter-event-name-risk-custom-rules",
			Aliases: []string{"stripe.meter_event_name_risk_custom_rules"},
			Usage:   "The Stripe custom-rules risk meter event name; empty disables export for this meter",
			EnvVars: []string{"STRIPE_METER_EVENT_NAME_RISK_CUSTOM_RULES"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "stripe-meter-event-name-risk-cli-destructive",
			Aliases: []string{"stripe.meter_event_name_risk_cli_destructive"},
			Usage:   "The Stripe destructive-command risk meter event name; empty disables export for this meter",
			EnvVars: []string{"STRIPE_METER_EVENT_NAME_RISK_CLI_DESTRUCTIVE"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "stripe-portal-configuration-id",
			Aliases: []string{"stripe.portal_configuration_id"},
			Usage:   "The controlled Stripe customer portal configuration ID",
			EnvVars: []string{"STRIPE_PORTAL_CONFIGURATION_ID"},
		}),
		&cli.BoolFlag{
			Name:    stripeTUMMeterStreamingFlagName,
			Usage:   "Send TUM meter events through Pub/Sub instead of legacy hourly Stripe reporting",
			EnvVars: []string{"GRAM_STRIPE_TUM_METER_STREAMING"},
			Value:   false,
		},
		&cli.BoolFlag{
			Name:    stripeMeterEventExportFlagName,
			Usage:   "Export Pub/Sub meter readings to Stripe; when disabled, acknowledge them without processing",
			EnvVars: []string{"GRAM_STRIPE_METER_EVENT_EXPORT_ENABLED"},
			Value:   false,
		},
	}
}
