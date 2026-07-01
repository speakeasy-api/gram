package attr

import (
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

type Key = attribute.Key

const (
	ErrorMessageKey                 = attribute.Key("error.message")
	ErrorStackKey                   = attribute.Key("error.stack")
	ErrorKindKey                    = attribute.Key("error.kind")
	FilePathKey                     = semconv.FilePathKey
	ServiceEnvKey                   = semconv.DeploymentEnvironmentNameKey
	ServiceNameKey                  = semconv.ServiceNameKey
	ServiceVersionKey               = semconv.ServiceVersionKey
	ValueKey                        = attribute.Key("value")
	CountKey                        = attribute.Key("count")
	DataDogGitCommitSHAKey          = attribute.Key("git.commit.sha")
	DataDogGitRepoURLKey            = attribute.Key("git.repository_url")
	DataDogTraceIDKey               = attribute.Key("dd.trace_id")
	DataDogSpanIDKey                = attribute.Key("dd.span_id")
	SpanIDKey                       = attribute.Key("span.id")
	TraceIDKey                      = attribute.Key("trace.id")
	GCPTopicQualifiedNameKey        = attribute.Key("gram.topic.qualified_name")
	GCPSubscriptionQualifiedNameKey = attribute.Key("gram.subscription.qualified_name")
	TopicProtoNameKey               = attribute.Key("gram.topic.proto_name")
	SubscriptionProtoNameKey        = attribute.Key("gram.subscription.proto_name")
	SubscriberBatchSizeKey          = attribute.Key("gram.subscriber.batch_size")
	SubscriberMessageIDKey          = attribute.Key("gram.subscriber.message_id")
	SubscriberDeliveryAttemptKey    = attribute.Key("gram.subscriber.delivery_attempt")
)

func Error(v error) attribute.KeyValue { return ErrorMessageKey.String(v.Error()) }
func SlogError(v error) slog.Attr      { return slog.String(string(ErrorMessageKey), v.Error()) }

func ErrorStack(v string) attribute.KeyValue { return ErrorStackKey.String(v) }
func SlogErrorStack(v string) slog.Attr      { return slog.String(string(ErrorStackKey), v) }

func ErrorKind(v string) attribute.KeyValue { return ErrorKindKey.String(v) }
func SlogErrorKind(v string) slog.Attr      { return slog.String(string(ErrorKindKey), v) }

func ErrorMessage(v string) attribute.KeyValue { return ErrorMessageKey.String(v) }
func SlogErrorMessage(v string) slog.Attr      { return slog.String(string(ErrorMessageKey), v) }

func FilePath(v string) attribute.KeyValue { return FilePathKey.String(v) }
func SlogFilePath(v string) slog.Attr      { return slog.String(string(FilePathKey), v) }

func Value(v string) attribute.KeyValue { return ValueKey.String(v) }
func SlogValue(v string) slog.Attr      { return slog.String(string(ValueKey), v) }

func Count(v int) attribute.KeyValue { return CountKey.Int(v) }
func SlogCount(v int) slog.Attr      { return slog.Int(string(CountKey), v) }

func ServiceName(v string) attribute.KeyValue { return ServiceNameKey.String(v) }
func SlogServiceName(v string) slog.Attr      { return slog.String(string(ServiceNameKey), v) }

func ServiceVersion(v string) attribute.KeyValue { return ServiceVersionKey.String(v) }
func SlogServiceVersion(v string) slog.Attr      { return slog.String(string(ServiceVersionKey), v) }

func ServiceEnv(v string) attribute.KeyValue { return ServiceEnvKey.String(v) }
func SlogServiceEnv(v string) slog.Attr      { return slog.String(string(ServiceEnvKey), v) }

func DataDogGitCommitSHA(v string) attribute.KeyValue { return DataDogGitCommitSHAKey.String(v) }
func SlogDataDogGitCommitSHA(v string) slog.Attr {
	return slog.String(string(DataDogGitCommitSHAKey), v)
}

func DataDogGitRepoURL(v string) attribute.KeyValue { return DataDogGitRepoURLKey.String(v) }
func SlogDataDogGitRepoURL(v string) slog.Attr      { return slog.String(string(DataDogGitRepoURLKey), v) }

func SlogValueAny(v any) slog.Attr { return slog.Any(string(ValueKey), v) }

func DataDogTraceID(v string) attribute.KeyValue { return DataDogTraceIDKey.String(v) }
func SlogDataDogTraceID(v string) slog.Attr      { return slog.String(string(DataDogTraceIDKey), v) }

func DataDogSpanID(v string) attribute.KeyValue { return DataDogSpanIDKey.String(v) }
func SlogDataDogSpanID(v string) slog.Attr      { return slog.String(string(DataDogSpanIDKey), v) }

func TraceID(v string) attribute.KeyValue { return TraceIDKey.String(v) }
func SlogTraceID(v string) slog.Attr      { return slog.String(string(TraceIDKey), v) }

func SpanID(v string) attribute.KeyValue { return SpanIDKey.String(v) }
func SlogSpanID(v string) slog.Attr      { return slog.String(string(SpanIDKey), v) }

func GCPTopicQualifiedName(v string) attribute.KeyValue { return GCPTopicQualifiedNameKey.String(v) }
func SlogGCPTopicQualifiedName(v string) slog.Attr {
	return slog.String(string(GCPTopicQualifiedNameKey), v)
}

func GCPSubscriptionQualifiedName(v string) attribute.KeyValue {
	return GCPSubscriptionQualifiedNameKey.String(v)
}
func SlogGCPSubscriptionQualifiedName(v string) slog.Attr {
	return slog.String(string(GCPSubscriptionQualifiedNameKey), v)
}

func TopicProtoName[S ~string](v S) attribute.KeyValue { return TopicProtoNameKey.String(string(v)) }
func SlogTopicProtoName[S ~string](v S) slog.Attr {
	return slog.String(string(TopicProtoNameKey), string(v))
}

func SubscriptionProtoName[S ~string](v S) attribute.KeyValue {
	return SubscriptionProtoNameKey.String(string(v))
}
func SlogSubscriptionProtoName[S ~string](v S) slog.Attr {
	return slog.String(string(SubscriptionProtoNameKey), string(v))
}

func SubscriberBatchSize(v int) attribute.KeyValue { return SubscriberBatchSizeKey.Int(v) }
func SlogSubscriberBatchSize(v int) slog.Attr      { return slog.Int(string(SubscriberBatchSizeKey), v) }

func SubscriberMessageID[S ~string](v S) attribute.KeyValue {
	return SubscriberMessageIDKey.String(string(v))
}
func SlogSubscriberMessageID[S ~string](v S) slog.Attr {
	return slog.String(string(SubscriberMessageIDKey), string(v))
}

func SubscriberDeliveryAttempt(v *int) attribute.KeyValue {
	if v == nil {
		return SubscriberDeliveryAttemptKey.Int(-1)
	}

	return SubscriberDeliveryAttemptKey.Int(*v)
}
func SlogSubscriberDeliveryAttempt(v *int) slog.Attr {
	if v == nil {
		return slog.Int(string(SubscriberDeliveryAttemptKey), -1)
	}
	return slog.Int(string(SubscriberDeliveryAttemptKey), *v)
}
