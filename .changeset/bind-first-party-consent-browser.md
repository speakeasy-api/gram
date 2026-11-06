---
"server": patch
---

A dashboard connection now only works in the browser that started it: signing in, the consent page (including the Code Mode tool review), and finishing a sign-in to an upstream service started from that page. Another browser that opens any of these links is sent to start again. A connection now has ten minutes from when it starts, the same as a trusted-provider sign-in. When it runs out, the page says the link expired. A sign-in restarted because the earlier one was revoked gets a fresh ten minutes. Sign-ins already in progress when this ships finish as before. One exception: when the consent page is on a different host from the sign-in (a custom domain or the authentication host), a sign-in that was in progress while this was rolling out may need to be started again.
