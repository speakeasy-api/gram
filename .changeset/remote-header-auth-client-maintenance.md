---
"server": patch
---

A credential source's typed upstream auth clients, server links and copied credential headers now stay in line as the source and its servers are written, instead of waiting for the next organization-wide backfill run. Adding, rotating or deleting a remote source's header records or updates its client and the copies on the upstream OAuth and EMA clients its servers offer; deleting the source or the server retires them; and a server created in front of a source that already carries headers picks them up as it appears. Tunneled sources get the same treatment through their own deletion and the servers arriving on or leaving them. Since the serving path sends an auth client's copied headers with the upstream request, a deleted header could previously still be sent, and a rotated one sent with its old value, until the next run. Administrator-created clients, administrator detachments and credential choices are left untouched.
