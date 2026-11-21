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
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/demoseed"
	"github.com/speakeasy-api/gram/server/internal/e2econtrol"
	"github.com/speakeasy-api/gram/server/internal/encryption"
)

// e2eControlTokenFlag belongs to the API server only (serverFlags): no other
// command mounts the surface, so no other command accepts its token.
const e2eControlTokenFlag = "e2e-control-token" //nolint:gosec // G101: a flag name, not a credential

// e2eControlDeps is what the end-to-end test control surface is built from.
type e2eControlDeps struct {
	environment string
	// identityBackend is GRAM_DEVIDP_BACKEND: "local" is the dev-idp's own
	// emulator, anything else forwards sign-ins to a real identity provider.
	identityBackend string
	// identityEndpoint is where Magic Auth is sent (WORKOS_API_URL; empty is
	// the real provider) and devIDPURL is this stack's dev-idp origin.
	identityEndpoint string
	devIDPURL        string
	token            string
	// developerEmail names the local developer, who is the test user.
	developerEmail func(ctx context.Context) string
	siteURL        *url.URL
	serverURL      *url.URL
	issuerURL      string
	db             *pgxpool.Pool
	clickhouse     driver.Conn
	blob           assets.BlobStore
	encryption     *encryption.Client
	redis          *redis.Client
	identity       e2econtrol.Identity
	sessions       e2econtrol.SessionStore
}

// attachE2EControl mounts the end-to-end test control surface (/rpc/e2e.*)
// when a control token is configured, and mounts nothing otherwise. A token
// configured anywhere but a local stack (e2econtrol.New), or on a local stack
// that signs in through a real identity provider, is an error, which stops
// the server from starting.
//
// The test organization is the local development organization and the test
// user is the developer the local seed adopts into it (`git config
// user.email`), so e2e.reseed is the Go that `mise run seed` runs for that
// organization.
func attachE2EControl(ctx context.Context, logger *slog.Logger, mux goahttp.Muxer, d e2eControlDeps) error {
	if d.token == "" {
		return nil
	}
	spec := demoseed.LocalSpec()
	email := d.developerEmail(ctx)
	service, err := e2econtrol.New(logger, e2econtrol.Config{
		Environment: d.environment,
		Token:       d.token,
		UserEmail:   email,
		SiteURL:     d.siteURL,
		Build:       GitSHA,
		Tenant: e2econtrol.Tenant{
			OrganizationID:   spec.OrgID,
			OrganizationSlug: spec.OrgSlug,
			ProjectSlug:      "default",
			SeededUserPrefix: spec.UserPrefix,
		},
		Reseed: func(ctx context.Context) error { return reseedLocalOrganization(ctx, logger, d, spec, email) },
	}, d.identity, d.sessions, cache.NewRedisCacheAdapter(d.redis))
	if err != nil {
		return fmt.Errorf("end-to-end test control surface: %w", err)
	}
	if d.identityBackend != "local" || d.devIDPURL == "" || d.identityEndpoint != d.devIDPURL+"/workos" {
		return errors.New("end-to-end test control surface: it signs in through this stack's dev-idp emulator only (GRAM_DEVIDP_BACKEND=local, WORKOS_API_URL at the dev-idp)")
	}
	e2econtrol.Attach(mux, service)
	logger.WarnContext(ctx, "end-to-end test control surface is enabled (/rpc/e2e.*)")
	return nil
}

// reseedLocalOrganization replaces the local organization's data with the
// shared seed, then applies the local fixtures that restore the developer's
// membership and the seeded API key.
//
// The developer-machine upstreams (Enterprise Sign-In through the dev-idp, the
// Linear and Notion clients) and the Slack test app are left as the seed set
// them: `mise run seed` wires those from the developer's environment, which a
// request handler does not read.
func reseedLocalOrganization(ctx context.Context, logger *slog.Logger, d e2eControlDeps, spec demoseed.Spec, email string) error {
	if err := demoseed.Run(ctx, logger, d.db, d.clickhouse, d.blob, d.encryption, d.issuerURL, d.serverURL.String(), spec); err != nil {
		return fmt.Errorf("apply seed: %w", err)
	}
	if err := demoseed.RunLocalFixtures(ctx, logger, d.db, d.blob, d.redis, demoseed.LocalFixturesOptions{
		SiteURL:        d.siteURL,
		DeveloperEmail: email,
		Environment:    d.environment,
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

// gitUserEmail is the address the dev-idp and the local seed both identify the
// developer by. Empty when git has none, which e2econtrol.New refuses.
func gitUserEmail(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "git", "config", "--get", "user.email").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
