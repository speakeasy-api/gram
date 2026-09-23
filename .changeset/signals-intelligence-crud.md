---
"server": minor
---

Add management APIs for reusable, project-scoped custom signals and sensors. Sensors support multi-label, exclusive-choice, and ordered-score modes, including incomplete drafts and atomic ordered-membership updates. The APIs include cursor pagination, project authorization, and transactional audit events. Deleting a shared signal detaches it from every sensor while preserving the remaining order; deleting a sensor preserves its catalog signals.

This change provides configuration CRUD only; it does not execute classifiers or produce classification results.
