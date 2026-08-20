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

// authRPCPrefix is the one RPC namespace exempt from the demo write guard: it
// is how a session enters and leaves the demo org.
const authRPCPrefix = "/rpc/auth."

// demoReadOnlyVerbs are the RPC method-name verb prefixes DemoOrgWriteGuard
// treats as reads. POST is not enough of a signal on its own: many read
// endpoints (telemetry.query, risk searches, …) use POST bodies, so the guard
// classifies by verb instead. Verbs that reach an external URL the caller
// supplies (check, discover, fetch, test, verify) are mutating for this
// purpose even when they persist nothing — the demo never needs them.
var demoReadOnlyVerbs = map[string]struct{}{
	"active": {}, "compile": {}, "content": {}, "download": {},
	"export": {}, "get": {}, "latest": {}, "list": {}, "load": {},
	"logs": {}, "preview": {}, "query": {}, "render": {}, "search": {},
	"serve": {},
}

// demoMutatingVerbs are the verb prefixes known to mutate. Unrecognized verbs
// are treated as mutations too, so this set does not decide anything the
// default would not — it exists to keep the classification of every shipped
// RPC explicit, which is what TestDemoGuardClassifiesEveryRPCVerb asserts. A
// new endpoint whose verb appears in neither map is blocked in the demo org
// until someone classifies it.
var demoMutatingVerbs = map[string]struct{}{
	"accept": {}, "acknowledge": {}, "add": {}, "apply": {}, "approve": {},
	"archive": {}, "assign": {}, "attach": {}, "authorize": {}, "block": {},
	"bulk": {}, "cancel": {}, "capture": {}, "check": {}, "claude": {},
	"clear": {}, "clone": {}, "codex": {}, "create": {}, "credit": {},
	"cursor": {}, "decline": {}, "delete": {}, "deny": {}, "detach": {},
	"disable": {}, "discover": {}, "dismiss": {}, "distribute": {},
	"enable": {}, "ensure": {}, "evaluate": {}, "evolve": {},
	"exchange": {}, "execute": {}, "fetch": {}, "generate": {}, "grant": {},
	"ingest": {}, "install": {}, "invite": {}, "join": {}, "leave": {},
	"link": {}, "mark": {}, "mask": {}, "merge": {}, "migrate": {},
	"mint": {}, "move": {}, "otel": {}, "pause": {}, "promote": {},
	"publish": {}, "receive": {}, "recheck": {}, "record": {},
	"redeem": {}, "redeploy": {}, "refresh": {}, "register": {},
	"remove": {}, "rename": {}, "reorder": {}, "repair": {}, "report": {},
	"request": {}, "resolve": {}, "restore": {}, "resume": {}, "retry": {},
	"revoke": {}, "rotate": {}, "run": {}, "save": {}, "schedule": {},
	"send": {}, "set": {}, "share": {}, "skill": {}, "start": {},
	"stop": {}, "submit": {}, "suggest": {}, "summarize": {},
	"suspend": {}, "sync": {}, "test": {}, "toggle": {}, "trigger": {},
	"unarchive": {}, "unassign": {}, "unblock": {}, "undistribute": {},
	"uninstall": {}, "unlink": {}, "unmark": {}, "unmask": {},
	"unshare": {}, "update": {}, "upload": {}, "upsert": {}, "verify": {},
}

// DemoOrgWriteGuard rejects mutating /rpc calls for sessions whose active
// organization is the shared read-only demo org. Demo sessions hold every
// user-visible scope (authz.DemoScopeGrants) so the dashboard they are shown
// is the dashboard the server will serve, which makes this guard — not scope
// enforcement — the control that keeps the demo read-only. It therefore fails
// closed: anything the verb classification does not recognise as a read is
// rejected. /rpc/auth.* stays reachable so users can exit the demo
// (switchScopes, logout).
func DemoOrgWriteGuard(demoOrgID string, resolve SessionActiveOrgResolver) func(http.Handler) http.Handler {
	// Sessions recently seen outside the demo org skip the lookup for a
	// short window, so customer write traffic doesn't pay a session fetch
	// per request. Only the "not demo" verdict is cached: a stale positive
	// would 403 writes in the user's real org right after exiting the demo.
	// A stale negative would be worse — it would let writes into the demo org
	// for the length of the window, and scopes no longer stop them — so every
	// /rpc/auth.* call evicts its token below: switching a session's active
	// organization is something only those endpoints can do.
	const notDemoTTL = 10 * time.Second
	const notDemoMaxEntries = 10000
	var mu sync.Mutex
	notDemo := make(map[string]time.Time)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, authRPCPrefix) {
				if token, ok := contextvalues.GetSessionTokenFromContext(r.Context()); ok && token != "" {
					mu.Lock()
					delete(notDemo, token)
					mu.Unlock()
				}
				next.ServeHTTP(w, r)
				return
			}

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
	if !strings.HasPrefix(path, "/rpc/") || strings.HasPrefix(path, authRPCPrefix) {
		return false
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	case http.MethodDelete, http.MethodPut, http.MethodPatch:
		return true
	}

	method := path[strings.LastIndex(path, ".")+1:]
	// Only a verb known to be a read passes; unclassified verbs are blocked.
	return classifyDemoVerb(leadingLowerWord(method)) != demoVerbRead
}

// demoVerbClass is how DemoOrgWriteGuard classifies an RPC method-name verb.
type demoVerbClass int

const (
	// demoVerbUnclassified is a verb in neither map — an endpoint added since
	// the maps were last reviewed. The guard blocks it in the demo org, and
	// TestDemoGuardClassifiesEveryRPCVerb fails until someone triages it.
	demoVerbUnclassified demoVerbClass = iota
	demoVerbRead
	demoVerbMutating
)

func classifyDemoVerb(verb string) demoVerbClass {
	if _, ok := demoReadOnlyVerbs[verb]; ok {
		return demoVerbRead
	}
	if _, ok := demoMutatingVerbs[verb]; ok {
		return demoVerbMutating
	}
	return demoVerbUnclassified
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
