package netingress

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
)

type RouteSurface string

type RouteID string

const (
	RouteSurfaceMCP  RouteSurface = "mcp"
	RouteSurfaceXMCP RouteSurface = "xmcp"

	RouteRuntime              RouteID = "runtime"
	RouteInstall              RouteID = "install"
	RouteInstallScript        RouteID = "install_script"
	RouteProtectedResource    RouteID = "protected_resource"
	RouteAuthorizationServer  RouteID = "authorization_server"
	RouteRegister             RouteID = "register"
	RouteAuthorize            RouteID = "authorize"
	RouteConnect              RouteID = "connect"
	RouteConnectRemoteSession RouteID = "connect_remote_session"
	RouteConnectMCP           RouteID = "connect_mcp"
	RouteConnectFirstParty    RouteID = "connect_first_party"
	RouteConsentScript        RouteID = "consent_script"
	RouteConsentToolsScript   RouteID = "consent_tools_script"
	RouteToken                RouteID = "token"
	RouteRevoke               RouteID = "revoke"
)

type RouteSpec struct {
	Surface RouteSurface
	ID      RouteID
	Method  string
	Path    string
}

var privateRoutes = []RouteSpec{
	{Surface: RouteSurfaceMCP, ID: RouteRuntime, Method: http.MethodDelete, Path: "/mcp/{mcpSlug}"},
	{Surface: RouteSurfaceMCP, ID: RouteRuntime, Method: http.MethodGet, Path: "/mcp/{mcpSlug}"},
	{Surface: RouteSurfaceMCP, ID: RouteRuntime, Method: http.MethodPost, Path: "/mcp/{mcpSlug}"},
	{Surface: RouteSurfaceMCP, ID: RouteInstall, Method: http.MethodGet, Path: "/mcp/{mcpSlug}/install"},
	{Surface: RouteSurfaceMCP, ID: RouteInstallScript, Method: http.MethodGet, Path: "/mcp/install-page-{hash}.js"},
	{Surface: RouteSurfaceMCP, ID: RouteProtectedResource, Method: http.MethodGet, Path: wellknown.OAuthProtectedResourcePath + "/mcp/{mcpSlug}"},
	{Surface: RouteSurfaceMCP, ID: RouteAuthorizationServer, Method: http.MethodGet, Path: wellknown.OAuthAuthorizationServerPath + "/mcp/{mcpSlug}"},
	{Surface: RouteSurfaceMCP, ID: RouteRegister, Method: http.MethodPost, Path: "/mcp/{mcpSlug}/register"},
	{Surface: RouteSurfaceMCP, ID: RouteAuthorize, Method: http.MethodGet, Path: "/mcp/{mcpSlug}/authorize"},
	{Surface: RouteSurfaceMCP, ID: RouteConnect, Method: http.MethodGet, Path: "/mcp/{mcpSlug}/connect"},
	{Surface: RouteSurfaceMCP, ID: RouteConnect, Method: http.MethodPost, Path: "/mcp/{mcpSlug}/connect"},
	{Surface: RouteSurfaceMCP, ID: RouteConnectRemoteSession, Method: http.MethodPost, Path: "/mcp/{mcpSlug}/connect/remote-session"},
	{Surface: RouteSurfaceMCP, ID: RouteConnectMCP, Method: http.MethodDelete, Path: "/mcp/{mcpSlug}/connect/mcp"},
	{Surface: RouteSurfaceMCP, ID: RouteConnectMCP, Method: http.MethodPost, Path: "/mcp/{mcpSlug}/connect/mcp"},
	{Surface: RouteSurfaceMCP, ID: RouteConnectFirstParty, Method: http.MethodGet, Path: "/mcp/{mcpSlug}/connect/first-party"},
	{Surface: RouteSurfaceMCP, ID: RouteConsentScript, Method: http.MethodGet, Path: "/mcp/consent-page-{hash}.js"},
	{Surface: RouteSurfaceMCP, ID: RouteConsentToolsScript, Method: http.MethodGet, Path: "/mcp/consent-tools-{hash}.js"},
	{Surface: RouteSurfaceMCP, ID: RouteToken, Method: http.MethodPost, Path: "/mcp/{mcpSlug}/token"},
	{Surface: RouteSurfaceMCP, ID: RouteRevoke, Method: http.MethodPost, Path: "/mcp/{mcpSlug}/revoke"},

	{Surface: RouteSurfaceXMCP, ID: RouteRuntime, Method: http.MethodDelete, Path: "/x/mcp/{slug}"},
	{Surface: RouteSurfaceXMCP, ID: RouteRuntime, Method: http.MethodGet, Path: "/x/mcp/{slug}"},
	{Surface: RouteSurfaceXMCP, ID: RouteRuntime, Method: http.MethodPost, Path: "/x/mcp/{slug}"},
	{Surface: RouteSurfaceXMCP, ID: RouteInstall, Method: http.MethodGet, Path: "/x/mcp/{mcpSlug}/install"},
	{Surface: RouteSurfaceXMCP, ID: RouteProtectedResource, Method: http.MethodGet, Path: wellknown.OAuthProtectedResourcePath + "/x/mcp/{mcpSlug}"},
	{Surface: RouteSurfaceXMCP, ID: RouteAuthorizationServer, Method: http.MethodGet, Path: wellknown.OAuthAuthorizationServerPath + "/x/mcp/{mcpSlug}"},
	{Surface: RouteSurfaceXMCP, ID: RouteRegister, Method: http.MethodPost, Path: "/x/mcp/{mcpSlug}/register"},
	{Surface: RouteSurfaceXMCP, ID: RouteAuthorize, Method: http.MethodGet, Path: "/x/mcp/{mcpSlug}/authorize"},
	{Surface: RouteSurfaceXMCP, ID: RouteConnect, Method: http.MethodGet, Path: "/x/mcp/{mcpSlug}/connect"},
	{Surface: RouteSurfaceXMCP, ID: RouteConnect, Method: http.MethodPost, Path: "/x/mcp/{mcpSlug}/connect"},
	{Surface: RouteSurfaceXMCP, ID: RouteConnectRemoteSession, Method: http.MethodPost, Path: "/x/mcp/{mcpSlug}/connect/remote-session"},
	{Surface: RouteSurfaceXMCP, ID: RouteConnectMCP, Method: http.MethodDelete, Path: "/x/mcp/{mcpSlug}/connect/mcp"},
	{Surface: RouteSurfaceXMCP, ID: RouteConnectMCP, Method: http.MethodPost, Path: "/x/mcp/{mcpSlug}/connect/mcp"},
	{Surface: RouteSurfaceXMCP, ID: RouteConnectFirstParty, Method: http.MethodGet, Path: "/x/mcp/{mcpSlug}/connect/first-party"},
	{Surface: RouteSurfaceXMCP, ID: RouteToken, Method: http.MethodPost, Path: "/x/mcp/{mcpSlug}/token"},
	{Surface: RouteSurfaceXMCP, ID: RouteRevoke, Method: http.MethodPost, Path: "/x/mcp/{mcpSlug}/revoke"},
}

var privateRouteMatcher = newPrivateRouteMatcher()

func PrivateRoutes(surface RouteSurface) []RouteSpec {
	routes := make([]RouteSpec, 0, len(privateRoutes))
	for _, route := range privateRoutes {
		if route.Surface == surface {
			routes = append(routes, route)
		}
	}
	return routes
}

// IsPrivateRoute reports whether method and path belong to the deliberately
// small HTTP surface reachable through private network ingress. The same route
// catalog drives handler registration on the private application mux.
func IsPrivateRoute(method, requestPath string) bool {
	if isReservedGlobalPath(method, requestPath) {
		return false
	}
	ctx := chi.NewRouteContext()
	return privateRouteMatcher.Match(ctx, method, requestPath)
}

func isReservedGlobalPath(method, requestPath string) bool {
	if strings.HasSuffix(requestPath, "/") || strings.Contains(requestPath, "//") || strings.Contains(requestPath, "/./") || strings.Contains(requestPath, "/../") {
		return true
	}
	for _, callback := range []string{"/mcp/idp_callback", "/mcp/remote_login_callback"} {
		if requestPath == callback || strings.HasPrefix(requestPath, callback+"/") {
			return true
		}
	}
	for _, prefix := range []string{"/mcp/install-page-", "/mcp/consent-page-", "/mcp/consent-tools-"} {
		if !strings.HasPrefix(requestPath, prefix) {
			continue
		}
		value := strings.TrimPrefix(requestPath, prefix)
		hash, hasSuffix := strings.CutSuffix(value, ".js")
		return method != http.MethodGet || !hasSuffix || hash == "" || strings.Contains(hash, "/")
	}
	return false
}

// RouteGuard rejects everything outside the private ingress route census before
// workload attestation or application middleware runs.
func RouteGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsPrivateRoute(r.Method, r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func newPrivateRouteMatcher() *chi.Mux {
	matcher := chi.NewRouter()
	for _, route := range privateRoutes {
		matcher.Method(route.Method, route.Path, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	}
	return matcher
}
