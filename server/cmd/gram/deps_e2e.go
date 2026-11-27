package gram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/demoseed"
	"github.com/speakeasy-api/gram/server/internal/e2econtrol"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// e2eControlTokenFlag belongs to the API server only (serverFlags): no other
// command mounts the surface, so no other command accepts its token.
const e2eControlTokenFlag = "e2e-control-token" //nolint:gosec // G101: a flag name, not a credential

// previewDatabase is the name of a pull request preview's own database, the
// throwaway clone its deployment creates for it. Nothing else has that name:
// an environment's own database does not carry a pull request number.
var previewDatabase = regexp.MustCompile(`^gram_preview_pr_([1-9][0-9]*)$`)

// e2eControlDeps is what the end-to-end test control surface is built from.
type e2eControlDeps struct {
	environment string
	token       string
	// databaseName asks the database the reseed will write to for its own
	// name (current_database()).
	databaseName func(ctx context.Context) (string, error)
	// developerEmail names the local developer, who is the test user on a
	// local stack.
	developerEmail func(ctx context.Context) string
	siteURL        *url.URL
	serverURL      *url.URL
	issuerURL      string
	db             *pgxpool.Pool
	clickhouse     driver.Conn
	blob           assets.BlobStore
	encryption     *encryption.Client
	redis          *redis.Client
	users          e2econtrol.Users
	sessions       e2eSessions
}

// e2eSessions is what the surface needs from sessions.Manager.
type e2eSessions interface {
	e2econtrol.SessionStore
	ConfineUser(userID, organizationID string)
	RaiseUserEpoch(ctx context.Context, userID string, at time.Time) error
}

// attachE2EControl mounts the end-to-end test control surface (/rpc/e2e.*)
// when a control token is configured, and mounts nothing otherwise. A token
// configured anywhere the surface may not exist is an error, which stops the
// server from starting. It may exist in two places:
//
//   - a local stack (environment "local"). The test organization is the local
//     development organization and the test user is the developer the local
//     seed adopts into it (`git config user.email`), so e2e.reseed is the Go
//     that `mise run seed` runs for that organization.
//   - a pull request preview: environment "dev", connected to that preview's
//     own database (gram_preview_pr_<N>) and served at pr-<N>. The test
//     organization and its one test user are synthetic, exist only in that
//     database, and are named after N (demoseed.PreviewSpec).
//
// The environment's own deployment is connected to the environment's database,
// so it is refused whatever else it is given, as is every other environment.
func attachE2EControl(ctx context.Context, logger *slog.Logger, mux goahttp.Muxer, d e2eControlDeps) error {
	if d.token == "" {
		return nil
	}
	cfg, err := e2eControlTarget(ctx, logger, d)
	if err != nil {
		return fmt.Errorf("end-to-end test control surface: %w", err)
	}
	cfg.Token = d.token
	cfg.SiteURL = d.siteURL
	cfg.Build = GitSHA
	service, err := e2econtrol.New(logger, cfg, d.users, d.sessions, cache.NewRedisCacheAdapter(d.redis))
	if err != nil {
		return fmt.Errorf("end-to-end test control surface: %w", err)
	}
	if cfg.Confined {
		d.sessions.ConfineUser(cfg.UserID, cfg.Tenant.OrganizationID)
	}
	e2econtrol.Attach(mux, service)
	logger.WarnContext(ctx, "end-to-end test control surface is enabled (/rpc/e2e.*)", attr.SlogOrganizationID(cfg.Tenant.OrganizationID))
	return nil
}

// e2eControlTarget decides where this server is and returns the organization,
// user and reseed the surface is fixed to there, or the refusal.
func e2eControlTarget(ctx context.Context, logger *slog.Logger, d e2eControlDeps) (e2econtrol.Config, error) {
	var none e2econtrol.Config
	switch d.environment {
	case "local":
		spec := demoseed.LocalSpec()
		email := d.developerEmail(ctx)
		return e2econtrol.Config{
			Token:     "",
			UserEmail: email,
			UserID:    "",
			Confined:  false,
			SiteURL:   nil,
			Build:     "",
			Tenant:    e2eTenant(spec),
			Reseed:    func(ctx context.Context) error { return reseedLocalOrganization(ctx, logger, d, spec, email) },
		}, nil
	case "dev":
		database, err := d.databaseName(ctx)
		if err != nil {
			return none, fmt.Errorf("read the database's name: %w", err)
		}
		pullRequest, err := previewPullRequest(database, d.siteURL, d.serverURL)
		if err != nil {
			return none, err
		}
		spec, err := demoseed.PreviewSpec(pullRequest)
		if err != nil {
			return none, fmt.Errorf("preview test organization: %w", err)
		}
		runner := spec.PreviewRunner()
		return e2econtrol.Config{
			Token:     "",
			UserEmail: runner.Email,
			UserID:    runner.ID,
			Confined:  true,
			SiteURL:   nil,
			Build:     "",
			Tenant:    e2eTenant(spec),
			Reseed:    func(ctx context.Context) error { return reseedPreviewOrganization(ctx, logger, d, spec) },
		}, nil
	default:
		return none, fmt.Errorf("it exists only on a local stack or a pull request preview, refusing to enable it in the %q environment", d.environment)
	}
}

// previewPullRequest is the pull request whose preview this server is: the
// number in its database's name, which the dashboard and server hosts must
// both carry too. Any disagreement is a refusal.
func previewPullRequest(database string, siteURL, serverURL *url.URL) (int, error) {
	match := previewDatabase.FindStringSubmatch(database)
	if match == nil {
		return 0, errors.New("in the \"dev\" environment it exists only on a pull request preview, and this server's database is not a preview's")
	}
	pullRequest, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, fmt.Errorf("preview database number: %w", err)
	}
	for _, u := range []*url.URL{siteURL, serverURL} {
		if u == nil || u.Hostname() != siteURL.Hostname() || !strings.HasPrefix(u.Hostname(), "pr-"+match[1]+".") {
			return 0, fmt.Errorf("this server's database is pull request %d's preview, but its dashboard and server are not both served at one pr-%d host", pullRequest, pullRequest)
		}
	}
	return pullRequest, nil
}

func e2eTenant(spec demoseed.Spec) e2econtrol.Tenant {
	return e2econtrol.Tenant{
		OrganizationID:   spec.OrgID,
		OrganizationSlug: spec.OrgSlug,
		ProjectSlug:      "default",
		SeededUserPrefix: spec.UserPrefix,
		CustomDomain:     spec.CustomDomain,
	}
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

// reseedPreviewOrganization replaces the preview's test organization with the
// shared seed in the preview's own Postgres database, then restores the test
// user's membership. It writes no telemetry: a preview shares its
// environment's ClickHouse, so the organization's telemetry pages stay empty.
// Every session the test user was issued before the reset ends with it.
func reseedPreviewOrganization(ctx context.Context, logger *slog.Logger, d e2eControlDeps, spec demoseed.Spec) error {
	if err := demoseed.Run(ctx, logger, d.db, nil, d.blob, d.encryption, d.issuerURL, d.serverURL.String(), spec); err != nil {
		return fmt.Errorf("apply seed: %w", err)
	}
	if err := demoseed.RunPreviewFixtures(ctx, logger, d.db, d.redis, spec); err != nil {
		return fmt.Errorf("apply preview fixtures: %w", err)
	}
	// Taken once the reset is committed, so a sign-in that read the
	// organization while it was being rebuilt ends too.
	if err := d.sessions.RaiseUserEpoch(ctx, spec.PreviewRunner().ID, time.Now()); err != nil {
		return fmt.Errorf("end the test user's earlier sessions: %w", err)
	}
	return nil
}

// currentDatabase asks db which database it is connected to.
func currentDatabase(ctx context.Context, db *pgxpool.Pool) (string, error) {
	name, err := demoseed.CurrentDatabase(ctx, db)
	if err != nil {
		return "", fmt.Errorf("current database: %w", err)
	}
	return name, nil
}

// e2eUsers reads the test user for the control surface, from the database.
type e2eUsers struct {
	db *pgxpool.Pool
}

func (u e2eUsers) UserByEmail(ctx context.Context, email string) (*sessions.CachedUserInfo, error) {
	user, err := userrepo.New(u.db).GetUserByEmail(ctx, email)
	switch {
	case errors.Is(err, pgx.ErrNoRows), err == nil && user.DeletedAt.Valid:
		return nil, nil //nolint:nilnil // a user that does not exist is not an error
	case err != nil:
		return nil, fmt.Errorf("get user by email: %w", err)
	}
	// Live memberships of enabled organizations, the list a session can reach.
	rows, err := orgrepo.New(u.db).ListOrganizationsForUser(ctx, conv.ToPGText(user.ID))
	if err != nil {
		return nil, fmt.Errorf("list organizations for user: %w", err)
	}
	organizations := make([]sessions.Organization, 0, len(rows))
	for _, row := range rows {
		//nolint:exhaustruct_v5 // the surface reads only which organizations these are
		organizations = append(organizations, sessions.Organization{ID: row.ID, Name: row.Name, Slug: row.Slug})
	}
	//nolint:exhaustruct_v5 // the surface reads only identity, staff bit and organizations
	return &sessions.CachedUserInfo{UserID: user.ID, Email: user.Email, Admin: user.Admin, Organizations: organizations}, nil
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
