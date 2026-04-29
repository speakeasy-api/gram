import type { IntegrationDoc } from "./index";

export const CRON_DOCS: IntegrationDoc = {
  slug: "cron",
  title: "Cron schedule",
  summary:
    "Run an assistant on a recurring schedule (every N minutes, daily, weekly, etc.).",
  body: `# Cron schedule trigger

Fires the assistant on a recurring UTC schedule. No credentials required; the trigger reuses the assistant's shared environment.

## Setup

\`create_trigger\` with \`definition_slug: "cron"\` and \`{ "schedule": "<cron expr>" }\`. Do NOT pass \`environment_id\`.

## Cron expressions

5-field, UTC: \`minute hour day-of-month month day-of-week\`.

| Schedule | Expression |
|----------|------------|
| Every 5 minutes | \`*/5 * * * *\` |
| Hourly | \`0 * * * *\` |
| Daily 09:00 UTC | \`0 9 * * *\` |
| Weekdays 09:00 UTC | \`0 9 * * 1-5\` |
| Mondays 14:30 UTC | \`30 14 * * 1\` |
| Monthly midnight UTC | \`0 0 1 * *\` |

If the user gives a local time, ask the timezone and convert to UTC.

## Cron has no payload

The trigger only signals "now". Any work — fetching messages, summarizing, posting — must be in the assistant's instructions and toolset. For a Slack-driven summary the assistant needs both a cron trigger and Slack tools; declare \`SLACK_BOT_TOKEN\` via \`add_environment_keys\` + \`request_environment_secrets\`. See \`docs("slack")\` for the credential setup.
`,
};
