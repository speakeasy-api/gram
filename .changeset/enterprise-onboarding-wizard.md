---
"server": minor
"dashboard": minor
---

Add the enterprise onboarding wizard at `/<org>/setup` that walks new orgs through five steps end-to-end: SSO setup via WorkOS, directory sync, publishing a private plugin marketplace to GitHub, instrumenting each agent platform (Claude Code, Claude Cowork, OpenAI Codex, Cursor) with the org's marketplace and observability plugin, and confirming traffic is flowing.

Includes:

- New `Create Plugin Marketplace` step that wraps the same GitHub publish flow as the Plugins page, with a typeahead-driven GitHub-user picker for collaborator access (replaces the old comma-separated input).
- `Instrument Agents` step that surfaces per-platform setup instructions with auto-generated API keys, marketplace URL / repo URL / plugin slug substitution, eligibility gating (Claude Teams/Enterprise check), and platform-specific screenshots. Coming-soon entries for GitHub Copilot, Gemini, Glean, and AWS Bedrock are rendered as a half-width muted grid and excluded from the configured/total count.
- Wizard resume logic backed by `organizations.getOnboardingStatus` and `plugins.getPublishStatus` — reloading lands on the deepest known-incomplete step instead of step 0.
- `organizations.sendEnterpriseAdminOnboardingEmail` endpoint and a super-admin "Onboarding" tab for dispatching the enterprise-admin invite email (Loops template `cmpqyxnzl00hj0jwtkibhyjdz`), which deep-links recipients into the active org's wizard.
- `organizations.verifyOnboardingHooksSetup` polling endpoint that surfaces recent hook events from ClickHouse for the `Confirm Traffic` step.
- Wizard chrome: header with Docs / Get Support (Pylon) / Go to Dashboard buttons, footer with the moonshine ThemeSwitcher, and a project-slug query-string override on the SDK provider so the wizard can hit project-scoped endpoints from org-level routes (falls back to the `default` project when unset).
