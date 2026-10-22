---
"server": patch
---

MCP clients that run in a VM, a container or over SSH can now finish signing in. When a client receives its authorization at a local address, the consent page offers "Finishing on another machine?". Ticked, Give access shows a `curl` command (and the plain address, for clients that ask for it) to run on the machine where the client listens, instead of sending this browser to a local address it cannot reach. The code in it works once, only for the client that started the sign-in, and expires in 10 minutes.
