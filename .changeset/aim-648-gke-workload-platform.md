---
"server": minor
"dashboard": minor
---

GKE is now a workload identity option when creating an agent. The setup trusts one cluster by its project, location and name, admits one namespace and service account on it, and gives the pod manifest and token exchange the workload needs, with no secret stored in the cluster.
