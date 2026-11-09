---
"server": patch
---

Gateways record an authorization check only for the servers a call names. Admitting and listing members, filtering a server's tools, and building the consent page's tool inventory no longer record a denied `mcp:connect` check for every private server or tool the caller has no grant for, so an agent's Failed authorization checks show only real denials. A request records each named server's check once, and each distinct refused tool once. A call that names a public server whose toolset is private now records that toolset's denial on organization and agent gateways too. A direct hosted server's tools/list still records its connection check, but no longer one check per tool.
