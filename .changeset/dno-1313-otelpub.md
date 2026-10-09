---
"server": patch
---

Speakeasy now has one internal path for writing its own log records into the OTel pipeline, applying the same tenancy and validation rules as the `/otel/v1/*` ingest endpoints. This is internal plumbing: nothing changes for customers sending telemetry.
