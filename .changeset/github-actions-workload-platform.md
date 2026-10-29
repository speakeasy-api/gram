---
"server": minor
"dashboard": minor
---

GitHub Actions is now a workload platform with a guided setup. An agent's credential can be **GitHub Actions**: you name one repository on github.com, and every job in it signs in as the agent with its own GitHub token, through the Speakeasy device agent action. The setup trusts GitHub for the whole organization, admits every branch, tag, environment and pull request job of that repository, and gives the agent `org:device_agent_sync` and `org:hooks_ingest` for the current project, so the job can fetch the agent's plugins, call its MCP servers and send hook events as that one agent. It warns that anyone who can push a branch to the repository can run such a job, lists the plugins the job will receive, and prints the workflow with your organization ID and project filled in. Repositories that use GitHub's immutable subjects are supported by adding `@ID` to the owner and repository names.

Catalog platform setups no longer treat a project's own issuer for the same URL as the organization trusting the platform, and they warn when a project's issuer would take precedence over the organization-wide rule for that project's MCP servers.
