---
name: testing-local-slack
description: Use when setting up or testing the local Slack app, connecting Slack to a project assistant, configuring ngrok Slack callbacks, or diagnosing missing Slack messages, challenge verification failures, invalid signatures, stuck routing, or Slack assistant permission denied errors.
---

# Testing the local Slack app

Test Slack → tunnel → server → linked sender → project assistant → Slack reply. Verification or HTTP 200 alone does not prove the assistant ran.

**Shared development app:** open <https://api.slack.com/apps> and ask a maintainer for the shared development app link and edit access to configure URLs and obtain credentials. Event and interactivity URLs are shared, not per developer: coordinate before taking over, record previous settings, and restore them when finished. Never change a production app.

## Quick reference

| Purpose                | Setting                                                    |
| ---------------------- | ---------------------------------------------------------- |
| OAuth client           | `SLACK_CLIENT_ID`, `SLACK_CLIENT_SECRET`                   |
| Request verification   | `SLACK_SIGNING_SECRET`                                     |
| OAuth callback         | `<GRAM_SERVER_URL>/rpc/slackDirectoryConnections.callback` |
| Incoming messages      | `https://<tunnel-host>/slack/events`                       |
| Approval button clicks | `https://<tunnel-host>/slack/interactions`                 |
| Local-only credentials | Ignored `mise.local.toml`                                  |

## 1. Local services and credentials

Use `pitchfork` for services and `gram-playwright-cli` for browser automation. Wake the stack if needed. Read `GRAM_SERVER_URL` and `GRAM_SITE_URL`; do not assume ports or HTTP/TLS. Open the dashboard at the latter.

In the app's **Basic Information → App Credentials**, obtain the client ID, client secret, and signing secret. Store them using `mise set --file mise.local.toml KEY=VALUE`. Never print secrets, put them in chat, enable shell tracing, or commit them.

Alternatively, use authorized Google Secret Manager access. Ask a maintainer to confirm the development GCP project and secret names:

| Secret                              | mise key               |
| ----------------------------------- | ---------------------- |
| `<DEV_SLACK_CLIENT_ID_SECRET>`      | `SLACK_CLIENT_ID`      |
| `<DEV_SLACK_CLIENT_SECRET_SECRET>`  | `SLACK_CLIENT_SECRET`  |
| `<DEV_SLACK_SIGNING_SECRET_SECRET>` | `SLACK_SIGNING_SECRET` |

Capture `gcloud secrets versions access latest --project <DEV_PROJECT> --secret <SECRET>` into a variable, pass it directly to `mise set`, then discard it. If authentication expired, ask the user to run `gcloud auth login`. Do not pick unrelated registry/bot tokens. The stored signing secret was stale during testing: check the app’s current value if signatures fail. Updating shared secrets requires explicit approval.

Restart server and worker after configuration changes:

```sh
mise exec -- pitchfork restart server worker
```

**No manually supplied bot token is needed for Connect Slack.** OAuth obtains and stores it. `mise run zero:slack` plus `GRAM_DEV_SLACK_BOT_TOKEN` and `mise run seed` is the alternative own-app approval/directory fixture flow, not a prerequisite here. `local/slack/app-manifest.yaml` describes that older flow; it is not a complete assistant-events manifest.

## 2. Tunnel and Slack settings

Run ngrok against the local server, not the dashboard. Example only for an HTTP server on 8080:

```sh
ngrok http 8080
```

Keep ngrok running. Record its hostname and, if backgrounding, process ID and log path. Hostnames can change on restart.

In the shared app:

1. **OAuth & Permissions → Redirect URLs:** add the exact callback derived from `GRAM_SERVER_URL`; retain existing entries. The browser follows it; localhost can work. Match scheme/port. Do not substitute the tunnel automatically: the login session belongs to the local origin.
2. **OAuth & Permissions → Bot Token Scopes:** `chat:write`, `users:read`, `users:read.email`, `app_mentions:read`, `im:history`.
3. **Socket Mode:** off for HTTP event delivery.
4. **Event Subscriptions:** enable, set the Request URL to `https://<tunnel-host>/slack/events`, wait for **Verified**, add bot events `app_mention`, `message.im`, and `app_home_opened`, then **Save Changes**. Verification alone does not save: without subscriptions the save button was disabled and refreshing reverted the toggle.
5. **Interactivity & Shortcuts:** enable and set the Request URL to `https://<tunnel-host>/slack/interactions`.
6. **App Home:** enable the Home tab, enable Messages, and allow users to send messages (manifest equivalent: `home_tab_enabled: true`, `messages_tab_read_only_enabled: false`).

After scope changes, reconnect/reinstall through **Connect Slack** in the local dashboard.

## 3. Project and assistant

On the organization's Slack workspaces page, confirm the workspace is connected, map your Slack account to your Speakeasy user, and select the target project (normally `default`). Verify it rather than assuming automatic defaults.

Open that project's Project Assistant/chat to provision the managed assistant. Check assistant runtime readiness; if logs say `local runtime image not found`, run `mise run build:assistants-runtime-image`. For a local “trial expired” blocker, preview `mise run account repair --dry-run`, then run `mise run account repair`.

DM the app, or invite it into a channel and @mention it. Ask “Who am I?” to check linked sender context; ask it to list organization users to exercise `org:read`. Test a second mapped user without that permission: their directory call must be denied, never retried as the owner. Trusted sender context is not proof of tool authorization.

## 4. Observe and diagnose

Inspect ngrok (usually port 4040). Check its target matches `GRAM_SERVER_URL`. These commands print tunnel/request metadata only:

```sh
curl -s http://localhost:4040/api/tunnels |
  jq -r '.tunnels[] | [.public_url, .config.addr] | @tsv'
curl -s 'http://localhost:4040/api/requests/http?limit=10' |
  jq -r '.requests[] | [.start, .request.method, .request.uri, .response.status] | @tsv'
```

If metadata is insufficient, inspect bounded service logs locally:

```sh
mise exec -- pitchfork logs server worker -n 200 --no-pager --raw
```

**Logs may contain sensitive data.** Filter before returning output; never dump raw requests, tokens, message contents, or customer identifiers.

| Observation                                                         | Next check                                                                                                                                                |
| ------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| No events                                                           | Saved subscriptions, Socket Mode, current tunnel, correct app                                                                                             |
| “Your URL didn’t respond with the value of the challenge parameter” | Inspect delivery/status. For 401 `invalid signature`, check current signing secret and restart; otherwise check tunnel/target. Never disable verification |
| Event 200 but no reply                                              | Sender mapping, default project, active managed assistant, then worker/runtime logs                                                                       |
| Stuck “routing”                                                     | Find the turn error; new messages are better probes than an old status                                                                                    |
| `trigger workload identity is UNAVAILABLE`                          | Slack trigger identity binding; fix provisioning, not authorization bypasses                                                                              |
| `slack HTTP client not configured`                                  | Reply-tool startup wiring must supply a Guardian HTTP client                                                                                              |
| `issuer name is taken by another registration`                      | Seed placeholder issuer may collide with runtime provisioning; investigate exact target before repairing                                                  |
| Directory permission denied                                         | Live invoking-user, assistant-agent, and owner ceilings; never grant broad access just to clear an error                                                  |

An unsigned POST returning 401 proves reachability only. Success means a real signed message produces an assistant turn and a visible Slack reply. Restore shared URLs and stop only your tunnel process afterwards.
