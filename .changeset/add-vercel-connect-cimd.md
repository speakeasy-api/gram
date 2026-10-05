---
"server": patch
---

Add Vercel Connect connectors wildcard to CIMD known clients. Vercel Connect (https://connect.vercel.com/connectors/*) mints one Client ID Metadata Document per connector with server-generated IDs. This is the first catalog entry using `private_key_jwt` authentication with `jwks_uri`. Authorization codes and tokens reach only Vercel's hosted callback, signed by Vercel's issuer keys.
