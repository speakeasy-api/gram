---
"server": minor
"dashboard": patch
---

Okta connections installed from the Okta Integration Network can authenticate with a client ID and client secret. Admins can choose the installation method, submit credentials, and replace the secret from the dashboard. A pending connection can switch between the two methods until its client ID is submitted, reusing one signing key. Token acquisition respects credential revocation and preserves observed DPoP binding across worker restarts.
