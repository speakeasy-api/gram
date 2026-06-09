---
"server": patch
---

Stop logging client cancellations (`context.Canceled`) as 500 server faults. When an HTTP client disconnects mid-request, `oops` now detects the cancellation at the error boundary, logs it at info level (no error log, no errored span, no exception event), and maps it to HTTP 499 instead of a 500 fault. Detection requires both a `context.Canceled` cause and a canceled request context, so server-initiated cancellations (e.g. graceful shutdown) and application-initiated cancellations (e.g. an `errgroup` or an explicitly cancelled derived context, whose parent request context is still live), along with `context.DeadlineExceeded` and all other errors, keep full error severity.
