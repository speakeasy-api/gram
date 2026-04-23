---
"server": patch
---

Updated fly app reaping to target all apps used by old deployments, leaving only the most recent deployment's app(s) untouched. This is a more aggressive strategy that is coming ahead of support for scaling up fly apps to multiple machines per deployment.
