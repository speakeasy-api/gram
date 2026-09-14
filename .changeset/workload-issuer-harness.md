---
"server": patch
---

Test-only: stand a real OIDC issuer up as a workload platform in the oauthtest harness, and drive workload assertions against it: verification with a key set fetched over the network, and the full admission pipeline over real issuer and admission rows, covering tenancy, withdrawn issuers, project-tier isolation and the absence of outbound requests for an untrusted issuer
