---
description: Investigate Gram production health and post a digest to Slack
---

# Gram Production Health Digest

You are producing a health report for Gram's production services. The report must be **actionable** and **visually structured** — critical issues must stand out immediately, tabular data must use code blocks, and every section must be separated by a divider.

**Before starting**: activate the `datadog` skill for Gram service names, MCP tools, and query guidelines.

> ⚠️ **MANDATORY FORMAT RULES — READ BEFORE COMPOSING THE MESSAGE:**
>
> 1. **Every major section MUST be preceded by a `divider` block.**
> 2. **Top endpoints, error type breakdowns, and latency tables MUST use triple-backtick code blocks** — never bullet points for tabular data.
> 3. **Code block tables must have aligned columns** using spaces. Minimum widths: endpoint 38 chars, count 8 chars, err% 6 chars, p95 8 chars.
> 4. **Monitors in alert MUST each get their own `section` block** — never combine them.
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
- Per-service breakdown (`gram-server`, `gram-worker`, `gram-dashboard`, `gram`, `fly`)

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
6. **gram (frontend) error rate timeseries cell** — uses RUM or log volume:
   ```json
   {
     "type": "notebook_cells",
     "attributes": {
       "definition": {
         "type": "timeseries",
         "title": "gram (frontend) Error Log Volume (1h buckets)",
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
7. **fly (functions) error rate timeseries cell**:
   ```json
   {
     "type": "notebook_cells",
     "attributes": {
       "definition": {
         "type": "timeseries",
         "title": "fly (functions) Error Log Volume (1h buckets)",
         "requests": [
           {
             "q": "sum:trace.http.server.request.errors{source:fly,env:prod}.rollup(sum, 3600)",
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
8. **Slow endpoints + top errors markdown table cell** with the real data from Steps 1–4.
9. **All Gram services error log stream cell**:
   ```json
   {
     "type": "notebook_cells",
     "attributes": {
       "definition": {
         "type": "log_stream",
         "query": "service:(gram-server OR gram-worker OR gram) env:prod status:error",
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

## Step 7: Compose the Slack message

Build a Slack Block Kit payload. Structure it so critical items are at the top and impossible to miss.

**REQUIRED: Use `slack_send_message` MCP tool** with the `blocks` array. Do NOT use curl unless the MCP tool is unavailable.

### Base structure

```json
{
  "channel": "C0AKLE930BX",
  "text": "<plain text fallback summarizing the verdict>",
  "blocks": [
    {
      "type": "header",
      "text": {
        "type": "plain_text",
        "text": "Gram Health Digest — <DAY> <DATE>"
      }
    },
    {
      "type": "section",
      "text": {
        "type": "mrkdwn",
        "text": "<VERDICT_EMOJI> *<One-line overall verdict>*"
      }
    }
  ]
}
```

Then append sections in this exact order:

### A. Monitors in alert (only if any exist)

**Each monitor gets its own section block.** Do NOT combine them.

````json
{ "type": "divider" },
{
  "type": "section",
  "text": { "type": "mrkdwn", "text": "🚨 *MONITORS IN ALERT*" }
},
{
  "type": "section",
  "text": {
    "type": "mrkdwn",
    "text": "🔴 *<Monitor name>* — <type>\n<What it means>\n*Notifying*: `#<channel>`\n*Source*: `server/internal/<path>` (<function>)"
  }
},
{ "type": "divider" },
{
  "type": "section",
  "text": { "type": "mrkdwn", "text": "⚠️ *Error Spike (last 6h vs prior 18h)*\n• `gram-server`: X errors in 6h (Y/h) vs Z in prior 18h (W/h) — *~Nx spike*" }
},
{
  "type": "section",
  "text": {
    "type": "mrkdwn",
    "text": "*Top gram-server error types (24h):*\n```\nerror message                     count   pct\nmcp server not found              2,662   41%\nmissing authorization code          686   11%\ninvalid or expired access token     300    5%\n```"
  }
}
````

> ⚠️ **The error type breakdown MUST be a code block table with aligned columns.** Never use bullet points for this data.

### B. Traffic summary

```json
{ "type": "divider" },
{
  "type": "section",
  "text": {
    "type": "mrkdwn",
    "text": "📊 *Traffic (24h)*\n• `gram-server`: ~Xk requests — ↑Y% vs previous 12h\n• `gram` (frontend): ~Xk log events — ↑Y%\n• `gram-worker`: ~Xk\n• `fly` (functions): ~Xk"
  }
}
```

### C. Top endpoints — MUST be a code block table

````json
{ "type": "divider" },
{
  "type": "section",
  "text": {
    "type": "mrkdwn",
    "text": "🔝 *Top Endpoints (gram-server, 24h)*\n```\nEndpoint                               Reqs    Err%   p95     Notes\nPOST /mcp/{slug}                       95.3k   3.2%   28ms\nPOST /rpc/hooks.otel/v1/logs           33.6k   0.0%   11ms\nGET  /mcp/{slug}                       21.2k   1.1%    3ms\n...```"
  }
}
````

> ⚠️ **This section MUST use a triple-backtick code block with space-aligned columns. NEVER use bullet points for the endpoint table.**

Column widths: endpoint 40 chars, reqs 8 chars, err% 6 chars, p95 8 chars, notes remaining.

### D. Slow endpoints (only if p95 > 2s)

````json
{ "type": "divider" },
{
  "type": "section",
  "text": {
    "type": "mrkdwn",
    "text": "🐢 *Slow Endpoints (p95 > 2s)*\n```\nEndpoint                               p95      p50    vol\nGET /rpc/auditlogs.listfacets          3.91s   800ms   4 req\nGET /rpc/auth.callback                 3.57s   400ms   6 req\n```\n*Global*: p50: Xms · p95: Xms · p99: Xms"
  }
}
````

> ⚠️ **The slow endpoint table MUST also be a code block with aligned columns. Never use bullet points.**

If all endpoints are fast, replace with: `⚡ *Latency* — All endpoints healthy. p50: Xms · p95: Xms · p99: Xms 🟢`

### E. Recommendation

```json
{ "type": "divider" },
{
  "type": "section",
  "text": {
    "type": "mrkdwn",
    "text": "💡 *Recommendation*\n<Your specific, concrete recommendation for the on-call engineer. One or two sentences. Name the action and where to look.>"
  }
}
```

### F. Footer

```json
{ "type": "divider" },
{
  "type": "context",
  "elements": [
    {
      "type": "mrkdwn",
      "text": "🔴 Critical  🟡 Warning  🟢 Healthy | <NOTEBOOK_URL|View charts in Datadog> | Generated by Claude Code + Datadog MCP"
    }
  ]
}
```

Replace `NOTEBOOK_URL` with the notebook URL from Step 5.

---

### Verdict emoji rules

- 🔴 if any active incidents or monitors in ALERT state
- 🟡 if elevated error rates (>2x normal), notable latency, or monitors in WARNING state
- 🟢 if everything looks healthy

---

## Step 8: Post to Slack

**Preferred: use `slack_send_message` MCP tool** — pass the `blocks` array directly.

**Fallback** (if MCP tool unavailable): write the JSON payload via Python then curl:

```bash
python3 << 'PYEOF'
import json
# ... build your payload dict here ...
with open("/tmp/slack-digest.json", "w") as f:
    json.dump(payload, f)
PYEOF

mise exec -- bash -c 'curl -s -X POST https://slack.com/api/chat.postMessage \
  -H "Authorization: Bearer $DATADOG_DIGEST_BOT_TOKEN" \
  -H "Content-Type: application/json" \
  --data @/tmp/slack-digest.json'
```

If `DATADOG_DIGEST_BOT_TOKEN` is not set, print the composed payload to the terminal instead and remind the user to:

1. Create a Slack app at https://api.slack.com/apps with the `chat:write` scope
2. Install it to the workspace and copy the Bot User OAuth Token (`xoxb-...`)
3. Add `DATADOG_DIGEST_BOT_TOKEN = "xoxb-..."` to `mise.local.toml`
