package attr

import (
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

type Key = attribute.Key

const (
	DeviceKey              = semconv.SystemDeviceKey
	ExceptionStacktraceKey = semconv.ExceptionStacktraceKey
	ErrorMessageKey        = semconv.ErrorMessageKey
	ErrorIDKey             = attribute.Key("gram.error.id")
	ProcessExitCodeKey     = semconv.ProcessExitCodeKey
	ServerAddressKey       = semconv.ServerAddressKey

	ComponentKey = attribute.Key("gram.component")

	ProjectIDKey    = attribute.Key("gram.project.id")
	ProjectSlugKey  = attribute.Key("gram.project.slug")
	DeploymentIDKey = attribute.Key("gram.deployment.id")
	FunctionIDKey   = attribute.Key("gram.function.id")

	EventPayloadKey = attribute.Key("gram.event.payload")
	EventOriginKey  = attribute.Key("gram.event.origin")

	SpanIDKey         = attribute.Key("span.id")
	TraceIDKey        = attribute.Key("trace.id")
	DataDogTraceIDKey = attribute.Key("dd.trace_id")
	DataDogSpanIDKey  = attribute.Key("dd.span_id")
)

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
