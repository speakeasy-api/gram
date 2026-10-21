---
"cli": patch
---

`speakeasy auth` now binds its browser login to the attempt that started it. The local callback URL carries a random single-use state token, and the CLI rejects any callback that does not present it, so another local user or process that finds the callback port can no longer swap in an API key for a different organization. Rejected callbacks leave the login waiting for the real one. The browser is opened through a private redirect file so the token never appears in process arguments, and the sign-in URL is always printed in case the browser does not open.
