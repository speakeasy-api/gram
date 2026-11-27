---
"server": patch
---

Hook events and telemetry that were queued before an organization was disabled are no longer processed after it is disabled. Queued logs, metrics and spans are dropped, signal evaluation skips its messages, and risk scans leave captured messages unscanned so they are scanned if the organization is re-enabled within the scan window.
