---
"server": patch
---

Speakeasy now writes its own log records into the OTel pipeline through one internal package, and the `/otel/v1/*` ingest endpoints accept exports through the same publishing path. This is internal plumbing: nothing changes for customers sending telemetry.
