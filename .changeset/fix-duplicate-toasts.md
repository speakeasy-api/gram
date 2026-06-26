---
"dashboard": patch
---

Fix toast notifications rendering twice. A second `<Toaster>` was mounted at the app root in addition to the one inside the provider tree, so every toast appeared (and dismissed) as a duplicate. Removed the redundant root-level Toaster.
