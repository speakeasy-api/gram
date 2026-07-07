---
"dashboard": patch
---

fix: restore Non-Corporate Accounts risk-policy card and remove duplicate Off-Policy Content card

The risk-policy eval refactor dropped the Non-Corporate Accounts detector (including its approved-email-domains config) from the new policy detail form and rendered Off-Policy Content twice. This re-adds `account_identity` to the available/display/flag-only category sets and payload mapping, restores the approved-domains state and Customize-sheet UI, and de-duplicates Off-Policy Content.
