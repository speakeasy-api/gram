---
"server": minor
---

Adds the `onboarding` management API: `listReferenceData` (products, vendor plans, use cases and MDM vendors from the coverage reference tables), `get`, `saveAnswers`, `verifyStep` and `getUseCaseStatus`. An organization admin declares the products they use with their plans, their MDM vendor and one use case; the server recommends a single next step from hardcoded rules, verifies it against the last 30 days of evidence, and marks onboarding done once the use case is covered. The server seeds the reference tables from its Go catalog at startup.
