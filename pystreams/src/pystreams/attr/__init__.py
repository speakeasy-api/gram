from typing import Final

from opentelemetry.semconv.attributes import deployment_attributes, service_attributes

SERVICE_NAME = service_attributes.SERVICE_NAME
SERVICE_VERSION = service_attributes.SERVICE_VERSION
SERVICE_ENVIRONMENT = deployment_attributes.DEPLOYMENT_ENVIRONMENT_NAME

# Datadog source-code integration: stamp the build's commit and repo onto every
# resource so traces/metrics link back to the exact line of code.
DATADOG_GIT_COMMIT_SHA: Final = "git.commit.sha"
DATADOG_GIT_REPOSITORY_URL: Final = "git.repository_url"

COMPONENT: Final = "gram.component"
ORGANIZATION_ID: Final = "gram.org.id"
ORGANIZATION_SLUG: Final = "gram.org.slug"
PROJECT_ID: Final = "gram.project.id"
PROJECT_SLUG: Final = "gram.project.slug"
STREAMS_PROCESSOR_ID: Final = "gram.streams.processor_id"
TOPIC_PROTO_NAME: Final = "gram.topic.proto_name"
SUBSCRIPTION_PROTO_NAME: Final = "gram.subscription.proto_name"

ERROR_ID: Final = "error.id"
ERROR_MESSAGE: Final = "error.message"
ERROR_STACK: Final = "error.stack"

TRACE_ID: Final = "trace.id"
SPAN_ID: Final = "span.id"
DATADOG_TRACE_ID: Final = "dd.trace_id"
DATADOG_SPAN_ID: Final = "dd.span_id"
