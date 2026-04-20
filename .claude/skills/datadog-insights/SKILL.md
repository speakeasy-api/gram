---
name: datadog-insights
description: Investigate Gram production health and post a digest to Slack
---

# Gram Production Health Digest

You are producing a health report for Gram's production services. The report must be **actionable** and **visually structured** — critical issues must stand out immediately, tabular data must use code blocks, and every section must be separated by a divider.

**Before starting**: activate the `datadog` skill for Gram service names, MCP tools, and query guidelines.

> ⚠️ **MANDATORY FORMAT RULES — READ BEFORE COMPOSING THE MESSAGE:**
>
> 1. **Every major section MUST be preceded by a Unicode divider line**: `──────────────────────────────────────` on its own line, with a blank line above and below.
> 2. **Top endpoints, error type breakdowns, and latency tables MUST use triple-backtick code blocks** — never bullet points for tabular data.
> 3. **Code block tables must have aligned columns** using spaces. Minimum widths: endpoint 38 chars, count 8 chars, err% 6 chars, p95 8 chars.
> 4. **Each monitor in alert MUST get its own paragraph** — never combine multiple monitors into one block.
> 5. **Do NOT collapse or omit data** to save space. If there are 8 monitors, show all 8.

---

## Step 1: Check for critical issues first

These take priority over everything else. If any exist, they become the top of the digest.

1. **Open incidents** — `search_datadog_incidents` for `state:(active OR stable)` in the last 24h
2. **Monitors in alert** — `search_datadog_monitors` for `status:alert`
3. **Error spikes** — Use `analyze_datadog_logs` with SQL:
   ```sql
   SELECT service, status, count(*) FROM logs GROUP BY service, status ORDER BY count(*) DESC
   ```
   Filter: `env:prod status:(error OR critical OR alert OR emergency)`, last 24h.
   Compare the last 6h vs. the previous 18h to detect spikes.

If there are critical issues, investigate each one:

- Get a sample of the actual error logs (`search_datadog_logs`)
- Follow trace IDs with `get_datadog_trace` to find root causes
- `Grep` in `server/internal/` for the error message to find the source code location

**For top error message breakdown**, use `analyze_datadog_logs`:

```sql
SELECT message, count(*) as cnt
FROM logs
WHERE service = 'gram-server' AND status IN ('error', 'critical')
GROUP BY message
ORDER BY cnt DESC
LIMIT 10
```

---

## Step 2: Top endpoints by traffic

Use `search_datadog_spans` for `service:gram-server env:prod` over the last 24h, or:

```
sum:trace.http.server.request.hits{service:gram-server,env:prod} by {resource_name}.rollup(sum, 86400)
```

Collect the **top 10 endpoints** with:

- Request count
- Error rate (% of requests returning 4xx/5xx)
- p95 latency

---

## Step 3: Traffic volume and trends

Compare traffic between two 12h windows:

1. **Current 12h**: `from: now-12h, to: now`
2. **Previous 12h**: `from: now-24h, to: now-12h`

Use `get_datadog_metric` with:

```
sum:trace.http.server.request.hits{service:gram-server,env:prod}.rollup(sum, 43200)
```

Report:

- Total requests in the last 24h
- % change between the two 12h periods (flag if > 30% change)
- Per-service breakdown (`gram-server`, `gram-worker`, `gram`, `fly`)

---

## Step 4: Latency analysis

```
p50:trace.http.server.request{service:gram-server,env:prod} by {resource_name}
p95:trace.http.server.request{service:gram-server,env:prod} by {resource_name}
p99:trace.http.server.request{service:gram-server,env:prod} by {resource_name}
```

Over the last 24h with `.rollup(avg, 86400)`.

Report:

- **Global latency**: p50, p95, p99 across all endpoints
- **Slowest 5 endpoints** by p95 latency (with their p50 for comparison)
- Flag any endpoint where p95 > 2s or p99 > 5s

---

## Step 5: Create a Datadog Notebook

Call `create_datadog_notebook` with name `"Gram Health Digest — <DAY> <DATE>"` (e.g. `"Gram Health Digest — Fri 2026-03-27"`). Use `absolute_time: true` with `start_time` = 24h ago and `end_time` = now. One notebook is created per run — old ones accumulate and can be manually deleted periodically.

The notebook `cells` must be wrapped in `{"cells": [...]}`. Include:

1. **Summary markdown cell**:
   ```json
   {
     "type": "notebook_cells",
     "attributes": {
       "definition": {
         "type": "markdown",
         "text": "One paragraph verdict with key numbers."
       }
     }
   }
   ```
2. **Error rate timeseries cell**:
   ```json
   {
     "type": "notebook_cells",
     "attributes": {
       "definition": {
         "type": "timeseries",
         "title": "gram-server Error Rate (1h buckets)",
         "requests": [
           {
             "q": "sum:trace.http.server.request.errors{service:gram-server,env:prod}.rollup(sum, 3600)",
             "display_type": "bars",
             "style": { "palette": "warm" }
           }
         ],
         "show_legend": true,
         "yaxis": { "scale": "linear" },
         "markers": [
           {
             "value": "y = 500",
             "display_type": "warning dashed",
             "label": "Elevated"
           }
         ]
       }
     }
   }
   ```
3. **Traffic volume timeseries cell**:
   ```json
   {
     "type": "notebook_cells",
     "attributes": {
       "definition": {
         "type": "timeseries",
         "title": "gram-server Traffic Volume (1h buckets)",
         "requests": [
           {
             "q": "sum:trace.http.server.request.hits{service:gram-server,env:prod}.rollup(sum, 3600)",
             "display_type": "area",
             "style": { "palette": "dog_classic" }
           }
         ],
         "show_legend": true,
         "yaxis": { "scale": "linear" }
       }
     }
   }
   ```
4. **p95 latency by endpoint timeseries cell**:
   ```json
   {
     "type": "notebook_cells",
     "attributes": {
       "definition": {
         "type": "timeseries",
         "title": "Top Endpoint p95 Latency",
         "requests": [
           {
             "q": "p95:trace.http.server.request{service:gram-server,env:prod} by {resource_name}.rollup(avg, 3600)",
             "display_type": "line",
             "style": { "palette": "dog_classic" }
           }
         ],
         "show_legend": true,
         "yaxis": { "scale": "linear" },
         "markers": [
           {
             "value": "y = 2",
             "display_type": "error dashed",
             "label": "2s threshold"
           }
         ]
       }
     }
   }
   ```
5. **gram-worker error rate timeseries cell**:
   ```json
   {
     "type": "notebook_cells",
     "attributes": {
       "definition": {
         "type": "timeseries",
         "title": "gram-worker Error Rate (1h buckets)",
         "requests": [
           {
             "q": "sum:trace.http.server.request.errors{service:gram-worker,env:prod}.rollup(sum, 3600)",
             "display_type": "bars",
             "style": { "palette": "warm" }
           }
         ],
         "show_legend": true,
         "yaxis": { "scale": "linear" }
       }
     }
   }
   ```
6. **gram (frontend) trace errors timeseries cell** — `gram` is an APM service, so use trace metrics:
   ```json
   {
     "type": "notebook_cells",
     "attributes": {
       "definition": {
         "type": "timeseries",
         "title": "gram (frontend) Trace Errors (1h buckets)",
         "requests": [
           {
             "q": "sum:trace.http.server.request.errors{service:gram,env:prod}.rollup(sum, 3600)",
             "display_type": "bars",
             "style": { "palette": "warm" }
           }
         ],
         "show_legend": true,
         "yaxis": { "scale": "linear" }
       }
     }
   }
   ```
7. **fly (functions) error log stream cell** — `fly` is a log source (not an APM service), so use a log stream, not a trace metric:
   ```json
   {
     "type": "notebook_cells",
     "attributes": {
       "definition": {
         "type": "log_stream",
         "title": "fly (functions) Error Logs (24h)",
         "query": "source:fly env:prod status:error",
         "columns": ["timestamp", "host", "message"],
         "message_display": "inline",
         "show_date_column": true,
         "show_message_column": true,
         "sort": { "column": "timestamp", "order": "desc" }
       }
     }
   }
   ```
8. **Slow endpoints + top errors markdown table cell** with the real data from Steps 1–4.
9. **All Gram services error log stream cell** — includes `source:fly` for Gram Functions logs:
   ```json
   {
     "type": "notebook_cells",
     "attributes": {
       "definition": {
         "type": "log_stream",
         "query": "(service:(gram-server OR gram-worker OR gram) OR source:fly) env:prod status:error",
         "columns": ["timestamp", "host", "service", "message"],
         "message_display": "inline",
         "show_date_column": true,
         "show_message_column": true,
         "sort": { "column": "timestamp", "order": "desc" }
       }
     }
   }
   ```

Save the notebook URL — you will link it in the Slack message footer.

---

## Step 6: Write a recommendation

Based on all the data gathered, write **one concrete recommendation** for the on-call engineer. Be specific:

- If errors are spiking: name the error type, the likely cause based on code grep, and the first action to take.
- If a slow endpoint is flagged: name it and suggest where to look (query, external call, etc.).
- If everything is healthy: say "No action needed. Monitor X for Y."
- If errors are declining: say so and advise continued monitoring.

This recommendation goes into the Slack message as a dedicated section.

---

## Step 7: Compose the Slack Block Kit message

Build a list of Block Kit blocks. The message is structured around the **4 Golden Signals**: Alerts → Errors → Traffic → Latency.

### Formatting rules

- **Prose and bullet lists**: use mrkdwn bullet points (`•`) with inline backtick formatting for endpoint/service names
- **Tabular data** (error type breakdowns, endpoint tables, latency tables): use triple-backtick code blocks inside `section` mrkdwn text — they render as aligned monospace in Slack and are much more readable than bullet points for columnar data
- **Verdict**: use a `section` with `fields` (2-column grid) — never a `context` block, which is too small to notice

### Verdict emoji rules

- 🔴 if any active incidents or monitors in ALERT state
- 🟡 if elevated error rates (>1.5x normal), notable latency, or monitors in WARNING/ALERT state
- 🟢 if everything looks healthy

---

### Block structure (in order)

**1. Header**

```json
{
  "type": "header",
  "text": { "type": "plain_text", "text": "Gram Health Digest — <DAY> <DATE>" }
}
```

**2. Verdict — `section` with `fields` (2-column grid)**

Always 6 fields: Status, Monitors in Alert, Errors (24h), Traffic (24h), Latency p95, Slow Endpoints.

```json
{
  "type": "section",
  "fields": [
    { "type": "mrkdwn", "text": "*Status*\n<VERDICT_EMOJI> <one-word status>" },
    { "type": "mrkdwn", "text": "*Monitors in Alert*\n<N (name)> or 0 🟢" },
    { "type": "mrkdwn", "text": "*Errors (24h)*\n<count> · ↑<Nx> last 6h" },
    { "type": "mrkdwn", "text": "*Traffic (24h)*\n~<Xk> · <↑/↓pct%> last 12h" },
    { "type": "mrkdwn", "text": "*Latency p95*\n<Xms> (global)" },
    {
      "type": "mrkdwn",
      "text": "*Slow Endpoints*\n<N endpoints > 2s> or All healthy 🟢"
    }
  ]
}
```

Follow with a divider.

---

**3. 🚨 Alerts** (omit section entirely if no monitors in alert)

Each monitor gets its own paragraph. Do NOT combine monitors.

```json
{
  "type": "section",
  "text": {
    "type": "mrkdwn",
    "text": "🚨 *Alerts*\n🔴 *<Monitor name>*\n<What it means and why it matters>\n*Notifying:* `#<channel>`\n\n🔴 *<Next monitor name>*\n<What it means>\n*Notifying:* `#<channel>`"
  }
}
```

Follow with a divider.

---

**4. ❌ Errors**

Bullet prose for per-service summary, then a **code block table** for top error types.

````
{"type": "section", "text": {"type": "mrkdwn", "text": "❌ *Errors*\n• `gram-server`: X errors in last 6h (Y/h) vs Z/h prior — *~Nx spike*\n• `gram-worker`: N errors (stable)\n• `gram` (frontend): N (stable)\n• `fly` (functions): 0 🟢\n\n*Top error types — gram-server (24h):*\n```\nmessage                                      count    pct\nnot found                                      402  31.4%\ntoken value is empty for bearer auth           270  21.1%\nmissing value for env var in api key auth       74   5.8%\nHTTP roundtrip failed                           70   5.5%\nno MCP install page metadata for toolset        65   5.1%\n```"}}
````

Follow with a divider.

---

**5. 📊 Traffic**

Bullet prose for trend, then a **code block table** for top endpoints by volume.

````
{"type": "section", "text": {"type": "mrkdwn", "text": "📊 *Traffic*\n• Previous 12h: ~Xk requests\n• Current 12h: ~Xk requests — *↑Y%* ⚠️ (flag if >30%)\n• Total 24h: ~Xk\n\n*Top endpoints by volume (24h):*\n```\nendpoint                                          hits\nPOST /mcp/{mcpSlug}                            103,784\nPOST /rpc/hooks.otel/v1/logs                    16,824\nPOST /rpc/hooks.claude                          14,956\nGET  /mcp/{mcpSlug}                             14,454\nGET  /.well-known/oauth-protected-resource       6,789\n```"}}
````

Follow with a divider.

---

**6. ⏱️ Latency**

If any endpoint has p95 > 2s, use a **code block table** for slow endpoints. Always include "approaching threshold" if any endpoints are 1–2s p95.

````
{"type": "section", "text": {"type": "mrkdwn", "text": "⏱️ *Latency*\n*Global:* p50: Xms · p95: Xms · p99: Xms\n\n*Slow endpoints (p95 > 2s):*\n```\nendpoint                           p95       p50   hits\nGET /rpc/toolsets.listfororg     7,275ms  5,766ms    57  ⚠️\nGET /rpc/usage.getperiodusage    5,173ms  3,403ms    49  ⚠️\nPOST /chat/completions           4,713ms  2,615ms    15  (AI)\n```\n*Approaching threshold (p95 > 1s):*\n```\nGET /rpc/environments.list       1,406ms             57\nGET /rpc/access.listgrants       1,281ms             84\n```"}}
````

If all endpoints are fast:

```json
{
  "type": "section",
  "text": {
    "type": "mrkdwn",
    "text": "⏱️ *Latency* — All endpoints healthy. p50: Xms · p95: Xms · p99: Xms 🟢"
  }
}
```

Follow with a divider.

---

**7. Recommendation**

```json
{
  "type": "section",
  "text": {
    "type": "mrkdwn",
    "text": "💡 *Recommendation*\n<Specific, concrete recommendation for the on-call engineer. One or two sentences. Name the action and where to look.>"
  }
}
```

Follow with a divider.

---

**8. Footer** — links to Datadog notebook and skill source

```json
{
  "type": "context",
  "elements": [
    {
      "type": "mrkdwn",
      "text": "🔴 Critical  🟡 Warning  🟢 Healthy  |  <NOTEBOOK_URL|View in Datadog>  |  <https://github.com/speakeasy-api/gram/blob/main/.claude/skills/datadog-insights/SKILL.md|Skill source>"
    }
  ]
}
```

Replace `NOTEBOOK_URL` with the actual notebook URL from Step 5.

---

## Step 8: Post to Slack

Write and run this Python script via Bash. Post to `#gram-datadog-insights` by default, unless a different channel was specified in the prompt.

```python
import json, urllib.request, os

env_path = os.path.expanduser("~/.config/gram/.env")
token = None
with open(env_path) as f:
    for line in f:
        if line.startswith("SLACK_BOT_TOKEN="):
            token = line.split("=", 1)[1].strip().strip('"').strip("'")
            break
if not token:
    raise RuntimeError("SLACK_BOT_TOKEN not found in ~/.config/gram/.env")

channel = "C0AKLE930BX"  # #gram-datadog-insights — override with channel name if specified in prompt

blocks = []  # replace with actual Block Kit blocks from Step 7

def slack_post(payload):
    data = json.dumps(payload).encode()
    req = urllib.request.Request(
        "https://slack.com/api/chat.postMessage",
        data=data,
        headers={"Content-Type": "application/json", "Authorization": f"Bearer {token}"},
        method="POST",
    )
    with urllib.request.urlopen(req) as resp:
        return json.loads(resp.read())

result = slack_post({
    "channel": channel,
    "text": "Gram Health Digest",
    "blocks": blocks,
})
if not result.get("ok"):
    raise RuntimeError(f"Slack error: {result}")

ts = result["ts"]
reply = slack_post({
    "channel": channel,
    "thread_ts": ts,
    "text": "<!subteam^S09EXM6DPCY|dev-mcp-oncall>",
})
if not reply.get("ok"):
    raise RuntimeError(f"Thread reply error: {reply}")

print(f"✓ Posted to {channel} (ts={ts}), oncall tagged in thread")
```

MANDATORY RULES — never violate:

- Post ALL content as ONE main message. Never split the digest across multiple messages.
- NEVER send test or placeholder messages. Only post if you have real data from Step 1.
- The thread reply must contain ONLY the oncall tag — nothing else.
