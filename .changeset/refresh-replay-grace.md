---
"server": patch
---

Keep a rotated MCP refresh token redeemable for its successor's access-token lifetime instead of 30 seconds, and follow later rotations, so clients that share one token store across several windows no longer fall out with `refresh_token is unknown or already used`.
