import type { IntegrationDoc } from "./index";

export const CRON_DOCS: IntegrationDoc = {
  slug: "cron",
  title: "Cron schedule",
  summary:
    "Run an assistant on a recurring schedule (every N minutes, daily, weekly, etc.).",
  body: `# Cron schedule trigger

Cron triggers fire the assistant on a recurring schedule. Useful for daily summaries, periodic syncs, or anything driven by a clock rather than an external event.

## Onboarding flow

1. Cron itself needs no credentials — only the schedule. The assistant already owns a shared environment; cron triggers attach to it automatically and simply don't read from it.
2. Call \`create_trigger\` with \`definition_slug: "cron"\` and a config like \`{ "schedule": "0 9 * * *" }\` (every day at 09:00 UTC). Do NOT pass \`environment_id\` — the assistant's env is used by default.

## Cron expression cheatsheet

Standard 5-field cron: \`minute hour day-of-month month day-of-week\`. All times are UTC.

| Schedule | Expression |
|----------|------------|
| Every 5 minutes | \`*/5 * * * *\` |
| Every hour on the hour | \`0 * * * *\` |
| Every day at 09:00 UTC | \`0 9 * * *\` |
| Weekdays at 09:00 UTC | \`0 9 * * 1-5\` |
| Every Monday at 14:30 UTC | \`30 14 * * 1\` |
| First of every month at midnight UTC | \`0 0 1 * *\` |

If the user gives a local time, ask which timezone and convert to UTC before writing the expression.

## Pattern: morning summary that DMs the user

Cron triggers don't carry a payload — they just say "the time is now". The assistant's instructions need to do the work:

1. Cron fires the assistant.
2. Assistant calls Slack toolset tools to read recent messages.
3. Assistant summarizes.
4. Assistant calls Slack toolset tool to send a DM to the user.

This means the assistant needs **both** a cron trigger (to fire it) **and** a toolset that includes Slack tools (to read and DM). The Slack tools read \`SLACK_BOT_TOKEN\` from the assistant's shared env — declare it with \`add_environment_keys\` and collect the value with \`request_environment_secrets\`. See \`docs("slack")\` for the credential setup, even though no Slack trigger is involved.
`,
};
