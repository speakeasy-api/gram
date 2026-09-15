---
"dashboard": minor
---

Drive the connect-Okta exchange in the identity provider setup card. With nothing connected the step asks for the Okta organization URL, which is what lets every later instruction deep-link into the administrator's own console. Once a connection exists the step renders from what the server says it is rather than from hardcoded Okta strings: the instructions, the values Speakeasy prints as copyable rows, a deep link when the tenant supports one, and one field per value expected back, with per-field outcomes under their field. Verification reports each capability Okta granted with the read that proved it, and says plainly when the check cannot run at all rather than reading as a failure. Once the connection is active the sub-step shows the tenant, the capabilities and the key Okta fetches, and ticks off in the rail. The provider choice stays reversible until a connection exists, after which the escape hatch is removing it behind an inline confirm.
  