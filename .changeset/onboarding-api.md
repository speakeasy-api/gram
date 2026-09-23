---
"server": minor
---

Adds the `onboarding` management API: `listReferenceData` (providers with their plans and products, use cases and MDM vendors from the coverage reference tables), `get`, `saveStack`, `selectUseCase`, `verifyStep` and `getUseCaseStatus`. An organization admin declares the providers they use with the plan each is on, the products of those providers and their MDM vendor, then picks one use case; the server recommends a single next step from hardcoded rules, verifies it against the last 30 days of evidence, and marks onboarding done once the use case is covered. The server seeds the reference tables from its Go catalog at startup.
