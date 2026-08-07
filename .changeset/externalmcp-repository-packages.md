---
"server": minor
---

Catalog entries from external MCP registries now carry the registry's linked source repository and published packages, which the registry client previously dropped. Both are registry declarations — nothing ties a linked repository or package to what a remote endpoint actually runs — and the API descriptions say so. These feed the MCP approval evidence surface and the upcoming artifact pin-and-fetch work.
