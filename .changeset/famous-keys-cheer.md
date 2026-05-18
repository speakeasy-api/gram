---
"server": patch
---

Invite acceptance now uses Gram invite tokens plus WorkOS User Management Magic Auth codes.
The server validates the invite token, creates and consumes the Magic Auth code for the invited email, verifies the email match, and completes provisioning.
