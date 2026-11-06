---
"server": patch
---

With upstream auth enforcement on, a change to a server's derived authorization server no longer invalidates saved consent or an approved gateway connection, and people are told to reconnect whenever a new consent can offer them a connection. With enforcement on, detaching a server's own sign-in client stops the server offering it instead of failing with a conflict; the underlying association is preserved.
