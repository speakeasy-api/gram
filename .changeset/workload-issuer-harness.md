---
"server": patch
---

Test-only: drive workload assertions against a dev-idp standing in for the workload platform, served over HTTPS with real discovery and a published key set: verification with a key set fetched over the network, including key rotation and an unreachable issuer, and the full admission pipeline over real issuer and admission rows, covering tenancy, withdrawn issuers, project-tier isolation and the absence of requests to an untrusted issuer. devidptest gains opt-in TLS, key rotation, stopping and request counting to support it
