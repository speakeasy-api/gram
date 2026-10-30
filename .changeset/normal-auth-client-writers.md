---
"server": patch
---

Keep Speakeasy authentication-client choices synchronized during OAuth client setup, credential-header configuration, and Enterprise Managed Authorization preparation without rerunning a migration. Publish bindings and typed associations atomically, preserve administrator detaches and disabled clients, and never grant access automatically.

Use a consistent lock order across configuration writers so concurrent setup preserves completed provider registrations and administrator decisions. Keep Enterprise Managed Authorization resource identifiers exact, so URLs that differ by a trailing slash do not share authentication choices.
