package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// SessionActiveOrgResolver reports the active organization id for a session
// token. The second return is false when the session cannot be resolved.
type SessionActiveOrgResolver func(ctx context.Context, sessionToken string) (string, bool)

// demoMutatingVerbs are the RPC method-name verb prefixes treated as
// mutations by DemoOrgWriteGuard. POST is not enough of a signal on its own:
// many read endpoints (telemetry.query, risk searches, …) use POST bodies, so
// the guard denies by verb instead. Verbs missing from this set still fall
// through to scope enforcement — this is defense-in-depth for handlers
// without complete scope coverage, not the primary access control.
var demoMutatingVerbs = map[string]struct{}{
	"accept": {}, "add": {}, "apply": {}, "assign": {}, "attach": {},
	"bulk": {}, "cancel": {}, "clone": {}, "create": {}, "decline": {},
	"delete": {}, "detach": {}, "disable": {}, "enable": {}, "evolve": {},
	"execute": {}, "grant": {}, "install": {}, "invite": {}, "join": {},
	"leave": {}, "link": {}, "merge": {}, "mint": {}, "move": {},
	"publish": {}, "redeem": {}, "register": {}, "remove": {}, "rename": {},
	"reorder": {}, "revoke": {}, "rotate": {}, "run": {}, "schedule": {},
	"send": {}, "set": {}, "start": {}, "stop": {}, "submit": {},
	"sync": {}, "toggle": {}, "trigger": {}, "unassign": {}, "uninstall": {},
	"unlink": {}, "update": {}, "upload": {}, "upsert": {},
}

// DemoOrgWriteGuard rejects mutating /rpc calls for sessions whose active
// organization is the shared read-only demo org. Scope enforcement (the demo
// org's fixed read-only grant set) is the primary control; this middleware
// backstops handlers that lack scope checks. /rpc/auth.* stays reachable so
// users can exit the demo (switchScopes, logout).
func DemoOrgWriteGuard(demoOrgID string, resolve SessionActiveOrgResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isMutatingRPC(r) {
				next.ServeHTTP(w, r)
				return
			}

			token, ok := contextvalues.GetSessionTokenFromContext(r.Context())
			if !ok || token == "" {
				next.ServeHTTP(w, r)
				return
			}

			if org, ok := resolve(r.Context(), token); !ok || org != demoOrgID {
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"name":"forbidden","message":"the demo organization is read-only"}`))
		})
	}
}

func isMutatingRPC(r *http.Request) bool {
	path := r.URL.Path
	if !strings.HasPrefix(path, "/rpc/") || strings.HasPrefix(path, "/rpc/auth.") {
		return false
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	case http.MethodDelete, http.MethodPut, http.MethodPatch:
		return true
	}

	method := path[strings.LastIndex(path, ".")+1:]
	_, mutating := demoMutatingVerbs[leadingLowerWord(method)]
	return mutating
}

// leadingLowerWord returns the leading lowercase run of a camelCase RPC
// method name — the verb ("createServer" → "create").
func leadingLowerWord(s string) string {
	for i, c := range s {
		if c < 'a' || c > 'z' {
			return s[:i]
		}
	}
	return s
}
