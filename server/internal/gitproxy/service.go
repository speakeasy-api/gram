// Package gitproxy implements a git smart-HTTP reverse proxy in front of
// github.com. Inbound requests are authenticated with a Gram API key supplied
// as the password component of HTTP Basic auth; outbound requests carry a
// short-lived GitHub App installation token, also as Basic auth, which is the
// credential format git over HTTPS expects.
//
// The result is that any consumer who can present a valid Gram API key can
// clone (and optionally push to) the repositories that the configured GitHub
// App installation has been granted access to, without ever holding GitHub
// credentials themselves. This is the building block for using Gram API keys
// as the unit of access provisioning for source code.
//
// Scope of this spike
//
//   - Read and write supported (info/refs, git-upload-pack, git-receive-pack).
//     Set GRAM_GIT_PROXY_READ_ONLY=true to disable receive-pack at the routing
//     layer.
//   - All API keys with the consumer scope are granted access to every
//     repository the installation can see. Per-key allowlisting is a TODO and
//     is the actual product surface — the proxy here is just plumbing.
//   - Git LFS is not handled. LFS does its own auth handshake and returns
//     S3-style redirect URLs that bypass this proxy.
//   - SSH is not handled — HTTPS only.
package gitproxy

import (
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/github"
)

// upstream is the only host this proxy will forward to.
var upstream = &url.URL{Scheme: "https", Host: "github.com"}

// Config is the deployment-time configuration. All four fields must be set
// for the feature to be enabled; the constructor returns (nil, nil) if none
// are set, matching the all-or-nothing pattern used by plugins.GitHubConfig.
type Config struct {
	AppID          int64
	PrivateKey     string
	InstallationID int64
	ReadOnly       bool
	HTTPClient     *guardian.HTTPClient
}

// Built is the resolved configuration after validation: a GitHub App client
// already constructed and the installation ID it should mint tokens for.
type Built struct {
	GitHub         *github.Client
	InstallationID int64
	ReadOnly       bool
}

// NewConfig validates the input holistically. Returns (nil, nil) when the
// feature is disabled (no fields set), (config, nil) when fully configured,
// and (nil, error) on a partial configuration so misconfigurations surface
// at startup rather than as silent 500s at request time.
func NewConfig(in Config) (*Built, error) {
	set := 0
	missing := []string{}
	if in.AppID != 0 {
		set++
	} else {
		missing = append(missing, "git-proxy-github-app-id")
	}
	if in.PrivateKey != "" {
		set++
	} else {
		missing = append(missing, "git-proxy-github-private-key")
	}
	if in.InstallationID != 0 {
		set++
	} else {
		missing = append(missing, "git-proxy-github-installation-id")
	}

	switch set {
	case 0:
		return nil, nil
	case 3:
		client, err := github.NewClient(in.AppID, []byte(in.PrivateKey), in.HTTPClient)
		if err != nil {
			return nil, fmt.Errorf("create github client: %w", err)
		}
		return &Built{
			GitHub:         client,
			InstallationID: in.InstallationID,
			ReadOnly:       in.ReadOnly,
		}, nil
	default:
		return nil, fmt.Errorf("git proxy requires all of git-proxy-github-app-id, git-proxy-github-private-key, git-proxy-github-installation-id; missing: %s", strings.Join(missing, ", "))
	}
}

// Service owns the dependencies used by the git smart-HTTP handler.
type Service struct {
	logger         *slog.Logger
	keyAuth        *auth.ByKey
	github         *github.Client
	installationID int64
	readOnly       bool
	upstreamRT     http.RoundTripper
	upstreamErrLog *log.Logger
}

// NewService constructs the Service. The proxy reverse-proxies to github.com
// using a streaming-friendly HTTP client built from the supplied policy.
// Retries are deliberately *not* enabled: git smart-HTTP responses are
// streamed in a stateful pack format, so replaying a partial transfer would
// corrupt the wire protocol.
func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	billingRepo billing.Repository,
	guardianPolicy *guardian.Policy,
	built *Built,
) *Service {
	logger = logger.With(attr.SlogComponent("gitproxy"))

	upstreamClient := guardianPolicy.PooledClient()

	_ = tracerProvider // wired through for future per-request spans.

	return &Service{
		logger:         logger,
		keyAuth:        auth.NewKeyAuth(db, logger, billingRepo),
		github:         built.GitHub,
		installationID: built.InstallationID,
		readOnly:       built.ReadOnly,
		upstreamRT:     upstreamClient.Transport,
		upstreamErrLog: slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}
}

// Attach registers three handlers, one per smart-HTTP endpoint. The catch-all
// pattern is deliberately avoided so that misrouted traffic 404s cleanly
// instead of being silently forwarded to GitHub.
func Attach(mux goahttp.Muxer, svc *Service) {
	if svc == nil {
		return
	}

	infoRefs := oops.ErrHandle(svc.logger, svc.handleInfoRefs).ServeHTTP
	uploadPack := oops.ErrHandle(svc.logger, svc.handleUploadPack).ServeHTTP
	receivePack := oops.ErrHandle(svc.logger, svc.handleReceivePack).ServeHTTP

	// Match both ".git" and bare paths so e.g. `git clone .../owner/repo`
	// works the same as `git clone .../owner/repo.git`.
	for _, base := range []string{"/git/{owner}/{repo}", "/git/{owner}/{repo}.git"} {
		o11y.AttachHandler(mux, http.MethodGet, base+"/info/refs", infoRefs)
		o11y.AttachHandler(mux, http.MethodPost, base+"/git-upload-pack", uploadPack)
		o11y.AttachHandler(mux, http.MethodPost, base+"/git-receive-pack", receivePack)
	}
}

func (s *Service) handleInfoRefs(w http.ResponseWriter, r *http.Request) error {
	service := r.URL.Query().Get("service")
	if service != "git-upload-pack" && service != "git-receive-pack" {
		return oops.E(oops.CodeBadRequest, nil, "unsupported service: %q", service)
	}
	if service == "git-receive-pack" && s.readOnly {
		return oops.E(oops.CodeForbidden, nil, "this git proxy is read-only")
	}
	return s.serve(w, r, "/info/refs")
}

func (s *Service) handleUploadPack(w http.ResponseWriter, r *http.Request) error {
	return s.serve(w, r, "/git-upload-pack")
}

func (s *Service) handleReceivePack(w http.ResponseWriter, r *http.Request) error {
	if s.readOnly {
		return oops.E(oops.CodeForbidden, nil, "this git proxy is read-only")
	}
	return s.serve(w, r, "/git-receive-pack")
}

func (s *Service) serve(w http.ResponseWriter, r *http.Request, suffix string) error {
	ctx := r.Context()

	owner := chi.URLParam(r, "owner")
	repo := strings.TrimSuffix(chi.URLParam(r, "repo"), ".git")
	if owner == "" || repo == "" {
		return oops.E(oops.CodeBadRequest, nil, "missing owner or repo")
	}

	// Phase 1: extract the Gram API key. Git's HTTP transport probes
	// without credentials first; a 401 + WWW-Authenticate triggers the
	// retry that includes Basic auth.
	_, key, ok := r.BasicAuth()
	if !ok || key == "" {
		w.Header().Set("WWW-Authenticate", `Basic realm="gram-git"`)
		return oops.E(oops.CodeUnauthorized, nil, "missing credentials")
	}

	// Phase 2: validate the key. The auth helper writes the principal
	// (org, project, key id, scopes) into the context for downstream use.
	authCtx, err := s.keyAuth.KeyBasedAuth(ctx, key, []string{auth.APIKeyScopeConsumer.String()})
	if err != nil {
		var se *oops.ShareableError
		if errors.As(err, &se) && se.HTTPStatus() == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", `Basic realm="gram-git"`)
		}
		return fmt.Errorf("validate gram api key: %w", err)
	}
	ctx = authCtx

	// TODO: enforce a per-API-key allowlist of {owner}/{repo} pairs here.
	// That mapping is the actual product feature; without it any valid
	// consumer key can reach every repo the installation has access to.
	logAttrs := []any{
		attr.SlogComponent("gitproxy"),
		attr.SlogGitProxyOwner(owner),
		attr.SlogGitProxyRepo(repo),
		attr.SlogGitProxyOp(strings.TrimPrefix(suffix, "/")),
	}
	if principal, ok := contextvalues.GetAuthContext(ctx); ok && principal != nil {
		logAttrs = append(logAttrs,
			attr.SlogOrganizationID(principal.ActiveOrganizationID),
			attr.SlogAPIKeyID(principal.APIKeyID),
		)
	}
	s.logger.InfoContext(ctx, "git proxy request", logAttrs...)

	// Phase 3: mint an installation token. Cached and singleflighted by
	// the github client, so a busy clone fleet won't hammer the API.
	token, err := s.github.InstallationToken(ctx, s.installationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "mint github installation token").Log(ctx, s.logger)
	}

	// Phase 4: forward. Rewrite the request in place — SetURL fills in
	// scheme/host/upstream path; the trailing /{owner}/{repo}.git/{suffix}
	// is appended so GitHub sees a canonical URL regardless of whether
	// the inbound path used .git or not.
	outPath := fmt.Sprintf("/%s/%s.git%s", owner, repo, suffix)
	rp := &httputil.ReverseProxy{
		// FlushInterval -1 disables response buffering. Required: git
		// smart-HTTP relies on incremental flushes to make progress and
		// will hang under default buffering.
		FlushInterval: -1,
		Transport:     s.upstreamRT,
		ErrorLog:      s.upstreamErrLog,
		// Director is the legacy hook; Rewrite supersedes it on Go 1.20+
		// and ReverseProxy ignores Director when Rewrite is non-nil.
		Director:   nil,
		BufferPool: nil,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(upstream)
			pr.Out.Host = upstream.Host
			pr.Out.URL.Path = outPath
			pr.Out.URL.RawPath = ""
			// Preserve the smart-HTTP service query on info/refs requests.
			pr.Out.URL.RawQuery = r.URL.RawQuery
			// "x-access-token" is the documented username for GitHub App
			// installation tokens used as Basic-auth credentials.
			pr.Out.SetBasicAuth("x-access-token", token)
			// Drop hop-by-hop headers that ProxyRequest.SetURL doesn't
			// strip, plus any inbound cookie remnants.
			pr.Out.Header.Del("Cookie")
			pr.Out.Header.Set("User-Agent", forwardedUserAgent(r))
		},
		ModifyResponse: func(resp *http.Response) error {
			// GitHub may emit a WWW-Authenticate challenge if the
			// installation token has expired or doesn't grant access
			// to this repo. Rewrite the realm so the client retries
			// against us, not GitHub.
			if resp.Header.Get("WWW-Authenticate") != "" {
				resp.Header.Set("WWW-Authenticate", `Basic realm="gram-git"`)
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			s.logger.ErrorContext(r.Context(), "upstream proxy error",
				attr.SlogError(err),
				attr.SlogGitProxyOwner(owner),
				attr.SlogGitProxyRepo(repo),
			)
			http.Error(w, "upstream error", http.StatusBadGateway)
		},
	}
	rp.ServeHTTP(w, r.WithContext(ctx))
	return nil
}

// forwardedUserAgent passes through the inbound git client UA but tags it
// so upstream logs can distinguish proxied traffic. GitHub uses UA strings
// in some abuse heuristics; surfacing both helps debugging.
func forwardedUserAgent(r *http.Request) string {
	in := r.Header.Get("User-Agent")
	if in == "" {
		return "gram-gitproxy"
	}
	return in + " (via gram-gitproxy)"
}
