---
"server": patch
---

Add support for signing with GCP Cloud KMS keys, so a signing key's private half never leaves the key management service holding it. Groundwork only: no API or dashboard surface uses it yet.
