package middleware

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

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
	"accept": {}, "acknowledge": {}, "add": {}, "apply": {}, "archive": {},
	"assign": {}, "attach": {}, "authorize": {}, "block": {}, "bulk": {},
	"cancel": {}, "capture": {}, "clone": {}, "create": {}, "decline": {},
	"delete": {}, "deny": {}, "detach": {}, "disable": {}, "dismiss": {},
	"distribute": {}, "enable": {}, "ensure": {}, "evolve": {},
	"exchange": {}, "execute": {}, "generate": {}, "grant": {},
	"ingest": {}, "install": {}, "invite": {}, "join": {}, "leave": {},
	"link": {}, "mark": {}, "mask": {}, "merge": {}, "migrate": {},
	"mint": {}, "move": {}, "pause": {}, "publish": {}, "redeem": {},
	"redeploy": {}, "refresh": {}, "register": {}, "remove": {},
	"rename": {}, "reorder": {}, "resolve": {}, "restore": {},
	"resume": {}, "retry": {}, "revoke": {}, "rotate": {}, "run": {},
	"save": {}, "schedule": {}, "send": {}, "set": {}, "share": {},
	"start": {}, "stop": {}, "submit": {}, "suspend": {}, "sync": {},
	"toggle": {}, "trigger": {}, "unarchive": {}, "unassign": {},
	"unblock": {}, "undistribute": {}, "uninstall": {}, "unlink": {},
	"unmark": {}, "unmask": {}, "unshare": {}, "update": {}, "upload": {},
	"upsert": {},
}

// DemoOrgWriteGuard rejects mutating /rpc calls for sessions whose active
// organization is the shared read-only demo org. Scope enforcement (the demo
// org's fixed read-only grant set) is the primary control; this middleware
// backstops handlers that lack scope checks. /rpc/auth.* stays reachable so
// users can exit the demo (switchScopes, logout).
func DemoOrgWriteGuard(demoOrgID string, resolve SessionActiveOrgResolver) func(http.Handler) http.Handler {
	// Sessions recently seen outside the demo org skip the lookup for a
	// short window, so customer write traffic doesn't pay a session fetch
	// per request. Only the "not demo" verdict is cached: a stale positive
	// would 403 writes in the user's real org right after exiting the demo,
	// while a stale negative merely delays this backstop — which scope
	// enforcement already covers.
	const notDemoTTL = 10 * time.Second
	const notDemoMaxEntries = 10000
	var mu sync.Mutex
	notDemo := make(map[string]time.Time)

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

			mu.Lock()
			exp, cached := notDemo[token]
			mu.Unlock()
			if cached && time.Now().Before(exp) {
				next.ServeHTTP(w, r)
				return
			}

			if org, ok := resolve(r.Context(), token); !ok || org != demoOrgID {
				mu.Lock()
				if len(notDemo) >= notDemoMaxEntries {
					notDemo = make(map[string]time.Time)
				}
				notDemo[token] = time.Now().Add(notDemoTTL)
				mu.Unlock()
				next.ServeHTTP(w, r)
				return
			}

			// Full Goa error envelope so generated SDK clients parse this as a
			// regular service error instead of failing response validation.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"name":"forbidden","id":"demo_org_read_only","message":"the demo organization is read-only","temporary":false,"timeout":false,"fault":false}`))
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
