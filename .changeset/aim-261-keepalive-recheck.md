---
"server": minor
---

Re-check idle remote sessions that carry no refresh token or refresh expiry on a keepalive sweep, so a revoked no-expiry credential reads rejected (or inactive when the provider's introspection says so) within the re-check interval without anyone opening the consent page. A grant is due once its last verdict, or its connection when it has none, is older than the interval, so a fresh connection is left to the connect-time verification. The sweep runs on the server process, is paced per issuer host fleet-wide, records nothing for tunneled members whose tunnel is down, and reports its probes under the new `keepalive` trigger on `gram.remote_session.validation`. `--remote-session-recheck-interval` (`GRAM_REMOTE_SESSION_RECHECK_INTERVAL`) defaults to 24h; zero or negative disables the sweep.
