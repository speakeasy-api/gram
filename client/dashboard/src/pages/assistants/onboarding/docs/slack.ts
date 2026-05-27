import type { IntegrationDoc } from "./index";

export const SLACK_DOCS: IntegrationDoc = {
  slug: "slack",
  title: "Slack",
  summary:
    "Wire an assistant to a Slack workspace end-to-end: respond to events, talk to users, and react to schedules.",
  body: `# Slack integration

Wire the assistant end-to-end with the user's Slack workspace: it reads/sends messages via the bot AND can be triggered by Slack events. The user installs a Slack connection in their workspace, then pastes credentials into the assistant's environment — Slack does not let us OAuth on their behalf for a brand-new integration.

## Default rule: per-assistant Slack toolset, never catalog reuse

Always create a NEW Slack toolset scoped to this assistant. Do not look in \`list_integrations\` or \`list_toolsets\` for an existing Slack toolset to reuse — even if one exists, it likely grants more (or different) capabilities than this assistant needs, and an assistant that shares a Slack toolset with others inherits any capability changes the user makes elsewhere. \`propose_slack_setup\` handles this — it creates a fresh per-assistant toolset every time and attaches only the capability groups the user picks.

## Default rule: always offer a slack trigger

If the user's request involves Slack at all, offer event subscriptions (the "What wakes it up?" section in \`propose_slack_setup\`) — even if their stated goal is one-directional ("summarize my DMs at 8am", "post a daily standup"). A bot the user can talk to is strictly more useful than one they can't: once it has DM'd them, they'll want to reply, ask follow-ups, or correct it. Skip the slack trigger only when the user has explicitly said they do not want the assistant to react to Slack events.

The slack trigger is additive — pair it with any cron/other trigger the user asked for. A "morning digest" assistant gets BOTH a cron trigger (fires the daily summary) AND a slack trigger (lets the user reply / chat).

For the default preselected events when offering \`propose_slack_setup\`: \`mentions\` covers the common @-mention case without noise. Add \`messages\` only when the user explicitly wants the bot to react to free-form chatter — and warn them via the card's always-on note that this fires constantly without a filter.

## Environment

The assistant owns one shared environment. Extend it with \`add_environment_keys\`; populate values with \`request_environment_secrets\`. Do not create a separate env per integration.

Required keys (listed in the order the user encounters them in Slack's UI — Signing Secret on Basic Information, then bot/user tokens on the Install App / OAuth & Permissions page):
- \`SLACK_SIGNING_SECRET\` — verifies inbound webhooks from the slack trigger. Required only when \`propose_slack_setup\` created a slack trigger.
- \`SLACK_BOT_TOKEN\` (xoxb-) — bot Web API auth. Required whenever any Slack capability group is selected.
- \`SLACK_USER_TOKEN\` (xoxp-) — user-token Web API auth. The manifest always pre-fills the full user-scope superset so Slack issues xoxp- in the same install as xoxb-.

## Recommended flow

1. **Propose the setup.** \`propose_slack_setup\` with preselected groups derived from the user's stated goal. Examples (shape, not content): an @-mention responder typically gets \`send\` + \`mentions\`; a research-and-post bot gets \`send\` + \`read\` + \`mentions\`; a reaction-driven workflow gets \`react\` + \`reactions\`. The card produces a per-assistant Slack toolset (already attached) and, if any events were chosen, a slack trigger with its \`webhook_url\`. **This tool BLOCKS** until the user submits or skips — never call other tools in parallel with it.
2. **Declare keys.** Once \`propose_slack_setup\` resolves, \`add_environment_keys\` with the keys this combination needs (Slack-UI order so the later \`request_environment_secrets\` card matches what the user sees).
3. **Show the install card.** Skip if \`SLACK_BOT_TOKEN\` is already populated (check \`list_environments\` → \`populated_entry_names\`). Otherwise \`show_slack_app_guide\`. Pass the slack trigger's \`webhook_url\` (from step 1's result) if a trigger was created. The manifest always grants the full bot- and user-scope supersets; do not pass scope overrides. The card walks the user through install + Event Subscriptions Retry. **This tool BLOCKS** until the user clicks "I'm done" — do not call other tools in parallel with it.
4. **User installs the connection.** Slack mints both \`xoxb-\` (bot) and \`xoxp-\` (user) tokens at install + approval.
5. **Show the tokens card.** Once the install card resolves with \`installed: true\`, call \`request_environment_secrets\` in the order the user will read them in Slack's UI: \`SLACK_SIGNING_SECRET\` (only if a trigger exists; Basic Information → App Credentials), then \`SLACK_BOT_TOKEN\` (OAuth & Permissions → Bot User OAuth Token), then \`SLACK_USER_TOKEN\` (OAuth & Permissions → User OAuth Token). If the install card resolved with \`cancelled: true\`, do not call \`request_environment_secrets\` — they have nothing to paste.
6. **Self-handshake** (only when a slack trigger was created). Single short burst, no confirmation:
   - **a.** Ask for the user's Slack handle.
   - **b.** DM the user a greeting in the assistant's voice (the persona picked via \`propose_personality\`). Do not template.
   - **c.** Search your own messages for the greeting; read \`bot_id\` off the bot-authored message. Do not substitute a user id.
   - **d.** \`update_trigger(id, config)\` adding \`event.bot_id != "<bot_id>"\` to \`config.filter\` (AND with any existing filter). Required to break reply loops.
7. **Offer to narrow the filter** (only when a slack trigger was created). After the self-handshake, prompt the user in plain English to scope the trigger further — see "Filter narrowing" below. Skip only if the user has already given a tight filter or has explicitly said the firehose is fine.

## Filter narrowing — plain English to CEL

Triggers are "always on": every matching Slack event in any channel the bot is in wakes the assistant. Most users want narrower behavior. After the self-handshake, offer to dial it back. Ask the user one short, plain-English question — never mention CEL, event field names, or scope/event jargon. Examples of what to ask:

- "Want to limit this to a few specific channels?" — if yes, ask for channel names, then add \`event.channel_id in ["C..."]\` (look up channel IDs via the Slack search/list tools the assistant just gained).
- "Want it to only respond when you (or specific people) mention it?" — if yes, gather Slack handles, add \`event.user_id in ["U..."]\` (look up via \`platform_slack_lookup_user_by_email\` or \`search_users\` if those tools are attached).
- "Should it stay out of DMs?" or "Only listen in DMs?" — \`event.envelope_type == "channel"\` or \`event.envelope_type == "im"\`.
- "Should it only react in threads?" — \`event.thread_id != ""\`.

AND multiple conditions with \`&&\` and merge with any existing \`config.filter\` (e.g. the bot-loop guard from step 6d) — never replace it. Apply via \`update_trigger(id, { filter: "<expr>" })\`.

If the user declines narrowing, acknowledge briefly and continue — they can ask later. Don't relitigate.

## Opt-out: no slack trigger

Only when the user has explicitly said the assistant should NOT react to Slack events. The bot still sends messages via API but cannot receive any.

Same as the recommended flow with these differences:
- In step 1, run \`propose_slack_setup\` with no preselected events (or zero events on submit). No slack trigger is created.
- In step 2, do NOT add \`SLACK_SIGNING_SECRET\` — there is no inbound webhook to verify. Still declare \`SLACK_BOT_TOKEN\` and \`SLACK_USER_TOKEN\`.
- In step 3, call \`show_slack_app_guide\` without \`webhook_url\`. The manifest is scope-only (no \`event_subscriptions\`); both xoxb- and xoxp- are still issued.
- In step 5, omit \`SLACK_SIGNING_SECRET\` from the secrets request.
- Skip steps 6 and 7 — without a trigger, the bot will never see the reply anyway.

## Webhook URL not yet available

If for some reason the webhook URL isn't returned at step 1 (e.g. trigger creation failed): call \`show_slack_app_guide\` without \`webhook_url\`. The card drops the Event Subscriptions Retry step automatically. The manifest is scope-only and Slack accepts it. After install, retry trigger creation by re-running \`propose_slack_setup\` (with capabilities now empty so only the trigger gets created), then \`show_webhook_url\` so the user pastes the URL into Event Subscriptions → Request URL manually.

## Trigger config

- \`event_types\` *(string[])* — required. Common: \`app_mention\`, \`message\` (noisy; pair with \`filter\`), \`reaction_added\`. Use bare event names — dotted names (\`message.im\` etc.) are rejected.
- \`filter\` *(string, optional)* — CEL expression. Do not mention CEL to the user; describe rules in plain English. Available fields on \`event\`: \`envelope_type\`, \`event_type\`, \`subtype\`, \`team_id\`, \`channel_id\`, \`thread_id\`, \`user_id\`, \`bot_id\`, \`app_id\`, \`text\`, \`timestamp\`. AND multiple conditions with \`&&\`. Use \`event.bot_id != "<id>"\` to break reply loops (id from step 6c).

## Tokens

- **Signing Secret** → \`SLACK_SIGNING_SECRET\`. HMAC-SHA256 key for verifying \`x-slack-signature\` on the slack trigger's webhook. Found on Basic Information → App Credentials.
- **Bot User OAuth Token** (\`xoxb-\`) → \`SLACK_BOT_TOKEN\`. Bot-token Web API auth (acts as the bot). Found on OAuth & Permissions after install.
- **User OAuth Token** (\`xoxp-\`) → \`SLACK_USER_TOKEN\`. User-token Web API auth (acts as the installing user — required for reading the user's own DMs, groups, etc.). Same page as the bot token.
- Client Secret and Verification Token are not used here.

## Gotchas

- **Reply loops.** Without step 6d, every bot reply re-triggers the assistant.
- **Always-on triggers.** A trigger with no filter wakes the assistant for every matching event in every channel it can see. Offer the filter-narrowing pass in step 7.
- **Install ≠ Create.** \`xoxb-\` only appears after install + approval. Users frequently stop after Create.
- **Scope changes need re-install.** Ship the full intended scope list in the first manifest — including user scopes.
- **App name unique per workspace.** \`duplicate_app_name\` → suggest renaming.
- **Bot must be invited to channels** for non-mention events in public channels.
- **Enterprise Grid** workspaces may require admin approval to install.
- **\`xoxp-\` is for user tokens, \`xoxb-\` for bot.** Don't confuse them in the secrets form.
`,
};
