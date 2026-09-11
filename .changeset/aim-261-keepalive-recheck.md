---
"server": minor
---

Re-check idle remote sessions that carry no refresh token or refresh expiry on a keepalive sweep, so a revoked no-expiry credential reads rejected (or inactive when the provider's introspection says so) within the re-check interval without anyone opening the consent page. The sweep runs on the server process, is paced per issuer host, skips tunneled members whose tunnel is down, and records its probes under the new `keepalive` trigger on `gram.remote_session.validation`.
