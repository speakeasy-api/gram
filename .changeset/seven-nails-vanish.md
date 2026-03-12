---
"dashboard": patch
---

Make deployment interactions non-blocking by passing `nonBlocking: true` to create/evolve API calls. The UI now polls for deployment completion instead of blocking the request, preventing timeouts on long-running deployments. Added error handling for polling failures so the UI shows an error state instead of getting stuck on a permanent spinner.
