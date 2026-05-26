---
"server": minor
---

add `remoteMcp.discoverProtectedResourceMetadata` endpoint that probes a remote MCP server for an RFC 9728 OAuth Protected Resource Metadata document server-side under `guardian.Policy`, since external resource servers are unlikely to allowlist the Gram dashboard origin via CORS; follows RFC 9728 §3.1 path-style + origin-style discovery and returns typed unavailability codes with backend-composed user messages
