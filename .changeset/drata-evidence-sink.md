---
"server": minor
---

Add the Drata evidence-sink provider to device integrations: pushes
per-device agent-coverage evidence into a customer's Drata workspace through
the Custom Connections API, using batched session uploads whose completion
atomically replaces the previous evidence set. Field names scope the
attestation to the assigned user (never "device monitored").
