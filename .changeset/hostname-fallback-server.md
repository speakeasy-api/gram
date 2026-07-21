---
"server": patch
"dashboard": patch
---

Fall back to the device hostname on the user cost breakdown when a session carries no email. The Go hooks report the machine's hostname on every event; it now rides the session cache onto Claude OTEL cost rows, and the `email` telemetry dimension groups identity-less spend per device instead of pooling it all into one bucket. Only sessions with neither email nor hostname remain under "Team-wide API Usage".
