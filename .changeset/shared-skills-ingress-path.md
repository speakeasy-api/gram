---
"server": patch
---

Custom domain ingresses now route /shared/skills to the Gram server, so public skill share pages resolve on custom domains instead of returning an edge 404. Existing domains pick up the new route on their next ingress re-apply (e.g. saving domain settings).
