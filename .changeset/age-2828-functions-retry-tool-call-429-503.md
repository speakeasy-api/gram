---
"server": minor
---

Gram Functions tool-call and resource-read POSTs now retry on a saturated runner's `429 + Retry-After` and Fly's `503` (both guaranteed before the function runs) instead of surfacing transient saturation as a hard failure, with jittered backoff to spread simultaneous retries and avoid a thundering herd. Transport errors that are transparently retried now log at `WARN` rather than `ERROR`, so recovered attempts no longer look like failures while the final unrecovered failure is still logged as an error.
