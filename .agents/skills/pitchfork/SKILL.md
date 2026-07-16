---
name: pitchfork
description: Manage Gram's local development services (server, worker, dashboard, streams, etc.) with pitchfork — start/stop/restart daemons, check health, and query their logs. Activate whenever you need to bring up the local dev stack, restart a service after a code change, check whether a service is running or why it failed, or read/search service logs — even if the user just says "start the server", "is the worker running?", or "check the logs".
---

# Pitchfork — Local Dev Services

[Pitchfork](https://pitchfork.jdx.dev) is a daemon manager. All Gram local services are declared in `pitchfork.toml` at the repo root and run under a pitchfork supervisor. Each daemon's `run` command is a mise task (`mise run start:<name>`) and its readiness is polled via `mise run check:daemon --name <name>`, so `start` only returns once the service is actually healthy.

## How to interact with pitchfork (in order of preference)

1. **MCP server** — if the `pitchfork` MCP tools are available (`pitchfork_status`, `pitchfork_logs`, `pitchfork_start`, `pitchfork_stop`, `pitchfork_restart`), use them. They return structured output and skip pager/formatting concerns entirely.
2. **CLI** — otherwise use the `pitchfork` CLI directly (commands below).
3. **Mise tasks** — `mise run start` / `mise run stop` bring the whole stack up/down. Use these for whole-stack operations even when the MCP/CLI are available, because `mise run start` also starts the supervisor and cleans up stale daemon entries. If the `pitchfork` binary itself is missing, run `mise install` first (mise provides it).

## Daemons

Declared in `pitchfork.toml`. Names are shown namespaced by project (e.g. `gram/server`) in `pitchfork list`, but commands accept the short name (`server`).

| Daemon                                      | What it is                                          |
| ------------------------------------------- | --------------------------------------------------- |
| `server`                                    | Main Gram API server                                |
| `worker`                                    | Temporal worker                                     |
| `dashboard`                                 | Dashboard frontend dev server (depends on `server`) |
| `streams`                                   | Go Pub/Sub consumers (`gram streams`)               |
| `pystreams-multi`                           | Python Pub/Sub consumers                            |
| `assistant-runtime`                         | Assistant runtime                                   |
| `dev-idp`, `dev-idp-dashboard`, `mock-oidc` | Local identity providers (login is credential-less) |
| `admin`                                     | Admin app (depends on `mock-oidc`)                  |
| `tunnel-gateway`, `tunnel-postgres-mcp`     | Tunnel gateway and its Postgres MCP daemon          |

## Whole-stack lifecycle

```bash
mise run start   # supervisor start + clean + start --all-local --force
mise run stop    # pitchfork stop --all-local
```

`mise run start` force-restarts everything. If services fail because Docker infra (Postgres, ClickHouse, etc.) isn't up, run `./zero --agent` for full environment setup instead.

## Individual daemons

`pitchfork start` is idempotent — it does nothing if the daemon is already running. After changing Go/backend code you must restart the affected daemon to pick up the change:

```bash
pitchfork start server worker      # start if not running (waits for ready)
pitchfork restart server           # stop + start (use after code changes)
pitchfork start server --force     # same as restart
pitchfork stop server
```

If commands fail because the supervisor isn't running, start it with `pitchfork supervisor start`.

## Status

```bash
pitchfork list                     # all daemons + status (running/available/errored/...)
pitchfork list --json              # machine-readable
pitchfork list --status errored --status failed   # only broken daemons
pitchfork status server            # detail for one daemon, includes error/exit info
```

`available` means defined in config but not started. `errored` includes the exit reason — follow up with logs.

The MCP `pitchfork_status` tool returns a JSON array covering all daemons (equivalent to `pitchfork list --json`); there is no per-daemon MCP detail call, so use `pitchfork status <name>` on the CLI when you need one daemon's full detail.

## Logs

This is the primary debugging tool. Logs persist across restarts and are timestamped.

```bash
pitchfork logs server -n 100               # last 100 lines
pitchfork logs server worker -n 50         # multiple daemons interleaved
pitchfork logs server --since 5min         # relative time window
pitchfork logs server --since '10:30' --until '11:00'
pitchfork logs server --grep error --grep panic   # OR'd, case-insensitive
pitchfork logs server --regex 'level=(warn|error)'
```

Tips for agent use:

- Add `--no-pager --raw` when piping or capturing output.
- Do not use `--tail`/`--follow` — it blocks forever. Prefer bounded queries (`-n`, `--since`) and poll if needed.
- After a restart, `pitchfork logs <name> --since 1min` shows just the fresh boot output.
- The MCP `pitchfork_logs` tool only supports "last N lines" (default 50); drop to the CLI when you need time windows or grep filters.
- Lines like `[start:server] ERROR ... exited with non-zero status` appear on every normal stop/restart — they are supervisor noise, not crashes. Look at the surrounding lines (clean shutdown sequence vs. panic/fatal) before concluding a service crashed.
