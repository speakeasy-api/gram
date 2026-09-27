---
"server": minor
"dashboard": minor
---

The setup board now has a single "Set up identity provider" task with three steps: verify a domain, connect single sign-on, and sync the directory. The separate "Verify your domain" task, and the legacy "Connect identity provider" and "Set up directory sync" tasks, are gone, so nothing on the board is blocked behind another identity task. The security onboarding preset and the demo organization show the combined task. The single sign-on step stays disabled until a domain is verified, and a blocked portal popup now shows an error on every step.
