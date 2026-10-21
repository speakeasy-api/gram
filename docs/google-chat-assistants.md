# Google Chat assistants

An organization administrator opens **Org > Identity > Google Chat**, chooses **Add Google account**, and completes Google consent. They select a project and assistant, then create an externally accessible space or choose an existing space they can manage. Speakeasy installs its Chat app, invites the specified people, attaches the Chat tools, creates event delivery, and maintains its subscription.

The hosted connection is the only shared-space setup. Customers do not supply a Cloud project, Pub/Sub subscription, service-account key, or renewal job. Google Workspace policies must allow the app, external spaces, and the invited participants. A Google-owned space works only if its administrators permit the app and an authorized member can add it; Speakeasy cannot override the owning organization's policies. Creating a Speakeasy-owned test space is usually the simplest first test.

## Identity and access

Sync reads Google-reported member identities and matches their emails to active Speakeasy organization members. Unmatched members need an explicit administrator mapping. A removed mapping stays removed during later syncs. External participants need their own active Speakeasy identity and the appropriate project/MCP access.

Every incoming turn resolves its sender again. The assistant acts with that person's permitted access, intersected with the assistant's attached MCPs and identity grants. Membership in a Chat space grants no MCP rights. Unmapped senders get a single explanation per thread asking an administrator to link their account. Pausing or disconnecting a binding stops new turns.

Responses are visible to the space's participants. Administrators should attach only MCPs appropriate for that audience; per-person authorization does not make posted replies private.

## Conversations, issue triage, and digests

A prefix such as `assistant:` starts a new conversation. Once invoked in a thread, subsequent messages from mapped participants continue the conversation without the prefix. Participants share one assistant chat, with their identity resolved independently each turn.

An automation can handle a GitHub issue and call `platform_googlechat_send_message` to post its triage summary. Speakeasy records the returned Chat thread against the originating assistant conversation. Team replies continue that conversation with the issue context intact. The same mechanism applies to a daily digest: configure an ordinary scheduled assistant trigger with its sources, destination space, and posting instructions. Google Chat does not prescribe the digest's content.

The assistant receives incoming messages as events. It may request earlier public thread history through its attached Chat tools when needed. Message text and fetched history remain untrusted input.

## One-time operator setup

The hosted deployment needs one Google Cloud project with Chat, Workspace Events, and Pub/Sub enabled; a configured Chat app; an OAuth web client; and a dedicated event topic. Both the server and worker consume:

| Variable                                  | Purpose                                                 |
| ----------------------------------------- | ------------------------------------------------------- |
| `GOOGLE_CHAT_CLIENT_ID`                   | Hosted OAuth client ID                                  |
| `GOOGLE_CHAT_CLIENT_SECRET`               | Hosted OAuth client secret                              |
| `HOSTED_GOOGLE_CHAT_SERVICE_ACCOUNT_JSON` | Hosted app and Pub/Sub provisioning identity            |
| `GOOGLE_CHAT_TOPIC`                       | Full `projects/<PROJECT_ID>/topics/<TOPIC_ID>` resource |
| `GOOGLE_CHAT_PUSH_SERVICE_ACCOUNT`        | Dedicated push authentication service-account email     |

Register the callback at the server's `/rpc/googleChatConnections.callback` path. Use the path declared by `googlechatconnections.CallbackPath` if deploying behind a path prefix. Restrict topic publishing to Google's Chat event publisher (`chat-api-push@system.gserviceaccount.com`) and provisioning permissions to the hosted identity. Grant the Pub/Sub service agent the token-creation permission required for authenticated push. Each binding's push subscription filters on its exact space, authenticates the expected audience and push identity, and validates the event's subscription and target. Unrelated well-formed deliveries are acknowledged and dropped.

Google consent requests `openid`, `email`, `chat.spaces.create`, `chat.spaces.readonly`, `chat.memberships`, and `chat.memberships.app`. The user token is used for setup and membership sync. It is not used to read messages or renew event subscriptions.

**A Workspace administrator must also approve the app's `chat.app.messages.readonly` scope.** This supports event subscriptions and public message history with app authentication, independently of the connecting administrator's personal message access. Google login alone cannot grant this app scope. Missing approval prevents activation; reconnecting a user does not replace it. Follow Google's [app authentication setup](https://developers.google.com/workspace/chat/authenticate-authorize-chat-app) and [Workspace Events authorization requirements](https://developers.google.com/workspace/events/guides/auth).

## Delivery and renewal

Subscriptions request `payloadOptions.includeResource: true` so events contain the message. Provisioning waits for Google's long-running create operation, stores the returned lease, and creates filtered, authenticated push delivery. [Subscription reference](https://developers.google.com/workspace/events/reference/rest/v1/subscriptions).

A shared hourly Temporal schedule renews active hosted bindings using app authentication. It runs in the worker, independently of assistants, and reports failures on the binding. There is one renewal job, with approximately 720 workflow starts plus 720 activities per month per shared namespace, before retries. The activity pages through bindings; it does not create an activity per space.

Preview workers do not register this shared schedule. A preview binding therefore expires after approximately four hours unless repaired or renewed explicitly. Production and main development workers register the maintenance job. [Renewal reference](https://developers.google.com/workspace/events/guides/update-subscription).

## Management and verification

Org administrators can inspect connections, synchronize members, map identities, pause/resume or repair a binding, and disconnect. Platform MCP exposes the corresponding administrative outcomes without exposing credentials; interactive Google consent remains a browser action.

Before rollout, test internal and external mapped participants, an unmapped participant, a prefix-free follow-up, a GitHub-originated Chat thread, and renewal from the worker. Verify identity-specific MCP denials, cross-space isolation, and pause/disconnect behavior. A successful API call or mocked transport test is not evidence of a completed live Google approval flow.

## Native app compatibility

The base integration still supports customer-managed native DMs and mentions. Its per-environment `GOOGLE_CHAT_SERVICE_ACCOUNT_JSON` and `GOOGLE_CHAT_PROJECT_NUMBER` remain separate from hosted deployment credentials. Native interactions use Project Number authentication for a non-add-on Chat app. This compatibility path does not offer shared-space text delivery or its own subscription renewal mode.
