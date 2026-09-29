---
"hooks": patch
---

Stop failing gating hooks closed over a slow network path, and say what actually failed when they do. The gate budget now derives from the deadline the provider already imposes (capped, with the previous 5s value as the floor for the shim path) instead of holding every provider to the tightest one's wall, and connect plus TLS handshake are bounded separately so a stalled connection fails fast and leaves room for a replay rather than consuming the whole budget. Transport failures now report their cause — dns, tls, timeout, canceled, connection, unreadable-response — in the blocked tool call and the debug log, in place of the opaque "Speakeasy hook returned HTTP 0".
