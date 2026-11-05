---
"server": patch
---

With upstream auth enforcement on, a change to a server's derived authorization server no longer invalidates saved consent or an approved gateway connection, and people are told to reconnect whenever a new consent can offer them a connection. Removing a server's own sign-in client from it no longer fails with a conflict.
