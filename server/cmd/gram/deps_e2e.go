package gram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os/exec"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/demoseed"
	"github.com/speakeasy-api/gram/server/internal/e2econtrol"
	"github.com/speakeasy-api/gram/server/internal/encryption"
)

// e2eUserEmail is the identity the control surface signs in. Locally it
// defaults to the developer the dev-idp and the local seed both derive from
// `git config user.email`, so a fresh clone needs no extra setting.
func e2eUserEmail(ctx context.Context, environment, configured string) string {
	if configured != "" || environment != "local" {
		return configured
	}
	out, err := exec.CommandContext(ctx, "git", "config", "--get", "user.email").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// newE2EReseeder returns what e2e.reseed runs in this environment, or nil when
// none is wired, in which case e2e.reseed and e2e.tenant answer 501.
//
// Locally the test organization is the local development organization: the
// reseed is the demo seed retargeted at demoseed.LocalSpec plus the local
// fixtures, the same Go that `mise run seed` runs.
//
// Deployed environments are not wired in this proposal. They need a Spec for
// a dedicated test organization (derived from its WorkOS organization id, as
// LocalSpec is from the dev-idp's) and the member-adoption half of the local
// fixtures generalized beyond LocalSpec.
func newE2EReseeder(environment string, logger *slog.Logger, db *pgxpool.Pool, ch driver.Conn, blob assets.BlobStore, enc *encryption.Client, cache *redis.Client, issuerURL string, siteURL, serverURL *url.URL, userEmail string) e2econtrol.Reseeder {
	if environment != "local" {
		return nil
	}
	return &localE2EReseeder{logger: logger, db: db, ch: ch, blob: blob, enc: enc, cache: cache, issuerURL: issuerURL, siteURL: siteURL, serverURL: serverURL, userEmail: userEmail}
}

type localE2EReseeder struct {
	logger    *slog.Logger
	db        *pgxpool.Pool
	ch        driver.Conn
	blob      assets.BlobStore
	enc       *encryption.Client
	cache     *redis.Client
	issuerURL string
	siteURL   *url.URL
	serverURL *url.URL
	userEmail string
}

func (r *localE2EReseeder) Tenant() e2econtrol.Tenant {
	spec := demoseed.LocalSpec()
	return e2econtrol.Tenant{
		OrganizationID:   spec.OrgID,
		OrganizationSlug: spec.OrgSlug,
		ProjectSlug:      "default",
		SeededUserPrefix: spec.UserPrefix,
		APIKeyName:       demoseed.LocalAPIKeyName,
	}
}

func (r *localE2EReseeder) Reseed(ctx context.Context) error {
	if err := demoseed.Run(ctx, r.logger, r.db, r.ch, r.blob, r.enc, r.issuerURL, r.serverURL.String(), demoseed.LocalSpec()); err != nil {
		if errors.Is(err, demoseed.ErrSeedLocked) {
			return e2econtrol.ErrSeedBusy
		}
		return fmt.Errorf("apply seed: %w", err)
	}
	// The developer-machine upstreams (Enterprise Sign-In through the dev-idp,
	// the Linear and Notion clients) and the Slack test app are left as the
	// shared seed set them: `mise run seed` wires those from the developer's
	// environment, which a request handler does not read.
	if err := demoseed.RunLocalFixtures(ctx, r.logger, r.db, r.blob, r.cache, demoseed.LocalFixturesOptions{
		SiteURL:        r.siteURL,
		DeveloperEmail: r.userEmail,
		Environment:    "local",
		ObservedEnv:    nil,
		Slack:          demoseed.LocalSlackOptions{BotToken: "", SlackEmail: "", Encryption: nil, Slack: nil},
		Upstreams:      demoseed.LocalUpstreamOptions{ServerURL: nil, Encryption: nil, DevIDPURL: "", DevIDPClientID: "", HTTPClient: nil, Policy: nil, CacheDir: ""},
		ClickHouse:     nil,
		MockMCPURL:     "",
	}); err != nil {
		return fmt.Errorf("apply local fixtures: %w", err)
	}
	return nil
}
