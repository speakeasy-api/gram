# Connect an assistant to Google Chat

You own the Google Cloud project, Chat app, and service account. This integration uses a **Google Chat app that is not a Google Workspace add-on**, with an HTTP endpoint and **Project Number** authentication audience. Do not select Workspace add-on mode or HTTP endpoint URL audience. No user OAuth flow or domain-wide delegation is required.

## 1. Create your Chat app and service account

1. In a Google Cloud project owned by your organization, enable the **Google Chat API**. Record its numeric **Project number** (not the project ID).
2. Open **Google Chat API → Configuration**. Configure an interactive Chat app, not a Google Workspace add-on. Set its name, avatar, and description; enable receiving direct messages and joining spaces/group conversations. Limit visibility to your test users initially.
3. Under **IAM & Admin → Service Accounts**, create a service account in the same project. Under **Keys → Add key → Create new key → JSON**, download its key. Keep the JSON private; do not paste it into assistant chat, instructions, source code, or support tickets. Your organization must permit service-account keys.

The service account is used for **outbound Chat API calls**. It is not the inbound event signer: Google Chat signs incoming Project Number JWTs as `chat@system.gserviceaccount.com`.

## 2. Configure the assistant in Speakeasy

Open **Assistants → New assistant** (or open an existing assistant's setup chat) and ask to connect Google Chat. Choose the capabilities you need:

- Send messages and replies, update/delete the app's own messages, and list spaces: `https://www.googleapis.com/auth/chat.bot` (no special administrator approval).
- Get/list messages, including thread history: `https://www.googleapis.com/auth/chat.app.messages.readonly` (**one-time Workspace administrator approval**, below). Do not enable history tools unless you need them.

The setup assistant discovers the native Google Chat tool catalog, creates a dedicated toolset containing only the tools you select, and attaches it using the assistant's shared environment. The Google Chat trigger must use **that same environment**. Merely adding credentials to a trigger does not configure a separately attached toolset.

Use the secure environment form to set:

| Variable                           | Value                                                                                |
| ---------------------------------- | ------------------------------------------------------------------------------------ |
| `GOOGLE_CHAT_PROJECT_NUMBER`       | The numeric Cloud project number selected as the Chat app's authentication audience. |
| `GOOGLE_CHAT_SERVICE_ACCOUNT_JSON` | The complete downloaded service-account JSON, saved as a secret.                     |

You can maintain these values later on **Environments**. Never put credentials in the trigger configuration JSON. **Save both values before creating the trigger.** Trigger creation validates its required environment before returning a webhook URL; empty declarations or a skipped/partial credential form are not sufficient. If either value is missing, finish the secure form first rather than retrying trigger creation. Then create the `googlechat` trigger targeting this assistant and copy its webhook URL. The Triggers page also exposes the URL and event status.

## 3. Finish the Google Chat connection

1. In **Google Chat API → Configuration → Connection settings**, select **HTTP endpoint URL** and paste the trigger webhook URL.
2. Set **Authentication Audience → Project Number**. It must match `GOOGLE_CHAT_PROJECT_NUMBER`. Keep the app in non-add-on mode; add-ons have a different event format and signer and are not supported here.
3. Save the configuration. Add the app to a test space or open a direct message with it. Send a DM or explicitly **@mention** the app in a space to invoke the assistant.
4. Check the assistant's Triggers tab / Triggers event history for delivery and processing errors. Test sending a reply; if history tools are enabled, separately test reading a thread after administrator approval.

## 4. Optional: approve message and thread history

For `chat.app.messages.readonly`, a Workspace administrator must approve the **app authentication scope**. Granting `chat.bot` alone is not enough.

1. Open **IAM & Admin → Service Accounts → your account → Advanced settings**. Choose **Create Google Workspace Marketplace-compatible OAuth client**, then **Continue**. This is a service-account client, not an end-user OAuth consent flow. Do not enable domain-wide delegation.
2. Enable the **Google Workspace Marketplace SDK** in the same Cloud project. Open **APIs & Services → Google Workspace Marketplace SDK → App Configuration**.
3. Choose **App visibility: Private**, **Installation settings: Individual + admin install**, and **App integrations: Chat app**. This Marketplace selection does not mean Workspace add-on mode in Chat API configuration.
4. Under **OAuth scopes**, add `https://www.googleapis.com/auth/chat.app.messages.readonly`. **Do not add `chat.bot` here**: Google explicitly excludes it from this configuration. Fill in developer name, website, and email, then **Save draft**.
5. Have a Workspace administrator follow Google's [Set up authorization for Chat apps](https://support.google.com/a?p=chat-app-auth) procedure to grant approval to this app/client for the requested scope. Saving the draft alone does not grant access.

History reads are on demand, limited to spaces the app can access. This is not full-history ingestion or domain-wide access.

## Behavior and troubleshooting

- **DMs and @mentions only.** An unmentioned follow-up in a space/thread does not wake the assistant, even after it previously replied. Mention it again. No ambient monitoring or background full-history ingestion.
- **401 on webhook:** check non-add-on mode, Project Number audience, and the exact numeric environment value. Do not use your credential service account as the inbound signer or choose the endpoint URL as audience.
- **403 on history reads:** confirm `chat.app.messages.readonly` approval for the correct service-account Marketplace-compatible client and Workspace organization. Recheck app membership in the space. Do not solve this with user OAuth or domain-wide delegation.
- **403 on writes/spaces:** check Chat API is enabled, credentials belong to the configured app's project, the app is installed/allowed by Workspace policy, and it has access to the space. Updating/deleting messages is limited to app-owned messages.
- **No reply:** check trigger is active, both environment values are populated, the toolset attachment uses the same environment, and you sent a DM or @mention. The event history distinguishes delivery problems from assistant/tool failures.
- **Key creation blocked:** ask your Cloud administrator about the organization's service-account key policy. Do not bypass organization policy. Rotate compromised keys in Cloud and replace the environment secret.

## Google references

Verified against Google's documentation:

- [Authenticate and authorize as a Chat app](https://developers.google.com/workspace/chat/authenticate-authorize-chat-app): service-account key, Marketplace-compatible client, private Marketplace SDK configuration, and administrator approval.
- [Verify requests from Chat](https://developers.google.com/workspace/chat/verify-requests-from-chat), specifically **Authenticate requests with a Project Number JWT (Chat apps that aren't add-ons)**. The page's earlier add-on instructions are a different mode.
- [Configure a Chat app](https://developers.google.com/workspace/chat/configure-chat-api).
