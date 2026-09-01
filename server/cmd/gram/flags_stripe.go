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
			EnvVars: []string{"STRIPE_METER_EVENT_NAME"},
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
