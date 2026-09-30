---
"server": patch
---

Keep a rotated MCP refresh token redeemable for its immediate successor until that successor's access token expires, instead of 30 seconds, so clients that share one token store across several windows no longer fall out with `refresh_token is unknown or already used` on every hourly refresh.
