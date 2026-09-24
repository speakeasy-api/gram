---
"dashboard": minor
---

Adds the two-stage onboarding wizard behind the `gram-new-onboarding` PostHog flag. Stage one is the organization's stack: the providers it uses with the plan it is on with each, the products of those providers, and its MDM vendor. Between the stages the admin picks the use case to reach first, on its own, and it decides what stage two shows. Stage two is that use case's steps, one at a time, each checked against real traffic until the use case is covered. Two unlabeled bars above the whole wizard fill as each stage progresses, and the rail down the left lists the screens of the active stage. A brand-new organization opens the wizard from org home; one with traffic or saved answers gets a dismissible banner. Answers stay editable at Onboarding settings, and the sidebar's setup entry points at the wizard while the flag is on.
