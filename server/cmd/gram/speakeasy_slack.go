package gram

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	slackapi "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/api"
)

// newSpeakeasySlackDirectory returns the directory that resolves which
// connected workspace the Speakeasy Slack app answers for and its bot token.
// Rotating tokens are renewed when the app's OAuth credentials are set.
func newSpeakeasySlackDirectory(c *cli.Context, db *pgxpool.Pool, enc *encryption.Client, guardianPolicy *guardian.Policy, auditLogger *audit.Logger) *slackdirectoryconnections.DirectorySync {
	var refresher slackdirectoryconnections.TokenRefresher
	if id, secret := c.String("slack-client-id"), c.String("slack-client-secret"); id != "" && id != "unset" && secret != "" && secret != "unset" {
		refresher = slackdirectoryconnections.NewOAuthProvider(slackapi.NewClient("", guardianPolicy.PooledClient()), id, secret, "")
	}
	provider := slackdirectoryconnections.NewDirectoryProvider(slackapi.NewClient("", guardianPolicy.PooledClient()))
	return slackdirectoryconnections.NewDirectorySync(db, enc, provider, auditLogger, refresher)
}
