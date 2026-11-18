---
"server": minor
"dashboard": patch
---

Speakeasy can run with Functions turned off by setting `GRAM_FUNCTIONS_PROVIDER=disabled`. A disabled deployment needs no Fly.io or Tigris settings and never calls either. Function uploads and deployments that add function sources are refused with "functions are not available on this deployment", deployments that carry existing functions fail with that reason in their logs, and calling a function tool returns a failed tool result with the same message. Product features report the new `functions_available` field, and the dashboard hides the Functions entry points when it is false. The `local` and `flyio` providers are unchanged.
