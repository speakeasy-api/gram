---
"server": minor
"dashboard": minor
---

Encrypt platform OpenRouter API keys at rest. New keys are written with an
AES-256-GCM encrypted copy alongside the plaintext column (dual-write during
the expand phase), every read path prefers the encrypted copy and lazily
records ciphertext for legacy plaintext rows, and the credits monitoring
activity decrypts inside the activity boundary. A new platform-admin
`adminOpenRouterKeys` service and dashboard page list every organization's
keys with their credit limit, live usage, and encryption state, with actions
to encrypt (verify the ciphertext, then clear the plaintext), enable, and
disable a key. Enable and disable actions are audit logged against the owning
organization; the encrypt action is internal storage hygiene and is not
surfaced in customer-visible audit logs.
