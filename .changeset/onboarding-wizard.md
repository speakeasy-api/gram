---
"dashboard": minor
---

Adds the question-driven onboarding wizard behind the `gram-new-onboarding` PostHog flag. An organization admin picks the products they use with their vendor plans, their MDM vendor and one use case, then gets a single next step that the dashboard checks against real traffic until the use case is covered. A brand-new organization opens the wizard from org home; one with traffic or saved answers gets a dismissible banner. Answers stay editable at Onboarding settings, and the sidebar's setup entry points at the wizard while the flag is on.
