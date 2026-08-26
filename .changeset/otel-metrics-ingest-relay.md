---
"server": minor
---

Gram's public server and SDK now accept authenticated OpenTelemetry Protocol metric exports at `/otel/v1/metrics` and relay them to configured OpenTelemetry destinations while preserving producer metric data, resources, instrumentation scopes, and schema URLs.
