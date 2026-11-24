---
"server": patch
"tunnel": patch
---

The Redis instance switch can move between instances that differ in TLS and authentication. Each new socket is encrypted and authenticated inside the switch's dialer for the instance it chose, and the client sends no credentials of its own, so a move that lands while a connection is opening can no longer send one instance's password or IAM token to the other. Pooled TLS connections to the current instance are reused instead of being redialled, and those the move closed are dropped. Switches are kept per instance and role (`auth`, `ops`, or the existing `all`), each role with its own next instance and the shared hold key, and dials right after a move are spread over up to 250 ms. With no next instance configured, clients are built exactly as before.
