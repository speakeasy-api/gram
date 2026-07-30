---
"server": minor
---

Add the Vanta evidence-sink provider to device integrations: OAuth
client-credentials auth with a per-run token cache (Vanta allows one active
token per application), and per-device agent-coverage evidence pushed as a
full-state Custom Resource sync whose property names scope the attestation
to the assigned user. Rejected records fail the push loudly, since
full-state semantics would otherwise read them as departed devices.
