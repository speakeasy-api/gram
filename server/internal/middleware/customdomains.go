package middleware

import (
	"log/slog"
	"net/http"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	domainsRepo "github.com/speakeasy-api/gram/internal/customdomains/repo"
)

// TODO: Running with custom domains in general should be a config
var GramDomains = []string{
	"app.getgram.ai",
	"prod.getgram.ai",
	"api.getgram.ai",
	"getgram.ai",
	"dev.getgram.ai",
}

func CustomDomainsMiddleware(logger *slog.Logger, db *pgxpool.Pool, env string) func(next http.Handler) http.Handler {
	domainsRepo := domainsRepo.New(db)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := r.Host
			// custom domains are not relevant in the local environment
			if env == "local" {
				next.ServeHTTP(w, r)
				return
			}

			if slices.Contains(GramDomains, host) {
				next.ServeHTTP(w, r)
				return
			}

			domain, err := domainsRepo.GetActiveCustomDomainByDomain(r.Context(), host)
			if err != nil {
				logger.ErrorContext(r.Context(), "failed to get custom domain", slog.String("host", host), slog.String("error", err.Error()))
				http.Error(w, "invalid domain", http.StatusForbidden)
				return
			}

			if !domain.Activated && !domain.Verified {
				http.Error(w, "invalid domain", http.StatusForbidden)
				logger.ErrorContext(r.Context(), "domain not activated", slog.String("host", host))
				return
			}

			ctx := contextvalues.SetCustomDomainContext(r.Context(), &contextvalues.CustomDomainContext{
				ProjectID: domain.ProjectID,
				Domain:    domain.Domain,
				DomainID:  domain.ID,
			})

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
