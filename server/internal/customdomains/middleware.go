package customdomains

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	domainsRepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/wide"
)

// ParsePlatformHosts validates the first-party hosts (GRAM_PLATFORM_HOSTS) and
// maps each canonical host to the base URL rendered for requests on it. The
// server URL's own host is always first-party; if listed, it keeps the server
// URL as its base URL.
func ParsePlatformHosts(rawHosts []string) (map[string]string, error) {
	hosts := make(map[string]string, len(rawHosts))
	for _, raw := range rawHosts {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		host, err := requestorigin.CanonicalHost(raw)
		if err != nil {
			return nil, fmt.Errorf("platform host %q: %w", raw, err)
		}
		baseURL, err := requestorigin.HTTPSBaseURL(host)
		if err != nil {
			return nil, fmt.Errorf("platform host %q: %w", raw, err)
		}
		hosts[host] = baseURL
	}
	return hosts, nil
}

// Middleware classifies each request by host: the server URL's host and the
// extra platformHosts (from ParsePlatformHosts) are first-party, and every
// other host must be an active custom domain.
func Middleware(logger *slog.Logger, db *pgxpool.Pool, env string, serverURL *url.URL, platformHosts map[string]string) func(next http.Handler) http.Handler {
	domainsRepo := domainsRepo.New(db)
	logger = logger.With(attr.SlogComponent("custom_domains_middleware"))
	platformBaseURL := strings.TrimSuffix(serverURL.String(), "/")
	platformHost, _ := requestorigin.CanonicalHost(serverURL.Host)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			// Local development deliberately accepts arbitrary request Hosts, but
			// externally visible URLs still use the configured platform origin.
			if env == "local" {
				ctx = requestorigin.WithContext(ctx, requestorigin.Origin{
					Surface:          requestorigin.SurfacePlatform,
					BaseURL:          platformBaseURL,
					OrganizationID:   "",
					NetworkIngressID: uuid.Nil,
					NetworkIdentity:  nil,
				})
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			host, hostErr := requestorigin.CanonicalHost(r.Host)
			if hostErr != nil {
				serr := oops.E(oops.CodeBadRequest, hostErr, "request host is invalid").LogError(ctx, logger, attr.SlogHostName(r.Host))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				if err := json.NewEncoder(w).Encode(serr); err != nil {
					logger.ErrorContext(ctx, "failed to encode invalid host error response", attr.SlogHostName(r.Host), attr.SlogError(err))
				}

				return
			}

			hostBaseURL, isPlatform := platformHosts[host]
			if host == platformHost {
				hostBaseURL, isPlatform = platformBaseURL, true
			}
			if isPlatform {
				ctx = requestorigin.WithContext(ctx, requestorigin.Origin{
					Surface:          requestorigin.SurfacePlatform,
					BaseURL:          hostBaseURL,
					OrganizationID:   "",
					NetworkIngressID: uuid.Nil,
					NetworkIdentity:  nil,
				})
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			domain, err := domainsRepo.GetCustomDomainByDomain(ctx, host)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				http.Error(w, "invalid domain", http.StatusForbidden)
				return
			case err != nil:
				serr := oops.E(oops.CodeUnexpected, err, "domain check failed").LogError(ctx, logger, attr.SlogHostName(host), attr.SlogError(err))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				if err := json.NewEncoder(w).Encode(serr); err != nil {
					logger.ErrorContext(ctx, "failed to encode unexpected error response", attr.SlogHostName(host), attr.SlogError(err))
				}

				return
			}

			domainAttrs := make([]slog.Attr, 0, 2)
			if domain.ID != uuid.Nil {
				domainAttrs = append(domainAttrs, attr.SlogRequestCustomDomainID(domain.ID.String()))
			}
			if domain.Domain != "" {
				domainAttrs = append(domainAttrs, attr.SlogRequestCustomDomainName(domain.Domain))
			}
			if len(domainAttrs) > 0 {
				wide.Push(ctx, domainAttrs...)
			}

			if !domain.Activated || !domain.Verified {
				http.Error(w, "invalid domain", http.StatusForbidden)
				logger.ErrorContext(ctx, "domain not activated", attr.SlogHostName(host))
				return
			}

			ctx = WithContext(ctx, &Context{
				OrganizationID: domain.OrganizationID,
				Domain:         domain.Domain,
				DomainID:       domain.ID,
			})
			ctx = requestorigin.WithContext(ctx, requestorigin.Origin{
				Surface:          requestorigin.SurfaceCustomDomain,
				BaseURL:          "https://" + host,
				OrganizationID:   domain.OrganizationID,
				NetworkIngressID: uuid.Nil,
				NetworkIdentity:  nil,
			})

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
