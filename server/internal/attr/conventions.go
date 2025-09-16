package attr

import (
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

type Key = attribute.Key

const (
	ErrorMessageKey                   = semconv.ErrorMessageKey
	FilePathKey                       = semconv.FilePathKey
	HostNameKey                       = semconv.HostNameKey
	HTTPRequestHeaderContentTypeKey   = attribute.Key("http.request.header.content_type")
	HTTPRequestHeaderUserAgentKey     = attribute.Key("http.request.header.user_agent")
	HTTPRequestMethodKey              = semconv.HTTPRequestMethodKey
	HTTPRequestBodyKey                = attribute.Key("http.request.body")
	HTTPRequestHeadersKey             = attribute.Key("http.request.headers")
	HTTPResponseHeaderContentTypeKey  = attribute.Key("http.response.header.content_type")
	HTTPResponseStatusCodeKey         = semconv.HTTPResponseStatusCodeKey
	HTTPResponseOriginalStatusCodeKey = attribute.Key("http.response.original_status_code")
	HTTPRouteKey                      = semconv.HTTPRouteKey
	HTTPServerRequestDurationKey      = attribute.Key("http.server.request.duration")
	ServerAddressKey                  = semconv.ServerAddressKey
	ServiceEnvKey                     = semconv.DeploymentEnvironmentNameKey
	ServiceNameKey                    = semconv.ServiceNameKey
	ServiceVersionKey                 = semconv.ServiceVersionKey
	URLDomainKey                      = semconv.URLDomainKey
	URLFullKey                        = semconv.URLFullKey
	URLOriginalKey                    = semconv.URLOriginalKey
	UserIDKey                         = semconv.UserIDKey

	ActualKey   = attribute.Key("actual")
	EventKey    = attribute.Key("event")
	ExpectedKey = attribute.Key("expected")
	NameKey     = attribute.Key("name")
	ReasonKey   = attribute.Key("reason")
	ValueKey    = attribute.Key("value")

	SpanIDKey         = attribute.Key("span.id")
	TraceIDKey        = attribute.Key("trace.id")
	DataDogTraceIDKey = attribute.Key("dd.trace_id")
	DataDogSpanIDKey  = attribute.Key("dd.span_id")

	GoaServiceKey = attribute.Key("goa.service")
	GoaMethodKey  = attribute.Key("goa.method")

	TemporalNamespaceNameKey = attribute.Key("temporal.namespace.name")
	TemporalTaskQueueNameKey = attribute.Key("temporal.taskqueue.name")
	TemporalWorkerIDKey      = attribute.Key("temporal.worker.id")
	TemporalActivityIDKey    = attribute.Key("temporal.activity.id")
	TemporalActivityTypeKey  = attribute.Key("temporal.activity.type")
	TemporalAttemptKey       = attribute.Key("temporal.attempt")
	TemporalWorkflowTypeKey  = attribute.Key("temporal.workflow.type")
	TemporalWorkflowIDKey    = attribute.Key("temporal.workflow.id")
	TemporalRunIDKey         = attribute.Key("temporal.run.id")

	AssetIDKey                     = attribute.Key("gram.asset.id")
	CacheKeyKey                    = attribute.Key("gram.cache.key")
	CacheNamespaceKey              = attribute.Key("gram.cache.namespace")
	ComponentKey                   = attribute.Key("gram.component")
	DBDeletedRowsCountKey          = attribute.Key("gram.db.deleted_rows_count")
	DeploymentIDKey                = attribute.Key("gram.deployment.id")
	DeploymentOpenAPIIDKey         = attribute.Key("gram.deployment.openapi.id")
	DeploymentOpenAPINameKey       = attribute.Key("gram.deployment.openapi.name")
	DeploymentOpenAPISlugKey       = attribute.Key("gram.deployment.openapi.slug")
	DeploymentStatusKey            = attribute.Key("gram.deployment.status")
	EnvironmentIDKey               = attribute.Key("gram.environment.id")
	EnvironmentSlugKey             = attribute.Key("gram.environment.slug")
	EnvVarNameKey                  = attribute.Key("gram.envvar.name")
	ErrorIDKey                     = attribute.Key("gram.error.id")
	FilterExpressionKey            = attribute.Key("gram.filter.src")
	HTTPEncodingStyleKey           = attribute.Key("gram.http.encoding.style")
	HTTPParamNameKey               = attribute.Key("gram.http.param.name")
	HTTPParamValueKey              = attribute.Key("gram.http.param.value")
	HTTPResponseExternalKey        = attribute.Key("gram.http.response.external")
	HTTPResponseFilteredKey        = attribute.Key("gram.http.response.filtered")
	HTTPStatusCodePatternKey       = attribute.Key("gram.http.status_code_pattern")
	IngressNameKey                 = attribute.Key("gram.ingress.name")
	McpMethodKey                   = attribute.Key("gram.mcp.method")
	McpURLKey                      = attribute.Key("gram.mcp.url")
	MetricNameKey                  = attribute.Key("gram.metric.name")
	OAuthClientIDKey               = attribute.Key("gram.oauth.client_id")
	OAuthCodeKey                   = attribute.Key("gram.oauth.code")
	OAuthExternalCodeKey           = attribute.Key("gram.oauth.external_code")
	OAuthGrantKey                  = attribute.Key("gram.oauth.grant")
	OAuthProviderKey               = attribute.Key("gram.oauth.provider")
	OAuthRedirectURIFullKey        = attribute.Key("gram.oauth.redirect_uri.full")
	OAuthScopeKey                  = attribute.Key("gram.oauth.scope")
	OpenAPIMethodKey               = attribute.Key("gram.openapi.method")
	OpenAPIOperationIDKey          = attribute.Key("gram.openapi.operation_id")
	OpenAPIPathKey                 = attribute.Key("gram.openapi.path")
	OpenAPIVersionKey              = attribute.Key("gram.openapi.version")
	OpenRouterKeyLimitKey          = attribute.Key("gram.openrouter.key.limit")
	OrganizationAccountTypeKey     = attribute.Key("gram.org.account_type")
	OrganizationIDKey              = attribute.Key("gram.org.id")
	OrganizationSlugKey            = attribute.Key("gram.org.slug")
	OutcomeKey                     = attribute.Key("gram.outcome")
	PackageNameKey                 = attribute.Key("gram.package.name")
	PackageVersionKey              = attribute.Key("gram.package.version")
	PKCEMethodKey                  = attribute.Key("gram.pkce.method")
	ProjectIDKey                   = attribute.Key("gram.project.id")
	ProjectNameKey                 = attribute.Key("gram.project.name")
	ProjectSlugKey                 = attribute.Key("gram.project.slug")
	SecretNameKey                  = attribute.Key("gram.secret.name")
	SecurityPlacementKey           = attribute.Key("gram.security.placement")
	SecuritySchemeKey              = attribute.Key("gram.security.scheme")
	SecurityTypeKey                = attribute.Key("gram.security.type")
	SessionIDKey                   = attribute.Key("gram.session.id")
	SlackEventFullKey              = attribute.Key("gram.slack.event.full")
	SlackEventTypeKey              = attribute.Key("gram.slack.event.type")
	SlackTeamIDKey                 = attribute.Key("gram.slack.team.id")
	ToolCallDurationKey            = attribute.Key("gram.tool_call.duration")
	ToolCallSourceKey              = attribute.Key("gram.tool_call.source")
	ToolHTTPResponseContentTypeKey = attribute.Key("gram.tool.http.response.content_type")
	ToolIDKey                      = attribute.Key("gram.tool.id")
	ToolNameKey                    = attribute.Key("gram.tool.name")
	ToolsetIDKey                   = attribute.Key("gram.toolset.id")
	ToolsetSlugKey                 = attribute.Key("gram.toolset.slug")
)

func Error(v error) attribute.KeyValue { return ErrorMessageKey.String(v.Error()) }
func SlogError(v error) slog.Attr      { return slog.String(string(ErrorMessageKey), v.Error()) }

func ErrorMessage(v string) attribute.KeyValue { return ErrorMessageKey.String(v) }
func SlogErrorMessage(v string) slog.Attr      { return slog.String(string(ErrorMessageKey), v) }

func FilePath(v string) attribute.KeyValue { return FilePathKey.String(v) }
func SlogFilePath(v string) slog.Attr      { return slog.String(string(FilePathKey), v) }

func HostName(v string) attribute.KeyValue { return HostNameKey.String(v) }
func SlogHostName(v string) slog.Attr      { return slog.String(string(HostNameKey), v) }

func HTTPRequestHeaderContentType(v string) attribute.KeyValue {
	return HTTPRequestHeaderContentTypeKey.String(v)
}
func SlogHTTPRequestHeaderContentType(v string) slog.Attr {
	return slog.String(string(HTTPRequestHeaderContentTypeKey), v)
}

func HTTPRequestHeaderUserAgent(v string) attribute.KeyValue {
	return HTTPRequestHeaderUserAgentKey.String(v)
}
func SlogHTTPRequestHeaderUserAgent(v string) slog.Attr {
	return slog.String(string(HTTPRequestHeaderUserAgentKey), v)
}

func HTTPRequestBody(v string) attribute.KeyValue { return HTTPRequestBodyKey.String(v) }
func SlogHTTPRequestBody(v string) slog.Attr      { return slog.String(string(HTTPRequestBodyKey), v) }

func HTTPRequestHeaders(v any) attribute.KeyValue {
	return HTTPRequestHeadersKey.String(fmt.Sprintf("%v", v))
}
func SlogHTTPRequestHeaders(v any) slog.Attr { return slog.Any(string(HTTPRequestHeadersKey), v) }

func HTTPRequestMethod(v string) attribute.KeyValue { return HTTPRequestMethodKey.String(v) }
func SlogHTTPRequestMethod(v string) slog.Attr      { return slog.String(string(HTTPRequestMethodKey), v) }

func HTTPResponseHeaderContentType(v string) attribute.KeyValue {
	return HTTPResponseHeaderContentTypeKey.String(v)
}
func SlogHTTPResponseHeaderContentType(v string) slog.Attr {
	return slog.String(string(HTTPResponseHeaderContentTypeKey), v)
}

func HTTPResponseStatusCode(v int) attribute.KeyValue { return HTTPResponseStatusCodeKey.Int(v) }
func SlogHTTPResponseStatusCode(v int) slog.Attr {
	return slog.Int(string(HTTPResponseStatusCodeKey), v)
}

func HTTPResponseOriginalStatusCode(v int) attribute.KeyValue {
	return HTTPResponseOriginalStatusCodeKey.Int(v)
}
func SlogHTTPResponseOriginalStatusCode(v int) slog.Attr {
	return slog.Int(string(HTTPResponseOriginalStatusCodeKey), v)
}

func HTTPRoute(v string) attribute.KeyValue { return HTTPRouteKey.String(v) }
func SlogHTTPRoute(v string) slog.Attr      { return slog.String(string(HTTPRouteKey), v) }

func HTTPServerRequestDuration(v float64) attribute.KeyValue {
	return HTTPServerRequestDurationKey.Float64(v)
}
func SlogHTTPServerRequestDuration(v float64) slog.Attr {
	return slog.Float64(string(HTTPServerRequestDurationKey), v)
}

func ServerAddress(v string) attribute.KeyValue { return ServerAddressKey.String(v) }
func SlogServerAddress(v string) slog.Attr      { return slog.String(string(ServerAddressKey), v) }

func ServiceName(v string) attribute.KeyValue { return ServiceNameKey.String(v) }
func SlogServiceName(v string) slog.Attr      { return slog.String(string(ServiceNameKey), v) }

func ServiceEnv(v string) attribute.KeyValue { return ServiceEnvKey.String(v) }
func SlogServiceEnv(v string) slog.Attr      { return slog.String(string(ServiceEnvKey), v) }

func ServiceVersion(v string) attribute.KeyValue { return ServiceVersionKey.String(v) }
func SlogServiceVersion(v string) slog.Attr      { return slog.String(string(ServiceVersionKey), v) }

func URLDomain(v string) attribute.KeyValue { return URLDomainKey.String(v) }
func SlogURLDomain(v string) slog.Attr      { return slog.String(string(URLDomainKey), v) }

func URLFull(v string) attribute.KeyValue { return URLFullKey.String(v) }
func SlogURLFull(v string) slog.Attr      { return slog.String(string(URLFullKey), v) }

func URLOriginal(v string) attribute.KeyValue { return URLOriginalKey.String(v) }
func SlogURLOriginal(v string) slog.Attr      { return slog.String(string(URLOriginalKey), v) }

func UserID(v string) attribute.KeyValue { return UserIDKey.String(v) }
func SlogUserID(v string) slog.Attr      { return slog.String(string(UserIDKey), v) }

func Actual(v any) attribute.KeyValue { return ActualKey.String(fmt.Sprintf("%v", v)) }
func SlogActual(v any) slog.Attr      { return slog.Any(string(ActualKey), v) }

func Event(v string) attribute.KeyValue { return EventKey.String(v) }
func SlogEvent(v string) slog.Attr      { return slog.String(string(EventKey), v) }

func Expected(v any) attribute.KeyValue { return ExpectedKey.String(fmt.Sprintf("%v", v)) }
func SlogExpected(v any) slog.Attr      { return slog.Any(string(ExpectedKey), v) }

func Name(v string) attribute.KeyValue { return NameKey.String(v) }
func SlogName(v string) slog.Attr      { return slog.String(string(NameKey), v) }

func Reason(v string) attribute.KeyValue { return ReasonKey.String(v) }
func SlogReason(v string) slog.Attr      { return slog.String(string(ReasonKey), v) }

func SpanID(v string) attribute.KeyValue { return SpanIDKey.String(v) }
func SlogSpanID(v string) slog.Attr      { return slog.String(string(SpanIDKey), v) }

func TraceID(v string) attribute.KeyValue { return TraceIDKey.String(v) }
func SlogTraceID(v string) slog.Attr      { return slog.String(string(TraceIDKey), v) }

func DataDogTraceID(v string) attribute.KeyValue { return DataDogTraceIDKey.String(v) }
func SlogDataDogTraceID(v string) slog.Attr {
	return slog.String(string(DataDogTraceIDKey), v)
}

func DataDogSpanID(v string) attribute.KeyValue { return DataDogSpanIDKey.String(v) }
func SlogDataDogSpanID(v string) slog.Attr {
	return slog.String(string(DataDogSpanIDKey), v)
}

func ValueAny(v any) attribute.KeyValue       { return ValueKey.String(fmt.Sprintf("%v", v)) }
func SlogValueAny(v any) slog.Attr            { return slog.Any(string(ValueKey), v) }
func ValueString(v string) attribute.KeyValue { return ValueKey.String(v) }
func SlogValueString(v string) slog.Attr      { return slog.String(string(ValueKey), v) }
func ValueInt(v int) attribute.KeyValue       { return ValueKey.Int(v) }
func SlogValueInt(v int) slog.Attr            { return slog.Int(string(ValueKey), v) }

func GoaService(v string) attribute.KeyValue { return GoaServiceKey.String(v) }
func SlogGoaService(v string) slog.Attr      { return slog.String(string(GoaServiceKey), v) }

func GoaMethod(v string) attribute.KeyValue { return GoaMethodKey.String(v) }
func SlogGoaMethod(v string) slog.Attr      { return slog.String(string(GoaMethodKey), v) }

func AssetID(v string) attribute.KeyValue { return AssetIDKey.String(v) }
func SlogAssetID(v string) slog.Attr      { return slog.String(string(AssetIDKey), v) }

func CacheKey(v string) attribute.KeyValue { return CacheKeyKey.String(v) }
func SlogCacheKey(v string) slog.Attr      { return slog.String(string(CacheKeyKey), v) }

func CacheNamespace(v string) attribute.KeyValue { return CacheNamespaceKey.String(v) }
func SlogCacheNamespace(v string) slog.Attr      { return slog.String(string(CacheNamespaceKey), v) }

func Component(v string) attribute.KeyValue { return ComponentKey.String(v) }
func SlogComponent(v string) slog.Attr      { return slog.String(string(ComponentKey), v) }

func DBDeletedRowsCount(v int64) attribute.KeyValue { return DBDeletedRowsCountKey.Int64(v) }
func SlogDBDeletedRowsCount(v int64) slog.Attr      { return slog.Int64(string(DBDeletedRowsCountKey), v) }

func DeploymentID(v string) attribute.KeyValue { return DeploymentIDKey.String(v) }
func SlogDeploymentID(v string) slog.Attr      { return slog.String(string(DeploymentIDKey), v) }

func DeploymentOpenAPIID(v string) attribute.KeyValue { return DeploymentOpenAPIIDKey.String(v) }
func SlogDeploymentOpenAPIID(v string) slog.Attr {
	return slog.String(string(DeploymentOpenAPIIDKey), v)
}

func DeploymentOpenAPIName(v string) attribute.KeyValue { return DeploymentOpenAPINameKey.String(v) }
func SlogDeploymentOpenAPIName(v string) slog.Attr {
	return slog.String(string(DeploymentOpenAPINameKey), v)
}

func DeploymentOpenAPISlug(v string) attribute.KeyValue { return DeploymentOpenAPISlugKey.String(v) }
func SlogDeploymentOpenAPISlug(v string) slog.Attr {
	return slog.String(string(DeploymentOpenAPISlugKey), v)
}

func DeploymentStatus(v string) attribute.KeyValue { return DeploymentStatusKey.String(v) }
func SlogDeploymentStatus(v string) slog.Attr      { return slog.String(string(DeploymentStatusKey), v) }

func EnvironmentID(v string) attribute.KeyValue { return EnvironmentIDKey.String(v) }
func SlogEnvironmentID(v string) slog.Attr      { return slog.String(string(EnvironmentIDKey), v) }

func EnvironmentSlug(v string) attribute.KeyValue { return EnvironmentSlugKey.String(v) }
func SlogEnvironmentSlug(v string) slog.Attr      { return slog.String(string(EnvironmentSlugKey), v) }

func EnvVarName(v string) attribute.KeyValue { return EnvVarNameKey.String(v) }
func SlogEnvVarName(v string) slog.Attr      { return slog.String(string(EnvVarNameKey), v) }

func ErrorID(v string) attribute.KeyValue { return ErrorIDKey.String(v) }
func SlogErrorID(v string) slog.Attr      { return slog.String(string(ErrorIDKey), v) }

func FilterExpression(v string) attribute.KeyValue { return FilterExpressionKey.String(v) }
func SlogFilterExpression(v string) slog.Attr      { return slog.String(string(FilterExpressionKey), v) }

func HTTPEncodingStyle(v string) attribute.KeyValue { return HTTPEncodingStyleKey.String(v) }
func SlogHTTPEncodingStyle(v string) slog.Attr      { return slog.String(string(HTTPEncodingStyleKey), v) }

func HTTPResponseExternal(v bool) attribute.KeyValue { return HTTPResponseExternalKey.Bool(v) }
func SlogHTTPResponseExternal(v bool) slog.Attr      { return slog.Bool(string(HTTPResponseExternalKey), v) }

func HTTPResponseFiltered(v bool) attribute.KeyValue { return HTTPResponseFilteredKey.Bool(v) }
func SlogHTTPResponseFiltered(v bool) slog.Attr      { return slog.Bool(string(HTTPResponseFilteredKey), v) }

func HTTPStatusCodePattern(v string) attribute.KeyValue { return HTTPStatusCodePatternKey.String(v) }
func SlogHTTPStatusCodePattern(v string) slog.Attr {
	return slog.String(string(HTTPStatusCodePatternKey), v)
}

func HTTPParamName(v string) attribute.KeyValue { return HTTPParamNameKey.String(v) }
func SlogHTTPParamName(v string) slog.Attr      { return slog.String(string(HTTPParamNameKey), v) }

func HTTPParamValue(v any) attribute.KeyValue { return HTTPParamValueKey.String(fmt.Sprintf("%v", v)) }
func SlogHTTPParamValue(v any) slog.Attr      { return slog.Any(string(HTTPParamValueKey), v) }

func IngressName(v string) attribute.KeyValue { return IngressNameKey.String(v) }
func SlogIngressName(v string) slog.Attr      { return slog.String(string(IngressNameKey), v) }

func MetricName(v string) attribute.KeyValue { return MetricNameKey.String(v) }
func SlogMetricName(v string) slog.Attr      { return slog.String(string(MetricNameKey), v) }

func OAuthClientID(v string) attribute.KeyValue { return OAuthClientIDKey.String(v) }
func SlogOAuthClientID(v string) slog.Attr      { return slog.String(string(OAuthClientIDKey), v) }

func OAuthCode(v string) attribute.KeyValue { return OAuthCodeKey.String(v) }
func SlogOAuthCode(v string) slog.Attr      { return slog.String(string(OAuthCodeKey), v) }

func OAuthExternalCode(v string) attribute.KeyValue { return OAuthExternalCodeKey.String(v) }
func SlogOAuthExternalCode(v string) slog.Attr      { return slog.String(string(OAuthExternalCodeKey), v) }

func OAuthGrant(v string) attribute.KeyValue { return OAuthGrantKey.String(v) }
func SlogOAuthGrant(v string) slog.Attr      { return slog.String(string(OAuthGrantKey), v) }

func OAuthProvider(v string) attribute.KeyValue { return OAuthProviderKey.String(v) }
func SlogOAuthProvider(v string) slog.Attr      { return slog.String(string(OAuthProviderKey), v) }

func OAuthRedirectURIFull(v string) attribute.KeyValue { return OAuthRedirectURIFullKey.String(v) }
func SlogOAuthRedirectURIFull(v string) slog.Attr {
	return slog.String(string(OAuthRedirectURIFullKey), v)
}

func OAuthScope(v string) attribute.KeyValue { return OAuthScopeKey.String(v) }
func SlogOAuthScope(v string) slog.Attr      { return slog.String(string(OAuthScopeKey), v) }

func OpenAPIMethod(v string) attribute.KeyValue { return OpenAPIMethodKey.String(v) }
func SlogOpenAPIMethod(v string) slog.Attr      { return slog.String(string(OpenAPIMethodKey), v) }

func OpenAPIOperationID(v string) attribute.KeyValue { return OpenAPIOperationIDKey.String(v) }
func SlogOpenAPIOperationID(v string) slog.Attr {
	return slog.String(string(OpenAPIOperationIDKey), v)
}

func OpenAPIPath(v string) attribute.KeyValue { return OpenAPIPathKey.String(v) }
func SlogOpenAPIPath(v string) slog.Attr      { return slog.String(string(OpenAPIPathKey), v) }

func OpenAPIVersion(v string) attribute.KeyValue { return OpenAPIVersionKey.String(v) }
func SlogOpenAPIVersion(v string) slog.Attr      { return slog.String(string(OpenAPIVersionKey), v) }

func OpenRouterKeyLimit(v int) attribute.KeyValue { return OpenRouterKeyLimitKey.Int(v) }
func SlogOpenRouterKeyLimit(v int) slog.Attr      { return slog.Int(string(OpenRouterKeyLimitKey), v) }

func OrganizationID(v string) attribute.KeyValue { return OrganizationIDKey.String(v) }
func SlogOrganizationID(v string) slog.Attr      { return slog.String(string(OrganizationIDKey), v) }

func OrganizationSlug(v string) attribute.KeyValue { return OrganizationSlugKey.String(v) }
func SlogOrganizationSlug(v string) slog.Attr      { return slog.String(string(OrganizationSlugKey), v) }

func OrganizationAccountType(v string) attribute.KeyValue {
	return OrganizationAccountTypeKey.String(v)
}
func SlogOrganizationAccountType(v string) slog.Attr {
	return slog.String(string(OrganizationAccountTypeKey), v)
}

func Outcome(v string) attribute.KeyValue { return OutcomeKey.String(v) }
func SlogOutcome(v string) slog.Attr      { return slog.String(string(OutcomeKey), v) }

func PackageName(v string) attribute.KeyValue { return PackageNameKey.String(v) }
func SlogPackageName(v string) slog.Attr      { return slog.String(string(PackageNameKey), v) }

func PackageVersion(v string) attribute.KeyValue { return PackageVersionKey.String(v) }
func SlogPackageVersion(v string) slog.Attr      { return slog.String(string(PackageVersionKey), v) }

func PKCEMethod(v string) attribute.KeyValue { return PKCEMethodKey.String(v) }
func SlogPKCEMethod(v string) slog.Attr      { return slog.String(string(PKCEMethodKey), v) }

func ProjectID(v string) attribute.KeyValue { return ProjectIDKey.String(v) }
func SlogProjectID(v string) slog.Attr      { return slog.String(string(ProjectIDKey), v) }

func ProjectSlug(v string) attribute.KeyValue { return ProjectSlugKey.String(v) }
func SlogProjectSlug(v string) slog.Attr      { return slog.String(string(ProjectSlugKey), v) }

func ProjectName(v string) attribute.KeyValue { return ProjectNameKey.String(v) }
func SlogProjectName(v string) slog.Attr      { return slog.String(string(ProjectNameKey), v) }

func SecretName(v string) attribute.KeyValue { return SecretNameKey.String(v) }
func SlogSecretName(v string) slog.Attr      { return slog.String(string(SecretNameKey), v) }

func SecurityPlacement(v string) attribute.KeyValue { return SecurityPlacementKey.String(v) }
func SlogSecurityPlacement(v string) slog.Attr      { return slog.String(string(SecurityPlacementKey), v) }

func SecurityScheme(v string) attribute.KeyValue { return SecuritySchemeKey.String(v) }
func SlogSecurityScheme(v string) slog.Attr      { return slog.String(string(SecuritySchemeKey), v) }

func SecurityType(v string) attribute.KeyValue { return SecurityTypeKey.String(v) }
func SlogSecurityType(v string) slog.Attr      { return slog.String(string(SecurityTypeKey), v) }

func SessionID(v string) attribute.KeyValue { return SessionIDKey.String(v) }
func SlogSessionID(v string) slog.Attr      { return slog.String(string(SessionIDKey), v) }

func SlackEventFull(v any) attribute.KeyValue { return SlackEventFullKey.String(fmt.Sprintf("%v", v)) }
func SlogSlackEventFull(v any) slog.Attr      { return slog.Any(string(SlackEventFullKey), v) }

func SlackEventType(v string) attribute.KeyValue { return SlackEventTypeKey.String(v) }
func SlogSlackEventType(v string) slog.Attr      { return slog.String(string(SlackEventTypeKey), v) }

func SlackTeamID(v string) attribute.KeyValue { return SlackTeamIDKey.String(v) }
func SlogSlackTeamID(v string) slog.Attr      { return slog.String(string(SlackTeamIDKey), v) }

func ToolCallSource(v string) attribute.KeyValue { return ToolCallSourceKey.String(v) }
func SlogToolCallSource(v string) slog.Attr      { return slog.String(string(ToolCallSourceKey), v) }

func ToolID(v string) attribute.KeyValue { return ToolIDKey.String(v) }
func SlogToolID(v string) slog.Attr      { return slog.String(string(ToolIDKey), v) }

func ToolName(v string) attribute.KeyValue { return ToolNameKey.String(v) }
func SlogToolName(v string) slog.Attr      { return slog.String(string(ToolNameKey), v) }

func ToolsetID(v string) attribute.KeyValue { return ToolsetIDKey.String(v) }
func SlogToolsetID(v string) slog.Attr      { return slog.String(string(ToolsetIDKey), v) }

func ToolsetSlug(v string) attribute.KeyValue { return ToolsetSlugKey.String(v) }
func SlogToolsetSlug(v string) slog.Attr      { return slog.String(string(ToolsetSlugKey), v) }

func McpURL(v string) attribute.KeyValue { return McpURLKey.String(v) }
func SlogMcpURL(v string) slog.Attr      { return slog.String(string(McpURLKey), v) }

func McpMethod(v string) attribute.KeyValue { return McpMethodKey.String(v) }
func SlogMcpMethod(v string) slog.Attr      { return slog.String(string(McpMethodKey), v) }

func ToolCallDuration(v time.Duration) attribute.KeyValue {
	return ToolCallDurationKey.Float64(v.Seconds())
}
func SlogToolCallDuration(v time.Duration) slog.Attr {
	return slog.Float64(string(ToolCallDurationKey), v.Seconds())
}
