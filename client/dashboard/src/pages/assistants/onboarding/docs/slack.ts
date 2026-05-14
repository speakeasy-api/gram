import type { IntegrationDoc } from "./index";

export const SLACK_DOCS: IntegrationDoc = {
  slug: "slack",
  title: "Slack",
  summary:
    "Wire an assistant to a Slack workspace end-to-end: respond to events, talk to users, and react to schedules.",
  body: `# Slack integration

Wire the assistant end-to-end with the user's Slack workspace: it reads/sends messages via the bot AND can be triggered by Slack events. The user installs a Slack connection in their workspace, then pastes credentials into the assistant's environment — Slack does not let us OAuth on their behalf for a brand-new integration. The user-facing flow is split into TWO cards: an install card (\`show_slack_app_guide\` — handles workspace pick, install, and Event Subscriptions Retry) followed by a tokens card (\`request_environment_secrets\`). Never call these two tools in the same turn — emit \`show_slack_app_guide\` alone, wait for it to resolve, then emit \`request_environment_secrets\` in a later turn.

## Default rule: always create a slack trigger

If the user's request involves Slack at all, create a slack trigger as part of setup — even if their stated goal is one-directional ("summarize my DMs at 8am", "post a daily standup"). A bot the user can talk to is strictly more useful than one they can't: once it has DM'd them, they'll want to reply, ask follow-ups, or correct it. Skip the slack trigger only when the user has explicitly said they do not want the assistant to react to Slack events.

The slack trigger is additive — pair it with any cron/other trigger the user asked for. A "morning digest" assistant gets BOTH a cron trigger (fires the daily summary) AND a slack trigger (lets the user reply / chat).

For event_types on the default slack trigger: \`["app_mention", "message"]\` covers the common cases (mentions in channels, DMs to the bot). Add a \`filter\` if \`message\` produces too much noise.

## Environment

The assistant owns one shared environment. Extend it with \`add_environment_keys\`; populate values with \`request_environment_secrets\`. Do not create a separate env per integration.

Required keys for the default flow:
- \`SLACK_BOT_TOKEN\` (xoxb-) — bot Web API auth.
- \`SLACK_USER_TOKEN\` (xoxp-) — user-token Web API auth. The manifest always pre-fills the full user-scope superset so Slack issues xoxp- in the same install as xoxb-.
- \`SLACK_SIGNING_SECRET\` — verifies inbound webhooks from the slack trigger.

## Recommended flow (slack trigger + any extras)

1. **Attach a Slack toolset.** \`list_integrations\` (\`"slack"\`) → \`create_toolset\` if needed → \`attach_toolset\`. After attach, default-add the reaction platform tools (\`tools:platform:slack:add_reaction\`, \`tools:platform:slack:remove_reaction\`, \`tools:platform:slack:get_reactions\`, \`tools:platform:slack:list_reactions\`, \`tools:platform:slack:list_emoji\`) via \`add_tools_to_toolset\` — skip only if the user has explicitly said they don't want reaction tooling.
2. **Declare keys.** \`add_environment_keys({ keys: ["SLACK_BOT_TOKEN", "SLACK_USER_TOKEN", "SLACK_SIGNING_SECRET"] })\`.
3. **Create the slack trigger.** \`create_trigger\` with \`definition_slug: "slack"\` and the relevant \`event_types\` (default \`["app_mention", "message"]\`). The response includes a \`webhook_url\` that already responds to Slack's \`url_verification\` challenge. Remember the trigger \`id\` for step 8d.
4. **Create any additional triggers the user asked for** (e.g. \`cron\` for a scheduled digest). These are additive.
5. **Show the install card.** Skip if \`SLACK_BOT_TOKEN\` is already populated (check \`list_environments\` → \`populated_entry_names\`). Otherwise \`show_slack_app_guide\` with the slack trigger's \`webhook_url\`. The manifest always grants the full bot- and user-scope supersets; do not pass scope overrides. The card walks the user through install + Event Subscriptions Retry — it does NOT collect any tokens. **This tool BLOCKS** until the user clicks "I'm done" — do not call other tools in parallel with it.
6. **User installs the connection.** Slack mints both \`xoxb-\` (bot) and \`xoxp-\` (user) tokens at install + approval.
7. **Show the tokens card.** Once the install card resolves with \`installed: true\`, call \`request_environment_secrets\` for \`SLACK_BOT_TOKEN\` (OAuth & Permissions → Bot User OAuth Token), \`SLACK_USER_TOKEN\` (OAuth & Permissions → User OAuth Token), and \`SLACK_SIGNING_SECRET\` (Basic Information → App Credentials). This is a separate card, not part of the install card. If the install card resolved with \`cancelled: true\`, do not call \`request_environment_secrets\` — they have nothing to paste.
8. **Self-handshake.** Single short burst, no confirmation:
   - **a.** Ask for the user's Slack handle.
   - **b.** DM the user a greeting in the assistant's voice (the persona picked via \`propose_personality\`). Do not template.
   - **c.** Search your own messages for the greeting; read \`bot_id\` off the bot-authored message. Do not substitute a user id.
   - **d.** \`update_trigger(id, config)\` adding \`event.bot_id != "<bot_id>"\` to \`config.filter\` (AND with any existing filter). Required to break reply loops.

## Opt-out: no slack trigger

Only when the user has explicitly said the assistant should NOT react to Slack events. The bot still sends messages via API but cannot receive any.

Same as the recommended flow with these differences:
- Skip step 3 (no slack trigger).
- In step 2, do NOT add \`SLACK_SIGNING_SECRET\` — there is no inbound webhook to verify. Still declare \`SLACK_BOT_TOKEN\` and \`SLACK_USER_TOKEN\`.
- In step 5, call \`show_slack_app_guide\` without \`webhook_url\`. The manifest is scope-only (no \`event_subscriptions\`); both xoxb- and xoxp- are still issued.
- In step 7, omit \`SLACK_SIGNING_SECRET\` from the secrets request.
- Skip step 8 (self-handshake) — without a trigger, the bot will never see the reply anyway.

## Webhook URL not yet available

If for some reason the webhook URL isn't available at step 5 (e.g. trigger creation deferred): call \`show_slack_app_guide\` without \`webhook_url\`. The card drops the Event Subscriptions Retry step automatically. The manifest is scope-only and Slack accepts it. After install, create the slack trigger and use \`show_webhook_url\` so the user pastes the URL into Event Subscriptions → Request URL manually and subscribes to \`bot_events\` in Slack's UI.

## Trigger config

- \`event_types\` *(string[])* — required. Common: \`app_mention\`, \`message\` (noisy; pair with \`filter\`), \`reaction_added\`. Use bare event names — dotted names (\`message.im\` etc.) are rejected.
- \`filter\` *(string, optional)* — CEL expression. Do not mention CEL to the user; describe rules in plain English. Available fields on \`event\`: \`envelope_type\`, \`event_type\`, \`subtype\`, \`team_id\`, \`channel_id\`, \`thread_id\`, \`user_id\`, \`bot_id\`, \`app_id\`, \`text\`, \`timestamp\`. AND multiple conditions with \`&&\`. Use \`event.bot_id != "<id>"\` to break reply loops (id from step 8c).

## Recommended event_types

| Use case | event_types |
|----------|-------------|
| Default (responsive bot) | \`["app_mention", "message"]\` |
| @-mention only | \`["app_mention"]\` |
| Reaction-driven | \`["reaction_added"]\` |
| Scheduled-only + replies | \`["app_mention", "message"]\` + cron trigger |

## Tokens

- **Bot User OAuth Token** (\`xoxb-\`) → \`SLACK_BOT_TOKEN\`. Bot-token Web API auth (acts as the bot).
- **User OAuth Token** (\`xoxp-\`) → \`SLACK_USER_TOKEN\`. User-token Web API auth (acts as the installing user — required for reading the user's own DMs, groups, etc.).
- **Signing Secret** → \`SLACK_SIGNING_SECRET\`. HMAC-SHA256 key for verifying \`x-slack-signature\` on the slack trigger's webhook.
- Client Secret and Verification Token are not used here.

## Gotchas

- **Reply loops.** Without step 8d, every bot reply re-triggers the assistant.
- **Install ≠ Create.** \`xoxb-\` only appears after install + approval. Users frequently stop after Create.
- **Scope changes need re-install.** Ship the full intended scope list in the first manifest — including user scopes.
- **App name unique per workspace.** \`duplicate_app_name\` → suggest renaming.
- **Bot must be invited to channels** for non-mention events in public channels.
- **Enterprise Grid** workspaces may require admin approval to install.
- **\`xoxp-\` is for user tokens, \`xoxb-\` for bot.** Don't confuse them in the secrets form.
`,
};
