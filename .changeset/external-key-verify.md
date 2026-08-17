---
"server": minor
---

Add `externalKeys.verifyGcpKms`, which proves end to end that Gram can reach an
organization's GCP KMS key and use it to sign: it reads the key's public half,
confirms the algorithm matches the one recorded, signs a probe digest, and
verifies that signature locally. Nothing is persisted. The result reports a
machine-readable outcome alongside human-readable detail, so a missing
`roles/cloudkms.signerVerifier` grant, a DISABLED key version, an algorithm
mismatch, and a transient failure worth retrying are all distinguishable rather
than one opaque failure. It performs a real signing operation billed to the
key's owner, so it requires `org:admin` and is rate limited per organization.

Deleting an external credential is now refused while a live external key still
references it. Previously the delete succeeded and silently left every key
behind that credential unusable, with the breakage surfacing only at signing
time.
