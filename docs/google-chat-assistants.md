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

You can maintain these values later on **Environments**. Never put credentials in the trigger configuration JSON. **Save both values before creating the trigger.** The backend checks required key presence; dashboard onboarding additionally checks that both values are populated before creating or resuming a trigger; empty declarations or a skipped/partial credential form are not sufficient. If either value is missing, finish the secure form first rather than retrying trigger creation. Then create the `googlechat` trigger targeting this assistant and copy its webhook URL. The Triggers page also exposes the URL and event status.

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

- **Native ingress: DMs and @mentions only.** An unmentioned follow-up in a space/thread does not wake the assistant, even after it previously replied. Mention it again. For explicit text commands from external participants, configure the separate Workspace Events mode below. No background full-history ingestion.
- **401 on webhook:** check non-add-on mode, Project Number audience, and the exact numeric environment value. Do not use your credential service account as the inbound signer or choose the endpoint URL as audience.
- **403 on history reads:** confirm `chat.app.messages.readonly` approval for the correct service-account Marketplace-compatible client and Workspace organization. Recheck app membership in the space. Do not solve this with user OAuth or domain-wide delegation.
- **403 on writes/spaces:** check Chat API is enabled, credentials belong to the configured app's project, the app is installed/allowed by Workspace policy, and it has access to the space. Updating/deleting messages is limited to app-owned messages.
- **No reply:** check trigger is active, both environment values are populated, the toolset attachment uses the same environment, and you sent a DM or @mention. The event history distinguishes delivery problems from assistant/tool failures.
- **Key creation blocked:** ask your Cloud administrator about the organization's service-account key policy. Do not bypass organization policy. Rotate compromised keys in Cloud and replace the environment secret.

## Management surfaces

Google Chat setup is intentionally a **dashboard onboarding workflow**, not a new Platform MCP administration tool. The operator selects an exact project and assistant, enters the customer-managed service-account key in the secure environment form, and binds the native toolset and trigger to that environment. The setup assistant receives populated key names, not credential values.

The native Google Chat tools and existing generic trigger tools live in `server/internal/platformtools`; they are distinct from `server/internal/platformmcp`. The latter's assistant identity tool manages workload identity, not assistant creation, toolset attachment, or trigger setup. It is not an existing setup workflow to extend.

**Platform MCP decision: omit a new setup tool in this release.** A raw service-account-key argument would expose credentials to the agent conversation, while a tool accepting only an existing environment would not complete the required secure credential-entry workflow. Do not claim that generic native trigger discovery provides Platform MCP parity. Use dashboard onboarding for installation and credential rotation; use native tools for the assistant's Chat actions. Regression coverage verifies the masked credential form, populated-key prerequisites, shared-environment binding, and discovery of the Google Chat trigger configuration.

## Google references

Verified against Google's documentation:

- [Authenticate and authorize as a Chat app](https://developers.google.com/workspace/chat/authenticate-authorize-chat-app): service-account key, Marketplace-compatible client, private Marketplace SDK configuration, and administrator approval.
- [Verify requests from Chat](https://developers.google.com/workspace/chat/verify-requests-from-chat), specifically **Authenticate requests with a Project Number JWT (Chat apps that aren't add-ons)**. The page's earlier add-on instructions are a different mode.
- [Configure a Chat app](https://developers.google.com/workspace/chat/configure-chat-api).

## Shared-space commands and scheduled digests

A shared space can contain members of multiple organizations, but Google disables native app interactions for external participants. A native `@mention` therefore cannot serve as a shared invocation mechanism. The optional **Workspace Events** ingress receives ordinary messages instead. Selected internal and external participants use the same literal command, for example `assistant: explain this digest`.

The assistant remains generic: configure its instructions and MCP access normally. A daily digest is an ordinary `cron` trigger targeting that assistant, with a note that names the exact destination space and instructs it to use `platform_googlechat_send_message`. Cron events do not carry a Google Chat source context, so the destination must be explicit in the assistant's task or trigger note. The digest's data sources and contents belong in the assistant configuration, not in the Google Chat integration. Attach the Google Chat message-reading tools if the assistant should answer questions about a previously posted digest or other thread context; inbound read permission does not attach those tools automatically. Use only MCP access appropriate for the shared audience.

### Configure a shared-space trigger

1. Create an invitation-only space with external membership enabled, install the Chat app, and invite the intended participants. Workspace policy must permit external spaces. Use the actual Chat user resource names (`users/{id}`), not display names or email guesses, for the participant allowlist.
2. Complete the service-account setup above, including Workspace administrator approval for `chat.app.messages.readonly`. This ingress needs message access even if the assistant has no history-reading tool attached. Enable the Google Workspace Events and Pub/Sub APIs in the app's Cloud project.
3. Create a dedicated Pub/Sub topic. Grant `chat-api-push@system.gserviceaccount.com` the Pub/Sub Publisher role **on that topic only**, as required by Google's Workspace Events setup.
4. Create a Workspace Events subscription as the approved Chat app service account, with this request body. Replace all placeholders; keep the returned `subscriptions/{id}` and its actual expiration time.

   ```json
   {
     "targetResource": "//chat.googleapis.com/spaces/<SPACE_ID>",
     "eventTypes": ["google.workspace.chat.message.v1.created"],
     "notificationEndpoint": {
       "pubsubTopic": "projects/<PROJECT_ID>/topics/<TOPIC_ID>"
     },
     "payloadOptions": { "includeResourceData": true },
     "ttl": "14400s"
   }
   ```

   Call `POST https://workspaceevents.googleapis.com/v1/subscriptions`. The same approved app identity can use `gws events subscriptions create --json '<body>'` with appropriately configured service-account authentication. The ordinary `gws` user login is a different identity and is not the app's credentials.

5. Create a dedicated push service account and reserve a Pub/Sub push subscription name. Configure the Google Chat trigger with the following shape. These fields contain identifiers, not credentials; keep the outbound service-account JSON in the assistant's secure shared environment.

   ```json
   {
     "space_names": ["spaces/<SPACE_ID>"],
     "workspace_events": {
       "subscription": "projects/<PROJECT_ID>/subscriptions/<PUSH_SUBSCRIPTION_ID>",
       "workspace_subscription": "subscriptions/<WORKSPACE_SUBSCRIPTION_ID>",
       "audience": "https://<PUBLIC_HOST>/<UNIQUE_PUSH_AUDIENCE>",
       "service_account_email": "<PUSH_ACCOUNT>@<PROJECT_ID>.iam.gserviceaccount.com",
       "user_names": ["users/<INTERNAL_USER_ID>", "users/<EXTERNAL_USER_ID>"],
       "text_prefix": "assistant:"
     }
   }
   ```

   The trigger requires exactly one space, an explicit nonempty user allowlist, and a nonempty prefix. An optional `filter` CEL expression can narrow it further using `event.text`, `event.user_name`, or the other existing event fields. Prefix matching is literal and case-sensitive after leading whitespace. Every invocation, including a reply to a digest, requires the prefix; unprefixed follow-ups do not invoke the assistant.

6. Create the authenticated, **wrapped** Pub/Sub push subscription against the dedicated topic. Set the trigger's returned webhook URL as the push endpoint, the dedicated push service account as the authentication identity, and `workspace_events.audience` as the exact token audience. Grant the Pub/Sub service agent the token-creation permission required by Google's authenticated-push setup. Keep publisher and service-account impersonation permissions narrow: anyone able to publish to this topic or impersonate the push account can submit events to this connection.
7. Test a command from one internal participant and one external participant, a command from an unlisted participant, and an ordinary unprefixed message. Only the first two should invoke the assistant. Test two simultaneous commands in different threads and redelivery: replies must stay in their original threads without duplication. Check trigger delivery errors as well as the visible replies. Do not claim live external-user support has been verified based only on a successful API subscription or synthetic tests.

This mode replaces native interaction delivery **for this trigger**. Do not configure the Chat app's native interaction endpoint to use this Pub/Sub webhook. The backend verifies a Google Accounts OIDC token for the exact audience and verified push service-account email, then checks both subscription identities and the space. The Project Number JWT used by native Chat interactions is not accepted here. Separate native-interaction and Workspace Events triggers remain possible, but overlapping triggers can invoke the assistant twice for the same message; use one path for a given space.

Pub/Sub receives all subscribed message creations, including unselected participants' messages; the backend filters before invoking the assistant. Bot messages, edits, deletes, and subscription lifecycle notifications never invoke it. Google may batch message creations automatically; each human message is filtered, deduplicated, and routed to its own thread, with only that message's payload attached to the assistant event. MCP authorization continues to use the configured assistant identity, not the external sender's credentials.

### Keep the subscription alive

Workspace Events subscriptions expire. With resource data included, use the expiration returned by Google rather than assuming a permanent subscription. Before enabling a durable workflow, configure an independent renewal job under the **same approved app identity** to call:

```http
PATCH https://workspaceevents.googleapis.com/v1/subscriptions/<WORKSPACE_SUBSCRIPTION_ID>?updateMask=ttl
Content-Type: application/json

{"ttl":"14400s"}
```

For example, an hourly Cloud Scheduler HTTP job can obtain an OAuth access token for that service account with the `https://www.googleapis.com/auth/chat.app.messages.readonly` scope and renew the four-hour subscription. Renewals must not run through an LLM or depend on a successful digest. Verify the returned operation completes and expiration advances, and monitor renewal failures and Workspace subscription lifecycle notifications. An expired subscription must be recreated, and the trigger's `workspace_subscription` updated to its new identity. Pausing a Speakeasy trigger does not delete the customer-managed Cloud subscription; retiring the integration requires deleting its renewal job and subscriptions too.

The backend accepts authenticated events; it does not provision or renew customer-managed Cloud resources. Existing native trigger tools expose this configuration through their discovered schema. No Platform MCP setup tool is added: provisioning and credential entry still require the operator-controlled setup described above. No new Temporal schedule or polling loop is introduced by this transport.

References: [Workspace event subscriptions](https://developers.google.com/workspace/events/guides/create-subscription), [event format and expiration](https://developers.google.com/workspace/events), [subscription renewal](https://developers.google.com/workspace/events/guides/update-subscription), [authenticated Pub/Sub push](https://cloud.google.com/pubsub/docs/authenticate-push-subscriptions), and [external-member restrictions](https://knowledge.workspace.google.com/admin/chat/chatting-with-external-users).
