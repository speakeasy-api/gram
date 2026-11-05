---
"dashboard": patch
---

The GitHub Actions setup's workflow now uses the device agent action's `v0.1` tag, which moves to each 0.1.x release, instead of a pre-release commit, so a job picks up new releases of the action without its workflow being edited. The setup notes that a team that requires commit pins can pin a release commit instead. The workflow still sets no speakeasyd `version`, so the action installs the latest stable speakeasyd.
