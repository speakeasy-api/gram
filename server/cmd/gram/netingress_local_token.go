package gram

import (
	"fmt"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/netingress"
)

// defaultLocalTokenTTL is how long a minted local attestation token lasts. A
// day outlasts a working session, and the local stack re-mints it on every
// tailnet gateway start, so a developer never has to renew it by hand.
const defaultLocalTokenTTL = 24 * time.Hour

func newNetingressLocalTokenCommand() *cli.Command {
	return &cli.Command{
		Name:   "netingress-local-token",
		Usage:  "Local development only: mint the attestation token the tailnet gateway presents to the private listener",
		Hidden: true,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "key-file",
				Usage:    "HMAC signing key shared with the private listener; created if missing",
				EnvVars:  []string{"GRAM_NETINGRESS_LOCAL_TOKEN_KEY_FILE"},
				Required: true,
			},
			&cli.StringFlag{
				Name:     "token-file",
				Usage:    "Where to write the token; replaced atomically",
				EnvVars:  []string{"GRAM_TAILNET_GATEWAY_TOKEN_PATH"},
				Required: true,
			},
			&cli.StringFlag{
				Name:     "namespace",
				Usage:    "Service account namespace the token asserts",
				EnvVars:  []string{"GRAM_NETINGRESS_GATEWAY_NAMESPACE"},
				Required: true,
			},
			&cli.StringFlag{
				Name:     "service-account",
				Usage:    "Service account the token asserts",
				EnvVars:  []string{"GRAM_NETINGRESS_GATEWAY_SERVICE_ACCOUNT"},
				Required: true,
			},
			&cli.StringFlag{
				Name:  "audience",
				Usage: "Audience the private listener requires",
				Value: netingress.DefaultTokenAudience,
			},
			&cli.DurationFlag{
				Name:  "ttl",
				Usage: "How long the token stays valid",
				Value: defaultLocalTokenTTL,
			},
		},
		Action: func(c *cli.Context) error {
			key, err := netingress.LoadOrCreateLocalTokenKey(c.String("key-file"))
			if err != nil {
				return fmt.Errorf("load local attestation key: %w", err)
			}

			token, err := netingress.MintLocalToken(key, netingress.LocalTokenClaims{
				Namespace:      c.String("namespace"),
				ServiceAccount: c.String("service-account"),
				Audience:       c.String("audience"),
				TTL:            c.Duration("ttl"),
			}, time.Now())
			if err != nil {
				return fmt.Errorf("mint local attestation token: %w", err)
			}

			if err := netingress.WriteLocalToken(c.String("token-file"), token); err != nil {
				return fmt.Errorf("save local attestation token: %w", err)
			}
			_, err = fmt.Fprintf(c.App.Writer, "wrote local attestation token to %s (valid for %s)\n", c.String("token-file"), c.Duration("ttl"))
			if err != nil {
				return fmt.Errorf("report local attestation token: %w", err)
			}
			return nil
		},
	}
}
