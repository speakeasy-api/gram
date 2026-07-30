---
"server": major
---

feat: evidence records carry per-device attestation strength

Pushed Drata/Vanta coverage records replace assignedUserAgentActive /
assignedUserAgentLastSeenAt with agentActive, agentAttestation, and
agentLastSeenAt. agentAttestation is "device" when the record is backed by
that machine's own agent heartbeat (matched on hardware serial) and "user"
when only its assigned user's, so a single push can carry both strengths
truthfully. Breaking for the customer-declared Drata/Vanta record schemas.
