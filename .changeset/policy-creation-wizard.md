---
"dashboard": minor
---

Replace the policy form with a guided 4-step creation/edit wizard for both
standard and prompt-based policies: Detect (detector cards + a searchable
customize sheet; prompt policies get the guardrail prompt + advanced judge
config), Scope (scope cards with a coverage warning + inline exemptions), Action
(flag/block), and Review. Built on a shared `WizardShell` over the onboarding
stepper (extended with badges + free-jump navigation).
