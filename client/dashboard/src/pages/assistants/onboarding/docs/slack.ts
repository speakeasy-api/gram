import type { IntegrationDoc } from "./index";

export const SLACK_DOCS: IntegrationDoc = {
  slug: "slack",
  title: "Slack",
  summary:
    "Trigger an assistant from Slack messages, mentions, and reactions; reply via the bot.",
  body: `# Slack integration

Slack triggers fire the assistant when the workspace emits events it subscribes to (\`app_mention\`, \`message\`, \`reaction_added\`, etc.). The bot must be installed in the workspace, and the trigger needs the **bot token** (\`xoxb-\`) and **signing secret** to respond to and verify events.

Slack doesn't let us do OAuth on the user's behalf for a brand-new app. The user creates a Slack app, installs it, and pastes the resulting credentials back into the assistant's environment. Your job is to make that as painless as possible: pre-fill the manifest with the assistant's name, the scopes it needs, the events it subscribes to, and — when possible — the webhook URL.

## The assistant's environment

Every Gram assistant owns exactly one environment. It was created the first time \`update_assistant\` set a name and it's the only env you should ever write to for this assistant. Don't create a separate \`slack-prod\` env — everything (Slack, cron, any other integration) shares the same bag. Extend it with \`add_environment_keys\` as new requirements appear; populate it with \`request_environment_secrets\` when the user has a value to enter.

## Recommended flow (pre-filled webhook)

This flow lets Slack verify the webhook URL the moment the manifest is applied, so the user doesn't have to paste it in by hand.

1. **Attach a Slack toolset to the assistant.** \`list_integrations\` with keyword \`"slack"\` (or \`list_toolsets\` if one exists) → \`create_toolset\` if needed → \`attach_toolset\`. This binds the toolset to the assistant's shared env automatically. **This step matters for you, the onboarding agent, not just the runtime assistant**: the moment a Slack toolset is attached, its tools become callable from this onboarding chat too. You'll need them in step 7 to do the post-install handshake. Skip this and you can't self-identify the bot.
2. **Declare the Slack keys on the env.** \`add_environment_keys({ keys: ["SLACK_BOT_TOKEN", "SLACK_SIGNING_SECRET"] })\`. The env now advertises the two variables even though the values are empty — good for the UI, good for later discoverability.
3. **Create the Slack trigger.** \`create_trigger\` with \`definition_slug: "slack"\` and the relevant \`event_types\` in the config. Do NOT pass \`environment_id\` — the assistant's env is used by default. The response includes a \`webhook_url\` — Gram's endpoint is live and will respond to Slack's \`url_verification\` challenge even before the signing secret is configured. **Remember the trigger \`id\`** — you'll need it in step 7d.
4. **Show the Slack app guide.** \`show_slack_app_guide\` with \`app_name\` (the assistant's name), \`bot_scopes\`, \`bot_events\`, and the \`webhook_url\` from step 3. The component builds a pre-filled manifest link. The user clicks through, picks a workspace, and creates the app — Slack verifies the webhook URL against Gram on the spot.
5. **User installs the app.** On the app config page, the user clicks **Install to Workspace**. This is a separate click from Create. After approving, Slack mints the **Bot User OAuth Token** (\`xoxb-...\`). Make it explicit: without this step there's no bot token.
6. **Request values.** \`request_environment_secrets\` with:
   - \`SLACK_BOT_TOKEN\` — sensitive, starts with \`xoxb-\`, found on **OAuth & Permissions**.
   - \`SLACK_SIGNING_SECRET\` — sensitive, found on **Basic Information → App Credentials**.
   The form writes into the assistant's env automatically. Once submitted, the bot can post, event signatures will verify, **and the Slack tools attached in step 1 are now live for you to use.**
7. **Self-handshake — say hi, learn your own \`bot_id\`, and guard against reply loops.** Do this immediately after step 6. It's a single short burst — don't ask "shall I proceed?", just go.

   - **a. Ask for the user's Slack handle.** One short question. Accept \`@name\` or \`name\`.
   - **b. DM the user a greeting — in the assistant's own voice.** Use the Slack toolset's post-message tool against the handle they gave you. **Write the message yourself, in the persona/tone the user picked via \`propose_identity\`.** Do not reuse a templated line from this doc — there isn't one for a reason. Short is fine. The DM both confirms the bot token works *and* gives you a message you authored that you can inspect in step 7c.
   - **c. Search your own messages to find your \`bot_id\`.** Use the Slack toolset's message-search or conversation-history tool to pull back the greeting you just sent. Slack stamps every bot-authored message with a \`bot_id\` — that's the bot's *bot identity*, distinct from its user id. Read it off the message you sent in step 7b. Don't guess and don't substitute a user id; the trigger filter compares against \`bot_id\` specifically.
   - **d. Update the trigger to filter out the bot's own messages.** \`update_trigger(id, config)\` with the trigger \`id\` from step 3. Set \`config.filter\` to include \`event.bot_id != "<the bot_id you read in 7c>"\`. If the trigger already has a filter, AND the new condition onto the existing one with \`&&\`. **This is required**: without it, every reply the assistant posts to Slack will fire the trigger again and you'll get an infinite loop.

   Tell the user you're done in one short sentence — again, in the assistant's voice, not a fixed line from this doc.

## Fallback flow (no pre-filled webhook)

Use this only if the webhook URL isn't available yet (e.g. you skipped ahead and created the app first).

1. \`attach_toolset\` for the Slack toolset so its tools are live for you in step 7.
2. \`show_slack_app_guide\` without a \`webhook_url\`. User creates the app with scopes but no event subscriptions.
3. User installs the app, copies bot token + signing secret.
4. \`add_environment_keys\` + \`request_environment_secrets\` for both.
5. \`create_trigger\` — returns the webhook URL. Remember the trigger \`id\` for step 7d.
6. \`show_webhook_url\` so the user can paste it into Slack's **Event Subscriptions → Request URL** and click **Verify**. Subscribe to the relevant \`bot_events\` in Slack's UI.
7. Run the **self-handshake** (steps 7a–7d above): ask Slack handle → DM greeting → search own messages for \`bot_id\` → \`update_trigger\` adding \`event.bot_id != "<bot id>"\` to \`config.filter\`.

The pre-filled flow is strictly better: one Slack visit instead of two.

## Trigger config

- \`event_types\` *(string[])* — required. Slack event types the trigger reacts to. Common choices:
  - \`app_mention\` — bot is @-mentioned.
  - \`message\` — every message in subscribed surfaces (noisy; pair with \`filter\`).
  - \`reaction_added\` — emoji reactions.
- \`filter\` *(string, optional)* — CEL expression, evaluated against the event. **Do not mention CEL to the user.** If the user asks for selective behavior ("only respond to my team", "ignore weekends", "skip messages from bots", etc.), tell them you can narrow what fires the assistant and propose the rule in plain English; you write the CEL silently. Available fields on \`event\`: \`envelope_type\`, \`event_type\`, \`subtype\`, \`team_id\`, \`channel_id\`, \`thread_id\`, \`user_id\`, \`bot_id\`, \`app_id\`, \`text\`, \`timestamp\` — only these. If a request needs a field that isn't listed, say so and offer the closest workable rule. \`event.bot_id\` is the field the recommended-flow step 7 uses to break reply loops — read the actual id off a real bot-authored message, never invent one. AND multiple conditions with \`&&\`.

Do **not** invent dotted event names (\`message.im\`, \`message.channels\`, \`message.groups\`, etc.) — those are scope-style names and the trigger config rejects them. Only the bare event names from Slack's Events API are accepted.

## Recommended event_types per use case

| Use case | event_types |
|----------|-------------|
| @-mention bot in a channel | \`["app_mention"]\` |
| Watch channel/DM messages | \`["message"]\` — noisy, add a filter |
| Reaction-driven workflow | \`["reaction_added"]\` |
| Morning summary at 8am | **cron** trigger (see \`docs("cron")\`), not Slack — have the assistant fetch Slack via toolset tools and DM the user |

## Tokens, disambiguated

Slack exposes four different secrets on the Basic Information page. Only two matter here:

- **Signing Secret** — HMAC-SHA256 key used to verify \`x-slack-signature\` on incoming events. This is what Gram needs for webhook verification. Stored as \`SLACK_SIGNING_SECRET\`.
- **Bot User OAuth Token** — starts with \`xoxb-\`. Used to call the Slack Web API (post messages, read users, etc.). Stored as \`SLACK_BOT_TOKEN\`.
- **Client Secret** — OAuth app secret. Not used here.
- **Verification Token** — deprecated by Slack in favor of Signing Secret. Not used here.

A common user mistake is pasting the Client Secret or Verification Token instead of the Signing Secret. If signatures fail, that's the first thing to check.

## Gotchas

- **Reply loops.** The bot's own messages fire the trigger. The recommended flow's step 7 sets \`event.bot_id != "<bot id>"\` on \`config.filter\` to break the loop. If you skip step 7, expect the assistant to talk to itself the moment it posts.
- **Install to Workspace is a separate click from Create.** The \`xoxb-\` token only appears after install + approval. Always call this out — users stop after Create and can't find the token.
- **Scope changes require re-install.** If the assistant needs a bot scope later, Slack forces the workspace admin to re-approve. Ship the full intended scope list in the first manifest.
- **App name must be unique per workspace.** If the user already has a Slack app with the same name, the manifest save fails with \`duplicate_app_name\`. Suggest renaming before retrying.
- **Bot must be invited to channels.** For non-mention events in a public channel, the bot has to be a member. \`/invite @bot-name\`.
- **Enterprise Grid / org-policy workspaces** may show "Request to Install" instead of the direct install button. The user may need admin approval first.
- **URL length.** The pre-filled manifest is encoded into the deep link URL. Keep it lean — don't ship icons, long descriptions, or workflow steps in the manifest. If the link ever gets rejected, fall back to the paste-in flow.
- **Bot tokens start with \`xoxb-\`.** \`xoxp-\` is a user token and won't work. If the user types something starting with \`xoxp-\`, they copied the wrong field.
`,
};
