package main

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/speakeasy-api/gram/dev-idp/internal/modes/oauth21"
	devidpworkos "github.com/speakeasy-api/gram/dev-idp/internal/modes/workos"
)

// retiredPrefix is a URL prefix dev-idp used to serve, paired with what
// replaced it and the environment variable that still points at the old one.
type retiredPrefix struct {
	old     string
	current string
	envVar  string
}

// retiredPrefixes exists because a prefix rename does not reach existing
// checkouts. `mise work:init` resolves dependent env vars and writes the
// literal URLs into mise.local.toml with the worktree's port baked in, so
// those pinned values keep pointing at the old prefix after a rename and
// mise.toml's new default never applies.
//
// Without this, the failure surfaces a long way from its cause: the server
// redirects the browser to a path that 404s, and the operator sees "authorize
// did not return a redirect" out of the seed script — which reads as a broken
// dev-idp rather than stale local configuration.
//
// Safe to delete once no local checkout predates the rename.
var retiredPrefixes = []retiredPrefix{
	{old: "/oauth2", current: oauth21.Prefix, envVar: "GRAM_IDP_BASE_URL"},
	{old: "/mock-workos", current: devidpworkos.Prefix, envVar: "WORKOS_API_URL"},
}

// mountRetiredPrefixes registers an explanatory handler on each prefix
// dev-idp no longer serves. Registering the subtree is safe alongside the
// live prefixes: ServeMux matches on whole path segments, so "/oauth2/" never
// captures "/oauth2-1/...".
func mountRetiredPrefixes(mux *http.ServeMux, logger *slog.Logger) {
	for _, p := range retiredPrefixes {
		mux.Handle(p.old+"/", retiredPrefixHandler(p, logger))
	}
}

func retiredPrefixHandler(p retiredPrefix, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Logged at error level on every hit: this is the one place the real
		// cause is visible, and whatever called it is about to report
		// something far less useful.
		logger.ErrorContext(r.Context(), "request to a retired dev-idp prefix — stale local configuration",
			slog.String("retired_prefix", p.old),
			slog.String("current_prefix", p.current),
			slog.String("env_var", p.envVar),
			slog.String("http.route", r.URL.Path),
			slog.String("fix", "mise set --file mise.local.toml "+p.envVar+"=\"$GRAM_DEVIDP_EXTERNAL_URL"+p.current+"\" && mise run start"),
		)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusGone)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "retired_prefix",
			"error_description": "dev-idp no longer serves " + p.old + "; it moved to " + p.current +
				". " + p.envVar + " is pinned to the old path, most likely in mise.local.toml, " +
				"where `mise work:init` writes resolved URLs with this worktree's port baked in.",
			"retired_prefix": p.old,
			"current_prefix": p.current,
			"env_var":        p.envVar,
			"fix":            `mise set --file mise.local.toml ` + p.envVar + `="$GRAM_DEVIDP_EXTERNAL_URL` + p.current + `"`,
		})
	})
}
