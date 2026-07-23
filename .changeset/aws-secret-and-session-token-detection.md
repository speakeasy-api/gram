---
"server": minor
---

Secret scanning now detects AWS secret access keys and session tokens, not just the access key id. The gitleaks default ruleset is extended with a native composite rule that reports a bare secret access key only when it is co-located with an AWS access key id (with an entropy floor to reject hash-shaped false positives) and a contextual rule for the session token. Redaction is updated so the access key id — an identifier, not a secret — is shown unmasked while the secret access key and session token are masked.
