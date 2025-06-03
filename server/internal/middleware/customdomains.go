package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	domainsRepo "github.com/speakeasy-api/gram/internal/customdomains/repo"
)

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
			logger.InfoContext(r.Context(), fmt.Sprintf("host: %s", host)) // TODO: Temporary sanity check
			if strings.HasPrefix(host, "localhost") && env != "local" {
				http.Error(w, "localhost not allowed in this environment", http.StatusForbidden)
				logger.ErrorContext(r.Context(), "localhost not allowed in this environment", slog.String("host", host))
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
