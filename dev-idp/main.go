// Package main is the dev-idp binary entrypoint. Boots an HTTP server on
// GRAM_DEVIDP_ADDRESS that mounts:
//
//   - the Goa management API (under /rpc/...) for /users, /organizations,
//     /memberships, /organization_roles, /invitations, /devIdp;
//   - the mock-workos mode at /mock-workos/ (mock WorkOS REST surface);
//   - the oauth2 mode at /oauth2/;
//   - the oauth2-1 mode at /oauth2-1/;
//   - the workos mode at /workos/ (only when GRAM_IDP_MODE=workos and GRAM_IDP_CLIENT_SECRET is a real key).
//
// A second tiny health server is mounted on GRAM_DEVIDP_CONTROL_ADDRESS.
//
// dev-idp is dev-only -- no auth, no OTel SDK, no production safety
// guardrails. Intended to back local end-to-end tests of Gram's auth
// flows.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/dev-idp/internal/bootstrap"
	"github.com/speakeasy-api/gram/dev-idp/internal/config"
	"github.com/speakeasy-api/gram/dev-idp/internal/keystore"
	"github.com/speakeasy-api/gram/dev-idp/internal/middleware"
	"github.com/speakeasy-api/gram/dev-idp/internal/modes/mockworkos"
	"github.com/speakeasy-api/gram/dev-idp/internal/modes/oauth2"
	"github.com/speakeasy-api/gram/dev-idp/internal/modes/oauth21"
	devidpworkos "github.com/speakeasy-api/gram/dev-idp/internal/modes/workos"
	"github.com/speakeasy-api/gram/dev-idp/internal/service"
	"github.com/speakeasy-api/gram/dev-idp/internal/workos"
	"github.com/speakeasy-api/gram/plog"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "dev-idp:", err)
		os.Exit(1)
	}
}

func run() error {
	address := flag.String("address", envOr("GRAM_DEVIDP_ADDRESS", ":35291"), "HTTP listener address")
	controlAddress := flag.String("control-address", envOr("GRAM_DEVIDP_CONTROL_ADDRESS", ":35292"), "HTTP listener address for the health/control server")
	externalURL := flag.String("external-url", os.Getenv("GRAM_DEVIDP_EXTERNAL_URL"), "Public base URL for discovery docs / redirect URIs (defaults from --address)")
	dbSpec := flag.String("db", os.Getenv("GRAM_DEVIDP_DB"), "SQLite location: 'memory' or 'file:<path>' (default file:local/devidp/devidp.db)")
	rsaKey := flag.String("rsa-private-key", os.Getenv("GRAM_DEVIDP_RSA_PRIVATE_KEY"), "PEM-encoded RSA private key (omit to generate a fresh ephemeral key)")
	idpMode := flag.String("idp-mode", envOr("GRAM_IDP_MODE", "mock-workos"), "IDP mode: mock-workos (default) or workos")
	workosKey := flag.String("workos-api-key", os.Getenv("GRAM_IDP_CLIENT_SECRET"), "WorkOS API key (required when --idp-mode=workos)")
	workosHost := flag.String("workos-host", envOr("WORKOS_API_URL", "https://api.workos.com"), "Base URL of the WorkOS API")
	flag.Parse()

	logger := plog.NewLogger(os.Stderr).With(slog.String("component", "dev-idp"))
	slog.SetDefault(logger)

	dbCfg, err := config.ParseDB(*dbSpec)
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	db, err := bootstrap.Open(ctx, dbCfg)
	if err != nil {
		return fmt.Errorf("open dev-idp database: %w", err)
	}
	defer func() { _ = db.Close() }()

	ks, err := keystore.New([]byte(*rsaKey), logger)
	if err != nil {
		return fmt.Errorf("init dev-idp keystore: %w", err)
	}

	pubURL := *externalURL
	if pubURL == "" {
		pubURL = deriveExternalURL(*address)
	}

	var tp trace.TracerProvider = tracenoop.NewTracerProvider()

	// Goa management API and the per-mode protocol handlers live on
	// different mux types. Compose them: register Goa endpoints onto an
	// inner goahttp.Muxer, then delegate to it from the outer http.ServeMux
	// as the catch-all. ServeMux specificity sends /<mode>/* to mode
	// handlers and everything else (including /rpc/* and /healthz) to Goa.
	goaMux := goahttp.NewMuxer()
	goaMux.Use(func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
				w.WriteHeader(http.StatusOK)
				return
			}
			h.ServeHTTP(w, r)
		})
	})
	goaMux.Use(middleware.NewHTTPLogging(logger))
	goaMux.Use(middleware.NewRecovery(logger))

	service.AttachOrganizations(goaMux, service.NewOrganizationsService(logger, tp, db))
	service.AttachUsers(goaMux, service.NewUsersService(logger, tp, db))
	service.AttachMemberships(goaMux, service.NewMembershipsService(logger, tp, db))
	service.AttachOrganizationRoles(goaMux, service.NewOrganizationRolesService(logger, tp, db))
	service.AttachInvitations(goaMux, service.NewInvitationsService(logger, tp, db))
	service.AttachDevIdp(goaMux, service.NewDevIdpService(logger, tp, db))

	outer := http.NewServeMux()

	mockHandler := mockworkos.NewHandler(logger, tp, db)
	outer.Handle(mockworkos.Prefix+"/", http.StripPrefix(mockworkos.Prefix, mockHandler.Handler()))

	oauth21Handler := oauth21.NewHandler(
		oauth21.Config{ExternalURL: pubURL},
		ks, logger, tp, db,
	)
	outer.Handle(oauth21.Prefix+"/", http.StripPrefix(oauth21.Prefix, oauth21Handler.Handler()))
	oauth21Handler.RegisterRootRoutes(outer)

	oauth2Handler := oauth2.NewHandler(
		oauth2.Config{ExternalURL: pubURL},
		ks, logger, tp, db,
	)
	outer.Handle(oauth2.Prefix+"/", http.StripPrefix(oauth2.Prefix, oauth2Handler.Handler()))
	oauth2Handler.RegisterRootRoutes(outer)

	if *idpMode == "workos" {
		if *workosKey == "" {
			return fmt.Errorf("GRAM_IDP_MODE=workos requires GRAM_IDP_CLIENT_SECRET to be a real WorkOS API key")
		}
		wsClient := workos.NewClient(*workosKey, workos.Opts{
			Endpoint: *workosHost,
		})
		wsHandler := devidpworkos.NewHandler(
			wsClient, logger, tp, db,
		)
		outer.Handle(devidpworkos.Prefix+"/", http.StripPrefix(devidpworkos.Prefix, wsHandler.Handler()))
		logger.InfoContext(ctx, "/workos/ proxy mounted (GRAM_IDP_MODE=workos)")
	}

	logger.InfoContext(ctx, "idp mode", slog.String("mode", *idpMode))

	outer.Handle("/", goaMux)

	srv := &http.Server{
		Addr:              *address,
		Handler:           outer,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	control := &http.Server{
		Addr:              *controlAddress,
		Handler:           controlMux(),
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	go func() {
		<-ctx.Done()
		logger.InfoContext(context.Background(), "shutting down dev-idp")
		graceCtx, graceCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer graceCancel()
		_ = srv.Shutdown(graceCtx)
		_ = control.Shutdown(graceCtx)
	}()

	go func() {
		if err := control.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.ErrorContext(ctx, "control server error", slog.Any("error", err))
		}
	}()

	logger.InfoContext(ctx, "dev-idp listening",
		slog.String("address", *address),
		slog.String("external_url", pubURL),
	)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("dev-idp server: %w", err)
	}
	return nil
}

func controlMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	})
	return mux
}

// deriveExternalURL turns a listener address ("host:port" or ":port") into
// an externally usable base URL.
func deriveExternalURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + strings.TrimPrefix(addr, "http://")
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
