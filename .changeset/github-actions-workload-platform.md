---
"server": minor
"dashboard": minor
---

GitHub Actions is now a workload platform with a guided setup, listed under **Coming soon** until a stable speakeasyd release supports it. Once it is offered, an agent's credential can be **GitHub Actions**: you name one repository on github.com, and its jobs sign in as the agent with their own GitHub token, through the Speakeasy device agent action. You choose which jobs: the whole repository (every branch, tag, environment and pull request job), one branch, or one environment, whose protection rules GitHub applies before the job starts. The setup trusts GitHub for the whole organization and gives the agent `org:device_agent_sync` and `org:hooks_ingest` for the current project, so the job can fetch the agent's plugins, call its MCP servers and send hook events as that one agent. It warns who can run such a job, lists the plugins the job will receive, and prints the workflow with your organization ID and project filled in, running only where the rule admits its jobs. Repositories that use GitHub's immutable subjects are supported by adding `@ID` to the owner and repository names.

The platform catalog can describe alternative access rules beside a platform's default; GitHub Actions is the first to use them. A catalog platform that is listed but not offered yet is named under Coming soon rather than shown as an unavailable setup, and an agent whose rule uses such a platform's issuer is edited as a custom issuer.

Catalog platform setups no longer treat a project's own issuer for the same URL as the organization trusting the platform, and they warn about every project whose own issuer would take precedence over the organization-wide rule for that project's MCP servers, naming each one and any project they could not check.

The workflow step offers Claude Code and Codex as tabs, each with its product logo, in the same tabbed panel as the agent's Connect step. It generates the selected tool's install steps, official GitHub Action, and API-key secret instructions. Both connect to Speakeasy before running the agent; Codex shares the configuration directory populated by the device agent.

The generated workflow pins every action it uses to a commit, and the Claude Code and Codex CLIs to a version. The device agent action installs the latest stable speakeasyd, and its `version` input pins one. The plugins step says which servers a job skips: gateways, which do not accept agents yet, and servers with no Speakeasy authorization server, which a job cannot get a token for. A deployment that does not serve Speakeasy over https cannot set GitHub Actions up, because the action refuses a plain-http control plane. Command K offers "Set up" for each platform with a guided setup, to people who can manage workload identities.
