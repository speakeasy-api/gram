---
"dashboard": patch
---

Hide Microsoft Intune from the MDM integrations UI until it is fully supported — a central `isProviderVisible` filter removes hidden providers from the list, the pipeline source count, the fleet-source breakdown, and direct-URL access, while leaving backend registration untouched. Also pluralize the pipeline agent-input label to "Active agents" and label its source as "reported by device agent".
