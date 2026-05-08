import type { IntegrationDoc } from "./index";

export const SLACK_DOCS: IntegrationDoc = {
  slug: "slack",
  title: "Slack",
  summary:
    "Trigger an assistant from Slack messages, mentions, and reactions; reply via the bot.",
  body: `# Slack integration

Fires the assistant from Slack events (\`app_mention\`, \`message\`, \`reaction_added\`, etc.). Requires a bot token (\`xoxb-\`) and signing secret. The user creates and installs a Slack app, then pastes the credentials into the assistant's environment — Slack does not let us OAuth on their behalf for a brand-new app.

## Environment

The assistant owns one shared environment. Extend it with \`add_environment_keys\`; populate values with \`request_environment_secrets\`. Do not create a separate env per integration.

## Recommended flow (pre-filled webhook)

1. **Attach a Slack toolset.** \`list_integrations\` (\`"slack"\`) → \`create_toolset\` if needed → \`attach_toolset\`. Required so the bot's tools are callable in step 7. After attach, default-add the reaction platform tools (\`tools:platform:slack:add_reaction\`, \`tools:platform:slack:remove_reaction\`, \`tools:platform:slack:get_reactions\`, \`tools:platform:slack:list_reactions\`, \`tools:platform:slack:list_emoji\`) via \`add_tools_to_toolset\` — skip only if the user has explicitly said they don't want reaction tooling.
2. **Declare keys.** \`add_environment_keys({ keys: ["SLACK_BOT_TOKEN", "SLACK_SIGNING_SECRET"] })\`.
3. **Create the trigger.** \`create_trigger\` with \`definition_slug: "slack"\` and the relevant \`event_types\`. The response includes a \`webhook_url\` that already responds to Slack's \`url_verification\` challenge. Remember the trigger \`id\` for step 7d.
4. **Show the app guide — only if the bot doesn't exist.** Check \`list_environments\` → \`populated_entry_names\`; skip if \`SLACK_BOT_TOKEN\` is already set. Otherwise \`show_slack_app_guide\` with the \`webhook_url\`. The component derives bot scopes and \`bot_events\` from the attached toolset and trigger; do not pass overrides unless adding a scope the catalog lacks.
5. **User installs the app.** Separate click from Create. Slack mints \`xoxb-...\` only after install + approval.
6. **Request values.** \`request_environment_secrets\` for \`SLACK_BOT_TOKEN\` (sensitive, OAuth & Permissions) and \`SLACK_SIGNING_SECRET\` (sensitive, Basic Information → App Credentials).
7. **Self-handshake.** Single short burst, no confirmation:
   - **a.** Ask for the user's Slack handle.
   - **b.** DM the user a greeting in the assistant's voice (the persona picked via \`propose_identity\`). Do not template.
   - **c.** Search your own messages for the greeting; read \`bot_id\` off the bot-authored message. Do not substitute a user id.
   - **d.** \`update_trigger(id, config)\` adding \`event.bot_id != "<bot_id>"\` to \`config.filter\` (AND with any existing filter). Required to break reply loops.

## Fallback flow (no pre-filled webhook)

Use only when the webhook URL isn't available yet.

1. \`attach_toolset\`.
2. If \`SLACK_BOT_TOKEN\` not yet populated, \`show_slack_app_guide\` without a webhook (subscriptions stay empty since Slack rejects unverified URLs).
3. User installs, copies bot token + signing secret.
4. \`add_environment_keys\` + \`request_environment_secrets\`.
5. \`create_trigger\` — returns the webhook URL.
6. \`show_webhook_url\` so the user can paste it into Event Subscriptions → Request URL and verify. User subscribes to \`bot_events\` in Slack's UI.
7. Self-handshake (steps 7a–7d above).

## Trigger config

- \`event_types\` *(string[])* — required. Common: \`app_mention\`, \`message\` (noisy; pair with \`filter\`), \`reaction_added\`. Use bare event names — dotted names (\`message.im\` etc.) are rejected.
- \`filter\` *(string, optional)* — CEL expression. Do not mention CEL to the user; describe rules in plain English. Available fields on \`event\`: \`envelope_type\`, \`event_type\`, \`subtype\`, \`team_id\`, \`channel_id\`, \`thread_id\`, \`user_id\`, \`bot_id\`, \`app_id\`, \`text\`, \`timestamp\`. AND multiple conditions with \`&&\`. Use \`event.bot_id != "<id>"\` to break reply loops (id from step 7c).

## Recommended event_types

| Use case | event_types |
|----------|-------------|
| @-mention | \`["app_mention"]\` |
| Watch channel/DM messages | \`["message"]\` (filter required) |
| Reaction-driven | \`["reaction_added"]\` |
| Scheduled summary | cron trigger; assistant fetches Slack via toolset |

## Tokens

- **Signing Secret** → \`SLACK_SIGNING_SECRET\`. HMAC-SHA256 key for verifying \`x-slack-signature\`.
- **Bot User OAuth Token** (\`xoxb-\`) → \`SLACK_BOT_TOKEN\`. Web API auth.
- Client Secret and Verification Token are not used here.

## Gotchas

- **Reply loops.** Without step 7d, every bot reply re-triggers the assistant.
- **Install ≠ Create.** \`xoxb-\` only appears after install + approval. Users frequently stop after Create.
- **Scope changes need re-install.** Ship the full intended scope list in the first manifest.
- **App name unique per workspace.** \`duplicate_app_name\` → suggest renaming.
- **Bot must be invited to channels** for non-mention events in public channels.
- **Enterprise Grid** workspaces may require admin approval to install.
- **\`xoxp-\` is wrong.** That's a user token. Bot tokens start with \`xoxb-\`.
- **\`search.all\` needs a user token.** The \`search_messages_and_files\` platform tool calls Slack's \`search.all\`, which requires a user-token \`search:read\` scope. The manifest builder skips it; if attached, also collect a user token from OAuth & Permissions → User Token Scopes → \`search:read\` and store as \`SLACK_USER_TOKEN\`.
`,
};
