---
"server": minor
---

Assistant runtimes can now export agent traces (turns, tool calls) over OTLP
to any OpenTelemetry-compatible backend such as Sentry, Datadog, or Honeycomb.
Export is enabled by configuring an OTLP endpoint for assistant runtimes, with
gRPC and HTTP transports supported; traces are tagged with the assistant and
project they belong to.
