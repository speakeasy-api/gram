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

## Step 7: Compose the Slack message

Use the `slack_send_message` MCP tool with the `message` parameter (Slack markdown). The tool does **not** support Block Kit `blocks` — use plain Slack markdown only.

Slack markdown syntax: `*bold*`, `_italic_`, `` `code` ``, ` ```code block``` `, `<URL|link text>`.

### Message structure

The message opens with three lines — no separate header block:

```
*Gram Health Digest — <DAY> <DATE>*
<VERDICT_EMOJI> *<One-line overall verdict>*
<!subteam^S09EXM6DPCY|dev-mcp-oncall>
```

Then append sections in this exact order, each preceded by the Unicode divider line on its own line with a blank line above and below:

```
──────────────────────────────────────
```

### A. Monitors in alert (only if any exist)

**Each monitor gets its own paragraph.** Do NOT combine multiple monitors.

```
──────────────────────────────────────


🚨 *MONITORS IN ALERT*

🔴 *<Monitor name>* — <type>
<What it means and why it matters>
*Notifying:* `#<channel>`

🔴 *<Next monitor name>* — <type>
<What it means>
*Notifying:* `#<channel>`
```

### A2. Error spike (always include if a spike was detected; independent of monitors)

````
──────────────────────────────────────


⚠️ *Error Spike (last 6h vs prior 18h)*
• `gram-server`: X errors in 6h (Y/h) vs Z in prior 18h (W/h) — *~Nx spike*

*Top gram-server error types (24h):*
```
error message                          count    pct
mcp server not found                   1,288  17.8%
token exchange failed                    447   6.2%
not found                                273   3.8%
```
````

> ⚠️ **The error type breakdown MUST be a code block table with aligned columns.** Never use bullet points for this data.

### B. Traffic summary

```
──────────────────────────────────────


📊 *Traffic (24h)*
• `gram-server`: ~Xk requests — ↑Y% vs previous 12h
• `gram` (frontend): ~X error log events
• `gram-worker`: ~Xk
• `fly` (functions): X errors
```

### C. Top endpoints — MUST be a code block table

````
──────────────────────────────────────


🔝 *Top Endpoints — gram-server (last ~Xh)*
```
Endpoint                                         Hits    Err%   p95
POST /mcp/{mcpSlug}                           191,311   0.1%   21ms
GET  /.well-known/oauth-protected-resource     39,262   0.1%    3ms
GET  /.well-known/oauth-authorization-server   39,158   0.0%    5ms
GET  /mcp/{mcpSlug}                            13,211   0.0%    2ms
POST /rpc/hooks.claude                          1,267   0.0%   80ms
```
````

> ⚠️ **This section MUST use a triple-backtick code block with space-aligned columns. NEVER use bullet points for the endpoint table.**

Column widths: endpoint 48 chars, hits 8 chars, err% 6 chars, p95 8 chars, notes remaining. Flag any endpoint with Err% > 5% with ⚠️.

### D. Slow endpoints (only if p95 > 2s)

````
──────────────────────────────────────


🐢 *Slow Endpoints (p95 > 2s)*
```
Endpoint                                p95       p50    Hits
GET /rpc/usage.getperiodusage         2,040ms   817ms      6  ⚠️
GET /rpc/auth.info                      638ms     8ms      6
```
*Global:* p50: Xms · p95: Xms · p99: Xms
````

> ⚠️ **The slow endpoint table MUST use a code block with aligned columns. Never use bullet points.**

If all endpoints are fast, use instead:

```
⚡ *Latency* — All endpoints healthy. p50: Xms · p95: Xms · p99: Xms 🟢
```

### E. Recommendation

```
──────────────────────────────────────


💡 *Recommendation*
<Specific, concrete recommendation for the on-call engineer. One or two sentences. Name the action and where to look.>
```

### F. Footer

```
──────────────────────────────────────

🔴 Critical  🟡 Warning  🟢 Healthy  |  <NOTEBOOK_URL|View charts in Datadog>  |  Generated by Claude Code + Datadog MCP
```

Replace `NOTEBOOK_URL` with the notebook URL from Step 5.

---

### Verdict emoji rules

- 🔴 if any active incidents or monitors in ALERT state
- 🟡 if elevated error rates (>2x normal), notable latency, or monitors in WARNING state
- 🟢 if everything looks healthy

---

## Step 8: Post to Slack

Use `slack_send_message` with `channel_id: C0AKLE930BX` and the composed `message` string. If the MCP tool is unavailable, print the message to the terminal instead.
