---
"server": minor
---

Remove the legacy Speakeasy IDP authentication layer and migrate to WorkOS-native auth. Authorization, token exchange, and session management now go directly through the WorkOS SDK instead of the intermediate Speakeasy IDP proxy. Deterministic UUIDv5 user/org IDs bridge cross-system identity without runtime lookups. Adds OAuth CSRF nonce validation and browser-binding cookie to the login flow.
