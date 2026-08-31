package customdomains

import (
	"encoding/json"
	"errors"
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
)

func Middleware(logger *slog.Logger, db *pgxpool.Pool, env string, serverURL *url.URL) func(next http.Handler) http.Handler {
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

			if host == platformHost {
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
