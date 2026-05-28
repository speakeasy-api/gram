---
"server": patch
---

Updated the create portal session endpoint for svix webhooks to request all capabilities for admins explicitly. Previously it was specifying an empty slice of capabilities, which appeared to result in a read only session.
