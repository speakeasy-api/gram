---
"dashboard": patch
"server": patch
---

Editing a Shadow MCP policy no longer fails with "cannot change the sources or action of a shadow mcp policy with a disposition". A `block_all` policy — the default, and what every policy the dashboard creates stores — can once again turn its Shadow MCP detector off or move off the Deny action, exactly as the policies that predate the disposition field always could; the server now retires the policy's allowed-server grants along with the posture instead of leaving them behind. An `allow_all` policy still cannot, because its blocked-server list would have nothing left to enforce, but the editor now pins its detector and action up front with the reason, and the remaining server-side rejections say which part of the edit was refused and what to do instead.
