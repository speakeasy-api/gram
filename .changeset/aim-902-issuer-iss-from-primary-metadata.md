---
"server": patch
---

Sign-in no longer fails with "remote login callback did not identify the expected issuer" for providers such as Zoom, whose OAuth metadata does not advertise RFC 9207 `iss` support but whose OpenID metadata does. Speakeasy now reads that setting only from the provider's primary metadata document, and affected providers recover after their next metadata refresh.
