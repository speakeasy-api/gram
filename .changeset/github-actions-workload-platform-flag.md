---
"server": patch
---

GitHub Actions can be offered as a workload platform to an organization before it is offered to everyone, through the `workload-platform-github-actions` feature flag. Where the flag is on, the platform catalog lists GitHub Actions as enabled, so the agent wizard, Command K and preselected setup links offer its guided setup. Everywhere else, and whenever the flag cannot be evaluated, it stays under Coming soon. A catalog platform file names such a flag with `feature_flag`, which only an entry that is not enabled may set.
