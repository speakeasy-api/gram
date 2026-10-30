---
"dashboard": patch
---

Editing an agent's platform sign-in now saves a changed workload, such as a new namespace or service account for a GKE cluster. The setup step shows Save changes, which adds the new access rule for the agent and then removes the old one. Previously the fields could be edited and the setup instructions showed the new values, but nothing was saved, so the platform's tokens were still refused.
