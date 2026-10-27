---
"server": minor
"dashboard": minor
---

Administrators can now correct a remote session client's client ID in place instead of deleting and recreating the client. A corrected ID revokes the sessions obtained through the old one, so users reconnect once. Clients can also be given a display name; a client without one is shown by its client ID.
