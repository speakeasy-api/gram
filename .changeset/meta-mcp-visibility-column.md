---
"server": patch
---

Adds a `visibility` column to `meta_mcp_servers`, mirroring `mcp_servers.visibility`, so a Gateway Endpoint can express whether it is disabled or requires an authenticated caller. Today a gateway has no such column, so the runtime infers that from whether an issuer happens to be attached — an overload that cannot represent a disabled gateway at all.

Schema only: nothing reads the column yet, so behavior is unchanged. It defaults to `private`, the closed state.
