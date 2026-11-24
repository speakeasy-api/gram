---
"server": patch
---

Saved Tailscale OAuth clients for private networks can move from the platform key to each organization's own encryption key. Until an organization is moved, nothing changes. Once it is, an organization key that cannot be used makes the private network retry and report `encryption_key_unavailable` instead of reporting invalid credentials.
