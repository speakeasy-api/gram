// ask-poc-control: a local stdio MCP server that runs the Ask POC for the
// control panel artifact. The artifact reaches it through the Claude app as
// host:ask-poc-control, so the page can restart the mock servers, read their
// event log and approve or deny asks with buttons.
//
// Register once:  claude mcp add --scope user ask-poc-control -- <node> <this file>
// It shares .run/ (pid, log) with ask-poc.sh, so both stay interchangeable.

import { execFileSync, spawn } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import readline from "node:readline";
import { fileURLToPath } from "node:url";

const DIR = path.dirname(fileURLToPath(import.meta.url));
const RUN_DIR = path.join(DIR, ".run");
const PID_FILE = path.join(RUN_DIR, "server.pid");
const LOG_FILE = path.join(RUN_DIR, "server.log");
const SERVER = path.join(DIR, "server.mjs");
const SLACK_PORT = 4701;
const APPROVALS_PORT = 4702;
const APPROVALS = `http://127.0.0.1:${APPROVALS_PORT}`;
const WATCHER_PATH = path.join(
  os.homedir(),
  ".claude",
  "ask-poc",
  "ask-watch-v1.sh",
);
const SUPPORTED_VERSIONS = ["2025-03-26", "2025-06-18", "2025-11-25"];

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function readPid() {
  try {
    const pid = Number(fs.readFileSync(PID_FILE, "utf8").trim());
    if (!pid) return null;
    process.kill(pid, 0);
    return pid;
  } catch {
    return null;
  }
}

function listenerPids(port) {
  try {
    return execFileSync(
      "/usr/sbin/lsof",
      ["-nP", "-t", `-iTCP:${port}`, "-sTCP:LISTEN"],
      { encoding: "utf8" },
    )
      .split("\n")
      .map((s) => Number(s.trim()))
      .filter(Boolean);
  } catch {
    return [];
  }
}

async function getJson(url) {
  try {
    const res = await fetch(url, { signal: AbortSignal.timeout(1500) });
    if (!res.ok) return null;
    return await res.json();
  } catch {
    return null;
  }
}

async function status() {
  const health = await getJson(`${APPROVALS}/health`);
  const slack = await getJson(`http://127.0.0.1:${SLACK_PORT}/health`);
  let logLines = 0;
  try {
    logLines = fs
      .readFileSync(LOG_FILE, "utf8")
      .split("\n")
      .filter(Boolean).length;
  } catch {}
  return {
    running: Boolean(health && slack),
    pid: readPid(),
    endpoints: {
      slack: `http://127.0.0.1:${SLACK_PORT}/mcp`,
      approvals: `${APPROVALS}/mcp`,
    },
    config: health
      ? {
          autoApproveSeconds: health.autoApproveSeconds,
          errorStyle: health.errorStyle,
        }
      : null,
    blockedCalls: health?.blockedCalls ?? [],
    asks: health?.asks ?? [],
    sent: health?.sent ?? [],
    watcherInstalled: fs.existsSync(WATCHER_PATH),
    watcherPath: WATCHER_PATH,
    logLines,
    checkedAt: new Date().toISOString(),
  };
}

async function stop() {
  const pid = readPid();
  if (pid) {
    try {
      process.kill(pid);
    } catch {}
  }
  for (const port of [SLACK_PORT, APPROVALS_PORT]) {
    for (const p of listenerPids(port)) {
      try {
        process.kill(p);
      } catch {}
    }
  }
  try {
    fs.rmSync(PID_FILE);
  } catch {}
  for (let i = 0; i < 20; i++) {
    if (
      listenerPids(SLACK_PORT).length === 0 &&
      listenerPids(APPROVALS_PORT).length === 0
    )
      break;
    await sleep(100);
  }
}

function clearWatcher() {
  try {
    fs.rmSync(WATCHER_PATH);
    return true;
  } catch {
    return false;
  }
}

async function start({ autoApproveSeconds, errorStyle }) {
  fs.mkdirSync(RUN_DIR, { recursive: true });
  fs.writeFileSync(LOG_FILE, "");
  const out = fs.openSync(LOG_FILE, "a");
  const child = spawn(process.execPath, [SERVER], {
    detached: true,
    stdio: ["ignore", out, out],
    env: {
      ...process.env,
      ASK_POC_AUTO_APPROVE_SECONDS: String(autoApproveSeconds),
      ASK_POC_ERROR_STYLE: errorStyle,
    },
  });
  child.unref();
  fs.writeFileSync(PID_FILE, String(child.pid));
  for (let i = 0; i < 40; i++) {
    const s = await status();
    if (s.running) return s;
    await sleep(150);
  }
  return status();
}

function readLogs(after, limit) {
  let lines = [];
  try {
    lines = fs.readFileSync(LOG_FILE, "utf8").split("\n").filter(Boolean);
  } catch {}
  const from = Math.max(0, Math.min(after, lines.length));
  const slice = lines.slice(from, from + limit);
  const events = slice.map((line, i) => {
    try {
      return { n: from + i, ...JSON.parse(line) };
    } catch {
      return { n: from + i, event: "raw", text: line };
    }
  });
  return { events, next: from + slice.length, total: lines.length };
}

async function post(pathname) {
  const res = await fetch(`${APPROVALS}${pathname}`, {
    method: "POST",
    signal: AbortSignal.timeout(3000),
  });
  return { ok: res.ok, status: res.status, text: (await res.text()).trim() };
}

const tools = [
  {
    name: "poc_status",
    description:
      "Whether the Ask POC servers are running, their config, blocked calls, asks and sent messages.",
    inputSchema: { type: "object", properties: {} },
    annotations: { readOnlyHint: true },
  },
  {
    name: "poc_logs",
    description: "Events from the POC server log, starting after line `after`.",
    inputSchema: {
      type: "object",
      properties: {
        after: { type: "integer", minimum: 0 },
        limit: { type: "integer", minimum: 1, maximum: 2000 },
      },
    },
    annotations: { readOnlyHint: true },
  },
  {
    name: "poc_restart",
    description: "Stop the POC servers and start them fresh with empty state.",
    inputSchema: {
      type: "object",
      properties: {
        auto_approve_seconds: { type: "integer", minimum: 0, maximum: 3600 },
        error_style: { type: "string", enum: ["tool", "jsonrpc"] },
        clear_watcher: {
          type: "boolean",
          description:
            "Delete the saved watcher script so the agent has to write it again",
        },
      },
    },
    annotations: { readOnlyHint: false, destructiveHint: false },
  },
  {
    name: "poc_stop",
    description: "Stop the POC servers.",
    inputSchema: { type: "object", properties: {} },
    annotations: { readOnlyHint: false, destructiveHint: false },
  },
  {
    name: "poc_reset",
    description:
      "Clear blocked calls, asks and sent messages without restarting.",
    inputSchema: {
      type: "object",
      properties: { clear_watcher: { type: "boolean" } },
    },
    annotations: { readOnlyHint: false, destructiveHint: false },
  },
  {
    name: "poc_decide",
    description: "Approve or deny a pending ask by id.",
    inputSchema: {
      type: "object",
      properties: {
        ask_id: { type: "string" },
        decision: { type: "string", enum: ["approve", "deny"] },
      },
      required: ["ask_id", "decision"],
    },
    annotations: { readOnlyHint: false, destructiveHint: false },
  },
];

async function callTool(name, args) {
  switch (name) {
    case "poc_status":
      return status();
    case "poc_logs":
      return readLogs(Number(args.after ?? 0), Number(args.limit ?? 500));
    case "poc_restart": {
      const autoApproveSeconds = Number.isFinite(
        Number(args.auto_approve_seconds),
      )
        ? Number(args.auto_approve_seconds)
        : 10;
      const errorStyle = args.error_style === "jsonrpc" ? "jsonrpc" : "tool";
      await stop();
      const watcherCleared = args.clear_watcher ? clearWatcher() : false;
      const s = await start({ autoApproveSeconds, errorStyle });
      return { ...s, watcherCleared };
    }
    case "poc_stop":
      await stop();
      return status();
    case "poc_reset": {
      const r = await post("/reset");
      const watcherCleared = args.clear_watcher ? clearWatcher() : false;
      return { ...r, watcherCleared };
    }
    case "poc_decide": {
      const decision = args.decision === "deny" ? "deny" : "approve";
      return post(
        `/asks/${encodeURIComponent(String(args.ask_id ?? ""))}/${decision}`,
      );
    }
    default:
      throw new Error(`Unknown tool ${name}`);
  }
}

function send(msg) {
  process.stdout.write(`${JSON.stringify(msg)}\n`);
}

async function handle(msg) {
  if (msg.id === undefined || msg.id === null) return;
  const ok = (result) => send({ jsonrpc: "2.0", id: msg.id, result });
  switch (msg.method) {
    case "initialize": {
      const asked = msg.params?.protocolVersion;
      return ok({
        protocolVersion: SUPPORTED_VERSIONS.includes(asked)
          ? asked
          : "2025-06-18",
        capabilities: { tools: { listChanged: false } },
        serverInfo: { name: "ask-poc-control", version: "0.1.0" },
      });
    }
    case "ping":
      return ok({});
    case "tools/list":
      return ok({ tools });
    case "tools/call":
      try {
        const out = await callTool(
          msg.params?.name,
          msg.params?.arguments ?? {},
        );
        return ok({
          content: [{ type: "text", text: JSON.stringify(out) }],
          structuredContent: out,
        });
      } catch (err) {
        return ok({
          content: [{ type: "text", text: String(err?.message ?? err) }],
          isError: true,
        });
      }
    default:
      return send({
        jsonrpc: "2.0",
        id: msg.id,
        error: { code: -32601, message: `Method not found: ${msg.method}` },
      });
  }
}

const rl = readline.createInterface({ input: process.stdin });
rl.on("line", (line) => {
  if (!line.trim()) return;
  let msg;
  try {
    msg = JSON.parse(line);
  } catch {
    return send({
      jsonrpc: "2.0",
      id: null,
      error: { code: -32700, message: "Parse error" },
    });
  }
  for (const m of Array.isArray(msg) ? msg : [msg]) {
    handle(m).catch((err) => process.stderr.write(`ask-poc-control: ${err}\n`));
  }
});
