---
"dashboard": patch
---

Gateway connection health no longer suggests admitting a workload or trusting its issuer as the fix for a refused workload assertion, since workloads cannot connect to a gateway directly; it now says to connect the workload to an MCP server. Attempts that used a JWT bearer grant are labeled "Signed assertion" instead of "Enterprise assertion", because the same grant carries both workload and enterprise assertions.
