package middleware

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	domainsRepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func CustomDomainsMiddleware(logger *slog.Logger, db *pgxpool.Pool, env string, serverURL *url.URL) func(next http.Handler) http.Handler {
	domainsRepo := domainsRepo.New(db)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			host := r.Host
			// custom domains are not relevant in the local environment
			if env == "local" {
				next.ServeHTTP(w, r)
				return
			}
			if env == "dev" && strings.Contains(host, "speakeasyapi.vercel.app") {
				// preview builds are good
				next.ServeHTTP(w, r)
				return
			}

			if host == "" {
				serr := oops.E(oops.CodeBadRequest, nil, "request host is not set").Log(ctx, logger, attr.SlogHostName(host))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				if err := json.NewEncoder(w).Encode(serr); err != nil {
					logger.ErrorContext(ctx, "failed to encode missing host error response", attr.SlogHostName(host), attr.SlogError(err))
				}

				return
			}

			if host == serverURL.Host {
				next.ServeHTTP(w, r)
				return
			}

			domain, err := domainsRepo.GetCustomDomainByDomain(ctx, host)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				http.Error(w, "invalid domain", http.StatusForbidden)
				return
			case err != nil:
				serr := oops.E(oops.CodeUnexpected, err, "domain check failed").Log(ctx, logger, attr.SlogHostName(host), attr.SlogError(err))
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

			ctx = gateway.DomainWithContext(ctx, &gateway.DomainContext{
				OrganizationID: domain.OrganizationID,
				Domain:         domain.Domain,
				DomainID:       domain.ID,
			})

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
