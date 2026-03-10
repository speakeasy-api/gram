---
description: Query Datadog logs for the last 24h, summarize problems, and post findings to Slack
---

# Daily Log Digest

You are performing a daily log investigation for the Gram production service. Follow these steps carefully:

## Step 1: Query Datadog logs

Use the Datadog MCP tools to search for logs from the **last 24 hours** in production. Run multiple targeted searches:

1. **Errors and panics**: Search for `status:error` or `level:error` logs, focusing on high-frequency or novel errors
2. **Warnings**: Search for `status:warn` logs that appear repeatedly or in bursts
3. **Slow requests / timeouts**: Look for timeout, deadline exceeded, or context cancelled patterns
4. **5xx responses**: Search for HTTP 5xx status codes

For each search, aim to capture representative samples — not every single log line, just enough to identify patterns and frequencies.

## Step 2: Analyze and identify problem areas

Once you have the logs, analyze them to:

- **Group errors by type/pattern** — identify the top 3-5 distinct problems
- **Estimate frequency** — how often is each problem occurring?
- **Find relevant code** — for each problem, search the codebase (`server/internal/`) to identify the likely source file and function. Use Grep to find relevant error messages, function names, or identifiers from the logs.
- **Assess severity** — is this a known issue, a regression, or something new?

## Step 3: Compose the Slack message

Write a concise, actionable Slack message in mrkdwn format. Structure it as:

```
*Daily Log Digest* — <date, last 24h>

*Top Issues*

1. *<Error name/pattern>* — seen ~N times
   • What's happening: <1-2 sentences>
   • Likely location: `server/internal/<path>:<function>`
   • Severity: 🔴 High / 🟡 Medium / 🟢 Low

2. ...

*Summary*: <1-2 sentence overall health assessment>
```

Keep it skimmable. Max 5 issues. If production looks healthy, say so briefly.

## Step 4: Post to Slack

Post the message to the Gram engineering Slack channel (`C0AKLE930BX`) using the Slack Web API and a bot token. Write the message to a temp file first to avoid shell escaping issues, then post it:

```bash
# Write the JSON payload to a temp file
cat > /tmp/slack-digest.json << 'EOF'
{
  "channel": "C0AKLE930BX",
  "text": "MESSAGE_HERE"
}
EOF

# Post using the Slack Web API (wrapped in mise exec so env vars are loaded)
mise exec -- bash -c 'curl -s -X POST https://slack.com/api/chat.postMessage \
  -H "Authorization: Bearer $DATADOG_DIGEST_BOT_TOKEN" \
  -H "Content-Type: application/json" \
  --data @/tmp/slack-digest.json'
```

Replace `MESSAGE_HERE` in the JSON file with the composed message. Use `\n` for newlines within the JSON string.

If `DATADOG_DIGEST_BOT_TOKEN` is not set, print the message to the terminal instead and remind the user to:

1. Create a Slack app at https://api.slack.com/apps with the `chat:write` scope
2. Install it to the workspace and copy the Bot User OAuth Token (`xoxb-...`)
3. Add `DATADOG_DIGEST_BOT_TOKEN = "xoxb-..."` to `mise.local.toml`
