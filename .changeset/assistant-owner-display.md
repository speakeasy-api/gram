---
"server": patch
"dashboard": patch
---

Show the assistant's creator as its owner. Assistants already recorded who created them; that attribution is now surfaced as a profile avatar (reusing the org-home member avatar treatment) on both the assistant card and the assistant setup page's overview panel. The owner resolves to one of three states: the creating member (avatar + name, full name on hover), "No owner" when the assistant was never attributed, or "Orphaned, no owner" when the creator is no longer a member of the organization. Backed by a new optional `created_by_user_id` field on the `Assistant` API type.
