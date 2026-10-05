---
"server": minor
"dashboard": minor
---

The Access Hub catalog now offers Claude Tag. Its card opens a page listing the access allowed under the platform, and **Register new access** opens a guided setup that asks only for the Anthropic organization ID, an agent and optional tags, then trusts the platform and allows every channel in that organization in one step. Catalog platforms and their setup steps are YAML files shipped with the server, validated when loaded, and served by the new `workloadIdentities.listPlatforms` method, which requires `workload:read`.
