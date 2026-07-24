---
"server": minor
"dashboard": minor
---

Add platform-admin management of Gram's own platform-level external credentials (starting with the ambient GCP identity) via a new `adminExternalCredentials` API (create, read, update, delete) and an "External Services" section in the organization settings with a creation sheet and a per-credential detail page. Includes a live "who am I" Verify probe backed by a reusable `gcpauth` identity resolver.
