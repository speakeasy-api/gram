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
	ExceptionStacktraceKey            = semconv.ExceptionStacktraceKey
	ContainerIDKey                    = semconv.ContainerIDKey
	ContainerNetworkIDKey             = attribute.Key("container.network.id")
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
	HTTPClientRequestDurationKey      = attribute.Key("http.client.request.duration_ms")
	HTTPRequestSizeKey                = semconv.HTTPRequestSizeKey
	HTTPResponseSizeKey               = semconv.HTTPResponseSizeKey
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

	SpanIDKey              = attribute.Key("span.id")
	TraceIDKey             = attribute.Key("trace.id")
	DataDogGitCommitSHAKey = attribute.Key("git.commit.sha")
	DataDogGitRepoURLKey   = attribute.Key("git.repository_url")
	DataDogTraceIDKey      = attribute.Key("dd.trace_id")
	DataDogSpanIDKey       = attribute.Key("dd.span_id")

	FlyAppNameKey    = attribute.Key("fly.app.name")
	FlyOrgIDKey      = attribute.Key("fly.org.id")
	FlyOrgSlugKey    = attribute.Key("fly.org.slug")
	FlyMachineIDsKey = attribute.Key("fly.machine_ids")

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
	AssetURLKey                    = attribute.Key("gram.asset.url")
	MCPRegistryIDKey               = attribute.Key("gram.mcp_registry.id")
	MCPRegistryURLKey              = attribute.Key("gram.mcp_registry.url")
	ExternalMCPIDKey               = attribute.Key("gram.external_mcp.id")
	ExternalMCPSlugKey             = attribute.Key("gram.external_mcp.slug")
	ExternalMCPNameKey             = attribute.Key("gram.external_mcp.name")
	URLKey                         = attribute.Key("url")
	CacheKeyKey                    = attribute.Key("gram.cache.key")
	CacheNamespaceKey              = attribute.Key("gram.cache.namespace")
	ComponentKey                   = attribute.Key("gram.component")
	DBDeletedRowsCountKey          = attribute.Key("gram.db.deleted_rows_count")
	DeploymentIDKey                = attribute.Key("gram.deployment.id")
	DeploymentFunctionsAccessIDKey = attribute.Key("gram.deployment.functions.access_id")
	DeploymentFunctionsIDKey       = attribute.Key("gram.deployment.functions.id")
	DeploymentFunctionsNameKey     = attribute.Key("gram.deployment.functions.name")
	DeploymentFunctionsSlugKey     = attribute.Key("gram.deployment.functions.slug")
	DeploymentOpenAPIIDKey         = attribute.Key("gram.deployment.openapi.id")
	DeploymentOpenAPINameKey       = attribute.Key("gram.deployment.openapi.name")
	DeploymentOpenAPIParserKey     = attribute.Key("gram.deployment.openapi_parser")
	DeploymentOpenAPISlugKey       = attribute.Key("gram.deployment.openapi.slug")
	DeploymentStatusKey            = attribute.Key("gram.deployment.status")
	EnvironmentIDKey               = attribute.Key("gram.environment.id")
	EnvironmentSlugKey             = attribute.Key("gram.environment.slug")
	EnvVarNameKey                  = attribute.Key("gram.envvar.name")
	ErrorIDKey                     = attribute.Key("gram.error.id")
	FilterExpressionKey            = attribute.Key("gram.filter.src")
	FlyAppInternalIDKey            = attribute.Key("gram.fly.app_id")
	FunctionsBackendKey            = attribute.Key("gram.functions.backend")
	FunctionsManifestVersionKey    = attribute.Key("gram.functions.manifest_version")
	FunctionsRunnerImageKey        = attribute.Key("gram.functions.runner_image")
	FunctionsRunnerVersionKey      = attribute.Key("gram.functions.runner_version")
	FunctionsRuntimeKey            = attribute.Key("gram.functions.runtime")
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
	MimeTypeKey                    = attribute.Key("mime.type")
	OAuthClientIDKey               = attribute.Key("gram.oauth.client_id")
	OAuthCodeKey                   = attribute.Key("gram.oauth.code")
	OAuthExternalCodeKey           = attribute.Key("gram.oauth.external_code")
	OAuthGrantKey                  = attribute.Key("gram.oauth.grant")
	OAuthProviderKey               = attribute.Key("gram.oauth.provider")
	OAuthRedirectURIFullKey        = attribute.Key("gram.oauth.redirect_uri.full")
	OAuthRequiredKey               = attribute.Key("gram.oauth.required")
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
	ProductFeatureNameKey          = attribute.Key("gram.product.feature.name")
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
	ToolCallKindKey                = attribute.Key("gram.tool_call.kind")
	ToolCallSourceKey              = attribute.Key("gram.tool_call.source")
	ToolHTTPResponseContentTypeKey = attribute.Key("gram.tool.http.response.content_type")
	ToolIDKey                      = attribute.Key("gram.tool.id")
	ToolURNKey                     = attribute.Key("gram.tool.urn")
	ToolNameKey                    = attribute.Key("gram.tool.name")
	ResourceIDKey                  = attribute.Key("gram.resource.id")
	ResourceNameKey                = attribute.Key("gram.resource.name")
	ResourceURNKey                 = attribute.Key("gram.resource.urn")
	ResourceURIKey                 = attribute.Key("gram.resource.uri")
	ToolsetIDKey                   = attribute.Key("gram.toolset.id")
	ToolsetSlugKey                 = attribute.Key("gram.toolset.slug")
	VisibilityKey                  = attribute.Key("gram.visibility")

	PaginationTsStartKey     = attribute.Key("gram.pagination.ts_start")
	PaginationTsEndKey       = attribute.Key("gram.pagination.ts_end")
	PaginationCursorKey      = attribute.Key("gram.pagination.cursor")
	PaginationLimitKey       = attribute.Key("gram.pagination.limit")
	PaginationSortOrderKey   = attribute.Key("gram.pagination.sort_order")
	PaginationHasNextPageKey = attribute.Key("gram.pagination.has_next_page")

	ClickhouseQueryDurationMsKey = attribute.Key("gram.clickhouse.query_duration_ms")

	RetryAttemptKey = attribute.Key("retry.attempt")
	RetryWaitKey    = attribute.Key("retry.wait")
)

const (
	VisibilityInternalValue = "internal"
)

func Error(v error) attribute.KeyValue { return ErrorMessageKey.String(v.Error()) }
func SlogError(v error) slog.Attr      { return slog.String(string(ErrorMessageKey), v.Error()) }

func ErrorMessage(v string) attribute.KeyValue { return ErrorMessageKey.String(v) }
func SlogErrorMessage(v string) slog.Attr      { return slog.String(string(ErrorMessageKey), v) }

func ExceptionStacktrace(v string) attribute.KeyValue { return ExceptionStacktraceKey.String(v) }
func SlogExceptionStacktrace(v string) slog.Attr {
	return slog.String(string(ExceptionStacktraceKey), v)
}

func ContainerID(v string) attribute.KeyValue { return ContainerIDKey.String(v) }
func SlogContainerID(v string) slog.Attr      { return slog.String(string(ContainerIDKey), v) }

func ContainerNetworkID(v string) attribute.KeyValue { return ContainerNetworkIDKey.String(v) }
func SlogContainerNetworkID(v string) slog.Attr {
	return slog.String(string(ContainerNetworkIDKey), v)
}

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

func DataDogGitCommitSHA(v string) attribute.KeyValue { return DataDogGitCommitSHAKey.String(v) }
func SlogDataDogGitCommitSHA(v string) slog.Attr {
	return slog.String(string(DataDogGitCommitSHAKey), v)
}

func DataDogGitRepoURL(v string) attribute.KeyValue { return DataDogGitRepoURLKey.String(v) }
func SlogDataDogGitRepoURL(v string) slog.Attr      { return slog.String(string(DataDogGitRepoURLKey), v) }

func DataDogTraceID(v string) attribute.KeyValue { return DataDogTraceIDKey.String(v) }
func SlogDataDogTraceID(v string) slog.Attr {
	return slog.String(string(DataDogTraceIDKey), v)
}

func DataDogSpanID(v string) attribute.KeyValue { return DataDogSpanIDKey.String(v) }
func SlogDataDogSpanID(v string) slog.Attr {
	return slog.String(string(DataDogSpanIDKey), v)
}

func FlyAppName(v string) attribute.KeyValue { return FlyAppNameKey.String(v) }
func SlogFlyAppName(v string) slog.Attr      { return slog.String(string(FlyAppNameKey), v) }

func FlyOrgID(v string) attribute.KeyValue { return FlyOrgIDKey.String(v) }
func SlogFlyOrgID(v string) slog.Attr      { return slog.String(string(FlyOrgIDKey), v) }

func FlyOrgSlug(v string) attribute.KeyValue { return FlyOrgSlugKey.String(v) }
func SlogFlyOrgSlug(v string) slog.Attr      { return slog.String(string(FlyOrgSlugKey), v) }

func FlyMachineIDs(v []string) attribute.KeyValue { return FlyMachineIDsKey.StringSlice(v) }
func SlogFlyMachineIDs(v []string) slog.Attr      { return slog.Any(string(FlyMachineIDsKey), v) }

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

func AssetURL(v string) attribute.KeyValue { return AssetURLKey.String(v) }
func SlogAssetURL(v string) slog.Attr      { return slog.String(string(AssetURLKey), v) }

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

func DeploymentFunctionsAccessID(v string) attribute.KeyValue {
	return DeploymentFunctionsAccessIDKey.String(v)
}
func SlogDeploymentFunctionsAccessID(v string) slog.Attr {
	return slog.String(string(DeploymentFunctionsAccessIDKey), v)
}

func DeploymentFunctionsID(v string) attribute.KeyValue { return DeploymentFunctionsIDKey.String(v) }
func SlogDeploymentFunctionsID(v string) slog.Attr {
	return slog.String(string(DeploymentFunctionsIDKey), v)
}

func DeploymentFunctionsName(v string) attribute.KeyValue {
	return DeploymentFunctionsNameKey.String(v)
}
func SlogDeploymentFunctionsName(v string) slog.Attr {
	return slog.String(string(DeploymentFunctionsNameKey), v)
}

func DeploymentFunctionsSlug[V ~string](v V) attribute.KeyValue {
	return DeploymentFunctionsSlugKey.String(string(v))
}
func SlogDeploymentFunctionsSlug[V ~string](v V) slog.Attr {
	return slog.String(string(DeploymentFunctionsSlugKey), string(v))
}

func DeploymentOpenAPIID(v string) attribute.KeyValue { return DeploymentOpenAPIIDKey.String(v) }
func SlogDeploymentOpenAPIID(v string) slog.Attr {
	return slog.String(string(DeploymentOpenAPIIDKey), v)
}

func DeploymentOpenAPIName(v string) attribute.KeyValue { return DeploymentOpenAPINameKey.String(v) }
func SlogDeploymentOpenAPIName(v string) slog.Attr {
	return slog.String(string(DeploymentOpenAPINameKey), v)
}

func DeploymentOpenAPIParser(v string) attribute.KeyValue {
	return DeploymentOpenAPIParserKey.String(v)
}
func SlogDeploymentOpenAPIParser(v string) slog.Attr {
	return slog.String(string(DeploymentOpenAPIParserKey), v)
}

func DeploymentOpenAPISlug[V ~string](v V) attribute.KeyValue {
	return DeploymentOpenAPISlugKey.String(string(v))
}
func SlogDeploymentOpenAPISlug[V ~string](v V) slog.Attr {
	return slog.String(string(DeploymentOpenAPISlugKey), string(v))
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

func FlyAppInternalID(v string) attribute.KeyValue { return FlyAppInternalIDKey.String(v) }
func SlogFlyAppInternalID(v string) slog.Attr      { return slog.String(string(FlyAppInternalIDKey), v) }

func FunctionsBackend(v string) attribute.KeyValue { return FunctionsBackendKey.String(v) }
func SlogFunctionsBackend(v string) slog.Attr      { return slog.String(string(FunctionsBackendKey), v) }

func FunctionsManifestVersion(v string) attribute.KeyValue {
	return FunctionsManifestVersionKey.String(v)
}
func SlogFunctionsManifestVersion(v string) slog.Attr {
	return slog.String(string(FunctionsManifestVersionKey), v)
}

func FunctionsRunnerImage(v string) attribute.KeyValue { return FunctionsRunnerImageKey.String(v) }
func SlogFunctionsRunnerImage(v string) slog.Attr {
	return slog.String(string(FunctionsRunnerImageKey), v)
}

func FunctionsRunnerVersion[V ~string](v V) attribute.KeyValue {
	return FunctionsRunnerVersionKey.String(string(v))
}
func SlogFunctionsRunnerVersion[V ~string](v V) slog.Attr {
	return slog.String(string(FunctionsRunnerVersionKey), string(v))
}

func FunctionsRuntime[V ~string](v V) attribute.KeyValue {
	return FunctionsRuntimeKey.String(string(v))
}
func SlogFunctionsRuntime[V ~string](v V) slog.Attr {
	return slog.String(string(FunctionsRuntimeKey), string(v))
}

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

func OAuthRequired(v bool) attribute.KeyValue { return OAuthRequiredKey.Bool(v) }
func SlogOAuthRequired(v bool) slog.Attr      { return slog.Bool(string(OAuthRequiredKey), v) }

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

func Outcome[V ~string](v V) attribute.KeyValue { return OutcomeKey.String(string(v)) }
func SlogOutcome(v string) slog.Attr            { return slog.String(string(OutcomeKey), v) }

func PackageName(v string) attribute.KeyValue { return PackageNameKey.String(v) }
func SlogPackageName(v string) slog.Attr      { return slog.String(string(PackageNameKey), v) }

func PackageVersion(v string) attribute.KeyValue { return PackageVersionKey.String(v) }
func SlogPackageVersion(v string) slog.Attr      { return slog.String(string(PackageVersionKey), v) }

func PKCEMethod(v string) attribute.KeyValue { return PKCEMethodKey.String(v) }
func SlogPKCEMethod(v string) slog.Attr      { return slog.String(string(PKCEMethodKey), v) }

func ProductFeatureName(v string) attribute.KeyValue { return ProductFeatureNameKey.String(v) }
func SlogProductFeatureName(v string) slog.Attr {
	return slog.String(string(ProductFeatureNameKey), v)
}

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

func ToolCallKind[V string](v V) attribute.KeyValue { return ToolCallKindKey.String(string(v)) }
func SlogToolCallKind[V string](v V) slog.Attr {
	return slog.String(string(ToolCallKindKey), string(v))
}

func ToolCallSource(v string) attribute.KeyValue { return ToolCallSourceKey.String(v) }
func SlogToolCallSource(v string) slog.Attr      { return slog.String(string(ToolCallSourceKey), v) }

func ToolID(v string) attribute.KeyValue { return ToolIDKey.String(v) }
func SlogToolID(v string) slog.Attr      { return slog.String(string(ToolIDKey), v) }

func ToolURN(v string) attribute.KeyValue { return ToolURNKey.String(v) }
func SlogToolURN(v string) slog.Attr      { return slog.String(string(ToolURNKey), v) }

func ToolName(v string) attribute.KeyValue { return ToolNameKey.String(v) }
func SlogToolName(v string) slog.Attr      { return slog.String(string(ToolNameKey), v) }

func ResourceID(v string) attribute.KeyValue { return ResourceIDKey.String(v) }
func SlogResourceID(v string) slog.Attr      { return slog.String(string(ResourceIDKey), v) }

func ResourceName(v string) attribute.KeyValue { return ResourceNameKey.String(v) }
func SlogResourceName(v string) slog.Attr      { return slog.String(string(ResourceNameKey), v) }

func ResourceURN(v string) attribute.KeyValue { return ResourceURNKey.String(v) }
func SlogResourceURN(v string) slog.Attr      { return slog.String(string(ResourceURNKey), v) }

func ResourceURI(v string) attribute.KeyValue { return ResourceURIKey.String(v) }
func SlogResourceURI(v string) slog.Attr      { return slog.String(string(ResourceURIKey), v) }

func ToolsetID(v string) attribute.KeyValue { return ToolsetIDKey.String(v) }
func SlogToolsetID(v string) slog.Attr      { return slog.String(string(ToolsetIDKey), v) }

func ToolsetSlug(v string) attribute.KeyValue { return ToolsetSlugKey.String(v) }
func SlogToolsetSlug(v string) slog.Attr      { return slog.String(string(ToolsetSlugKey), v) }

func McpURL(v string) attribute.KeyValue { return McpURLKey.String(v) }
func SlogMcpURL(v string) slog.Attr      { return slog.String(string(McpURLKey), v) }

func McpMethod(v string) attribute.KeyValue { return McpMethodKey.String(v) }
func SlogMcpMethod(v string) slog.Attr      { return slog.String(string(McpMethodKey), v) }

func MimeType(v string) attribute.KeyValue { return MimeTypeKey.String(v) }
func SlogMimeType(v string) slog.Attr      { return slog.String(string(MimeTypeKey), v) }

func ToolCallDuration(v time.Duration) attribute.KeyValue {
	return ToolCallDurationKey.Float64(v.Seconds())
}
func SlogToolCallDuration(v time.Duration) slog.Attr {
	return slog.Float64(string(ToolCallDurationKey), v.Seconds())
}

func VisibilityInternal() attribute.KeyValue {
	return VisibilityKey.String(VisibilityInternalValue)
}
func SlogVisibilityInternal() slog.Attr {
	return slog.String(string(VisibilityKey), VisibilityInternalValue)
}

func PaginationTsStart(v time.Time) attribute.KeyValue {
	return PaginationTsStartKey.String(v.Format(time.RFC3339))
}
func SlogPaginationTsStart(v time.Time) slog.Attr {
	return slog.Time(string(PaginationTsStartKey), v)
}

func PaginationTsEnd(v time.Time) attribute.KeyValue {
	return PaginationTsEndKey.String(v.Format(time.RFC3339))
}
func SlogPaginationTsEnd(v time.Time) slog.Attr {
	return slog.Time(string(PaginationTsEndKey), v)
}

func PaginationCursor(v string) attribute.KeyValue {
	return PaginationCursorKey.String(v)
}
func SlogPaginationCursor(v time.Time) slog.Attr {
	return slog.Time(string(PaginationCursorKey), v)
}

func PaginationLimit(v int) attribute.KeyValue { return PaginationLimitKey.Int(v) }
func SlogPaginationLimit(v int) slog.Attr {
	return slog.Int(string(PaginationLimitKey), v)
}

func PaginationSortOrder(v string) attribute.KeyValue { return PaginationSortOrderKey.String(v) }
func SlogPaginationSortOrder(v string) slog.Attr {
	return slog.String(string(PaginationSortOrderKey), v)
}

func HTTPClientRequestDuration(v float64) attribute.KeyValue {
	return HTTPClientRequestDurationKey.Float64(v)
}
func SlogHTTPClientRequestDuration(v float64) slog.Attr {
	return slog.Float64(string(HTTPClientRequestDurationKey), v)
}

func HTTPRequestBodyBytes(v int) attribute.KeyValue { return HTTPRequestSizeKey.Int(v) }
func SlogHTTPRequestBodyBytes(v int) slog.Attr {
	return slog.Int(string(HTTPRequestSizeKey), v)
}

func HTTPResponseBodyBytes(v int) attribute.KeyValue { return HTTPResponseSizeKey.Int(v) }
func SlogHTTPResponseBodyBytes(v int) slog.Attr {
	return slog.Int(string(HTTPResponseSizeKey), v)
}

func PaginationHasNextPage(v bool) attribute.KeyValue { return PaginationHasNextPageKey.Bool(v) }
func SlogPaginationHasNextPage(v bool) slog.Attr {
	return slog.Bool(string(PaginationHasNextPageKey), v)
}

func ClickhouseQueryDurationMs(v float64) attribute.KeyValue {
	return ClickhouseQueryDurationMsKey.Float64(v)
}
func SlogClickhouseQueryDurationMs(v float64) slog.Attr {
	return slog.Float64(string(ClickhouseQueryDurationMsKey), v)
}

func MCPRegistryID(v string) attribute.KeyValue { return MCPRegistryIDKey.String(v) }
func SlogMCPRegistryID(v string) slog.Attr      { return slog.String(string(MCPRegistryIDKey), v) }

func MCPRegistryURL(v string) attribute.KeyValue { return MCPRegistryURLKey.String(v) }
func SlogMCPRegistryURL(v string) slog.Attr      { return slog.String(string(MCPRegistryURLKey), v) }

func ExternalMCPID(v string) attribute.KeyValue { return ExternalMCPIDKey.String(v) }
func SlogExternalMCPID(v string) slog.Attr      { return slog.String(string(ExternalMCPIDKey), v) }

func ExternalMCPSlug(v string) attribute.KeyValue { return ExternalMCPSlugKey.String(v) }
func SlogExternalMCPSlug(v string) slog.Attr      { return slog.String(string(ExternalMCPSlugKey), v) }

func ExternalMCPName(v string) attribute.KeyValue { return ExternalMCPNameKey.String(v) }
func SlogExternalMCPName(v string) slog.Attr      { return slog.String(string(ExternalMCPNameKey), v) }

func URL(v string) attribute.KeyValue { return URLKey.String(v) }
func SlogURL(v string) slog.Attr      { return slog.String(string(URLKey), v) }

func RetryAttempt(v int) attribute.KeyValue { return RetryAttemptKey.Int(v) }
func SlogRetryAttempt(v int) slog.Attr      { return slog.Int(string(RetryAttemptKey), v) }

func RetryWait(v time.Duration) attribute.KeyValue { return RetryWaitKey.String(v.String()) }
func SlogRetryWait(v time.Duration) slog.Attr      { return slog.Duration(string(RetryWaitKey), v) }
