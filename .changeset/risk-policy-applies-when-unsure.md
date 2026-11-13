---
"server": patch
---

A risk policy now applies when the caller's permissions cannot be read or the policy cannot be evaluated. Before, such a failure left the request unchecked. A policy is skipped only when the caller is outside its audience or holds an exemption.
