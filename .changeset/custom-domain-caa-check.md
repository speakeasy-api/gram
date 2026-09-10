---
"server": patch
"dashboard": patch
---

Custom domain setup now checks CAA records so a domain that only authorizes another CA (for example Google Trust Services) is caught before Let's Encrypt issuance fails. Verification waits until `0 issue "letsencrypt.org"` is present, health checks surface `caa_forbidden`, and the setup wizard documents the record.
