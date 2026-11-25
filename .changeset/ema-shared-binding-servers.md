---
"server": minor
"dashboard": minor
---

Enterprise Managed Authorization is shared by every server in a project that uses the same sign-in and service. The server's settings now say so. Turn on and Turn off, including turning it off for an old credential, name the other servers they also change, and an on setting lists the servers it is shared with. Once it is on, the list includes servers reached through a gateway, tunneled servers, and servers whose protected resource names the service. Before it is on, Turn on asks first only when another server on the same sign-in and service could share it, and then says those servers may change too; it says the same when the list can't be read. Switching the credential says the credential changes on the other servers. readEMA returns those servers, naming only the ones you can read, and `servers_complete` says whether the list is whole.
