---
"server": patch
---

The gateway sign-in page now says why a server needs attention, in the same words as its connection card, instead of always showing "Needs reconnecting". When reconnecting cannot fix the problem, the page says what does (choose a connection, or ask an admin) and does not offer a reconnect.

A tool call to a server that gained a sign-in after you connected now says to sign in to the gateway again from your AI client.

The server now warns, once per person, server and reason, when a gateway server has no usable connection choice, and counts these cases in a new metric. It also warns when a new connection fails its check right after sign-in, or when a provider reports an authorization lifetime that has already ended.
