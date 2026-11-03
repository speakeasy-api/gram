---
"server": minor
"dashboard": minor
---

The GitHub Actions setup offers a fourth choice of jobs, **Pull requests**. It writes the exact rule `repo:<owner>/<repo>:pull_request`, which admits pull request jobs that use no environment and refuses branch, tag and environment jobs. The generated workflow runs only on `pull_request` (opened, synchronize, reopened and ready for review) and skips pull requests from forks, which get no GitHub token. The setup warns that anyone who can push a branch can run such a job with the workflow as their branch has it, and that a `pull_request_target` workflow gets the same subject, a fork's pull request included. Editing an agent whose rule admits pull requests opens on this choice. GitHub Actions is still offered only where the `workload-platform-github-actions` feature flag is on.
