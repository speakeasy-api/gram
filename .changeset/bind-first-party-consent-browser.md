---
"server": patch
---

A dashboard connection's consent page, including the Code Mode tool review, now only works in the browser that started the sign-in, and so does finishing a sign-in to an upstream service started from that page. Another browser that opens either link is sent to start again. Sign-ins already in progress when this ships finish as before, except one that started on an updated server but finished signing in on a server that wasn't updated yet when its consent page is on a different host from sign-in: connecting an upstream service from that page asks the person to start again.
