package attr

import (
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
)

type Key = attribute.Key

const (
	ServiceNameKey    = semconv.ServiceNameKey
	ServiceVersionKey = semconv.ServiceVersionKey

	DeviceKey              = semconv.SystemDeviceKey
	ExceptionStacktraceKey = semconv.ExceptionStacktraceKey
	ErrorMessageKey        = semconv.ErrorMessageKey
	ErrorIDKey             = attribute.Key("gram.error.id")
	ProcessExitCodeKey     = semconv.ProcessExitCodeKey
	ServerAddressKey       = semconv.ServerAddressKey
	DurationKey            = attribute.Key("duration")

	ComponentKey = attribute.Key("gram.component")

	ProjectIDKey    = attribute.Key("gram.project.id")
	ProjectSlugKey  = attribute.Key("gram.project.slug")
	DeploymentIDKey = attribute.Key("gram.deployment.id")
	FunctionIDKey   = attribute.Key("gram.function.id")
	AssetIDKey      = attribute.Key("gram.asset.id")
	URNKey          = attribute.Key("gram.urn")

	EventPayloadKey = attribute.Key("gram.event.payload")
	EventOriginKey  = attribute.Key("gram.event.origin")

	SpanIDKey         = attribute.Key("span.id")
	TraceIDKey        = attribute.Key("trace.id")
	DataDogTraceIDKey = attribute.Key("dd.trace_id")
	DataDogSpanIDKey  = attribute.Key("dd.span_id")
)

func ServiceName(v string) attribute.KeyValue { return ServiceNameKey.String(v) }
func SlogServiceName(v string) slog.Attr      { return slog.String(string(ServiceNameKey), v) }

func ServiceVersion(v string) attribute.KeyValue { return ServiceVersionKey.String(v) }
func SlogServiceVersion(v string) slog.Attr {
	return slog.String(string(ServiceVersionKey), v)
}

func Device(v string) attribute.KeyValue { return DeviceKey.String(v) }
func SlogDevice(v string) slog.Attr      { return slog.String(string(DeviceKey), v) }

func ExceptionStacktrace(v string) attribute.KeyValue { return ExceptionStacktraceKey.String(v) }
func SlogExceptionStacktrace(v string) slog.Attr {
	return slog.String(string(ExceptionStacktraceKey), v)
}

func Error(v error) attribute.KeyValue { return ErrorMessageKey.String(v.Error()) }
func SlogError(v error) slog.Attr      { return slog.String(string(ErrorMessageKey), v.Error()) }

func ErrorMessage(v string) attribute.KeyValue { return ErrorMessageKey.String(v) }
func SlogErrorMessage(v string) slog.Attr      { return slog.String(string(ErrorMessageKey), v) }

func ErrorID(v string) attribute.KeyValue { return ErrorIDKey.String(v) }
func SlogErrorID(v string) slog.Attr      { return slog.String(string(ErrorIDKey), v) }

func ProcessExitCode(v int) attribute.KeyValue { return ProcessExitCodeKey.Int(v) }
func SlogProcessExitCode(v int) slog.Attr      { return slog.Int(string(ProcessExitCodeKey), v) }

func ServerAddress(v string) attribute.KeyValue { return ServerAddressKey.String(v) }
func SlogServerAddress(v string) slog.Attr      { return slog.String(string(ServerAddressKey), v) }

func Duration(v time.Duration) attribute.KeyValue { return DurationKey.Float64(v.Seconds()) }
func SlogDuration(v time.Duration) slog.Attr      { return slog.Float64(string(DurationKey), v.Seconds()) }

func Component(v string) attribute.KeyValue { return ComponentKey.String(v) }
func SlogComponent(v string) slog.Attr      { return slog.String(string(ComponentKey), v) }

func ProjectID(v string) attribute.KeyValue { return ProjectIDKey.String(v) }
func SlogProjectID(v string) slog.Attr      { return slog.String(string(ProjectIDKey), v) }

func ProjectSlug(v string) attribute.KeyValue { return ProjectSlugKey.String(v) }
func SlogProjectSlug(v string) slog.Attr      { return slog.String(string(ProjectSlugKey), v) }

func DeploymentID(v string) attribute.KeyValue { return DeploymentIDKey.String(v) }
func SlogDeploymentID(v string) slog.Attr      { return slog.String(string(DeploymentIDKey), v) }

func FunctionID(v string) attribute.KeyValue { return FunctionIDKey.String(v) }
func SlogFunctionID(v string) slog.Attr {
	return slog.String(string(FunctionIDKey), v)
}

func AssetID(v string) attribute.KeyValue { return AssetIDKey.String(v) }
func SlogAssetID(v string) slog.Attr      { return slog.String(string(AssetIDKey), v) }

func URN(v string) attribute.KeyValue { return URNKey.String(v) }
func SlogURN(v string) slog.Attr      { return slog.String(string(URNKey), v) }

func EventPayload(v string) attribute.KeyValue { return EventPayloadKey.String(v) }
func SlogEventPayload(v string) slog.Attr      { return slog.String(string(EventPayloadKey), v) }

func EventOrigin(v string) attribute.KeyValue { return EventOriginKey.String(v) }
func SlogEventOrigin(v string) slog.Attr      { return slog.String(string(EventOriginKey), v) }

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
