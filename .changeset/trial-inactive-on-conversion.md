---
"server": patch
---

Stop leftover trial reminder emails after a customer converts or a trial
expires. `trialActive` is now cleared in Loops on Polar conversion, an admin
account-type change, and demotion, so the 7-day and 1-day sequences no longer
keep sending to paying or expired orgs.
