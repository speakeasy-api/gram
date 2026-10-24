---
"server": patch
---

Include authenticated attachment upload defaults in AHP configuration discovery. Advertise the organization's trusted platform upload URL, a ten-second timeout, and a 1 MiB limit without exposing credentials. Discovery clients must authorize the upload destination before forwarding their configured transport headers.
