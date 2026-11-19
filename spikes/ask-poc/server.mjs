// Ask POC: two MCP servers that share in-memory state.
//
//   external-slack (port 4701): send_message, a mock of posting to the
//     customer-facing Slack workspace. Gated by an "ask" policy: a call with
//     no approved ask is rejected with instructions to request approval.
//   approvals (port 4702): request_approval, which files an ask, "notifies"
//     approvers (logged only) and returns a status URL plus a watcher script
//     for Claude Code's Monitor tool. The ask is approved automatically after
//     ASK_POC_AUTO_APPROVE_SECONDS, so the agent should retry on its own.
//
// No dependencies: MCP streamable HTTP with plain JSON responses.

import crypto from "node:crypto";
import fs from "node:fs";
import http from "node:http";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const DIR = path.dirname(fileURLToPath(import.meta.url));
const HOST = "127.0.0.1";
// Interface to listen on. Set 0.0.0.0 only when a container must reach the servers.
const BIND = process.env.ASK_POC_BIND ?? HOST;
const SLACK_PORT = Number(process.env.ASK_POC_SLACK_PORT ?? 4701);
const APPROVALS_PORT = Number(process.env.ASK_POC_APPROVALS_PORT ?? 4702);
const AUTO_APPROVE_SECONDS = Number(
  process.env.ASK_POC_AUTO_APPROVE_SECONDS ?? 10,
);
const ASK_TTL_SECONDS = Number(process.env.ASK_POC_TTL_SECONDS ?? 1800);
// "tool": blocked calls return a tool result with isError (what the model reads most reliably).
// "jsonrpc": blocked calls return JSON-RPC error -32003, like the Speakeasy gateway does today.
const ERROR_STYLE = process.env.ASK_POC_ERROR_STYLE ?? "tool";
const APPROVAL_CHANNEL = "#fde-approvals";
const WATCHER_SOURCE = fs.readFileSync(
  path.join(DIR, "ask-watch-v1.sh"),
  "utf8",
);
// Where agents are told to save the watcher. Override for agents that run
// elsewhere, such as the OpenClaw container.
const WATCHER_PATH =
  process.env.ASK_POC_WATCHER_PATH ??
  path.join(os.homedir(), ".claude", "ask-poc", "ask-watch-v1.sh");
const SUPPORTED_VERSIONS = ["2025-03-26", "2025-06-18", "2025-11-25"];
const FALLBACK_VERSION = "2025-06-18";

const state = { blocked: new Map(), asks: new Map(), sent: [], callSeq: 0 };

function log(event, fields = {}) {
  console.log(
    JSON.stringify({ t: new Date().toISOString(), event, ...fields }),
  );
}

function shortId(prefix) {
  return `${prefix}_${crypto.randomBytes(3).toString("hex")}`;
}

function normalizeArgs(args) {
  return {
    channel: String(args?.channel ?? "").trim(),
    text: String(args?.text ?? "").trim(),
  };
}

function fingerprint(args) {
  return crypto
    .createHash("sha256")
    .update(JSON.stringify(normalizeArgs(args)))
    .digest("hex")
    .slice(0, 16);
}

function text(body) {
  return { content: [{ type: "text", text: body }] };
}

function toolError(body) {
  return { content: [{ type: "text", text: body }], isError: true };
}

function blockedResult(body) {
  if (ERROR_STYLE === "jsonrpc")
    return { rpcError: { code: -32003, message: body } };
  return toolError(body);
}

// The status URL uses the host the agent reached us on, so an agent in a
// container gets host.docker.internal rather than its own 127.0.0.1.
function statusUrl(ask) {
  const base = ask.statusBase ?? `http://${HOST}:${APPROVALS_PORT}`;
  return `${base}/asks/${ask.id}`;
}

function asksFor(fp) {
  return [...state.asks.values()].filter((a) => a.fingerprint === fp);
}

// ---------- external-slack ----------

const slackTools = [
  {
    name: "send_message",
    description:
      "Send a message to a channel in the customer-facing (external) Slack workspace.",
    inputSchema: {
      type: "object",
      properties: {
        channel: {
          type: "string",
          description: "Channel name, for example #ext-support",
        },
        text: { type: "string", description: "Message text" },
      },
      required: ["channel", "text"],
    },
  },
];

function sendMessage(rawArgs) {
  const args = normalizeArgs(rawArgs);
  if (!args.channel || !args.text)
    return toolError("send_message needs both channel and text.");
  const fp = fingerprint(args);
  const asks = asksFor(fp);

  const approved = asks.find((a) => a.status === "approved" && !a.consumedAt);
  if (approved) {
    approved.consumedAt = new Date().toISOString();
    const ts = `${Math.floor(Date.now() / 1000)}.${crypto.randomBytes(3).toString("hex")}`;
    state.sent.push({
      ts,
      ...args,
      askId: approved.id,
      at: approved.consumedAt,
    });
    log("slack.sent", {
      ask: approved.id,
      channel: args.channel,
      text: args.text,
      ts,
    });
    return text(
      `Message sent to ${args.channel} in the external Slack workspace (mock). ts=${ts}. Allowed by approved ask ${approved.id}.`,
    );
  }

  const pending = asks.find((a) => a.status === "pending");
  if (pending) {
    log("slack.blocked_pending", { ask: pending.id, channel: args.channel });
    return blockedResult(
      `Still waiting for approval: ask ${pending.id} is pending. Do not call request_approval again. ` +
        `Keep your Monitor watch on ${statusUrl(pending)} running and retry this exact call once it reports APPROVED.`,
    );
  }

  const callId = `call_${++state.callSeq}`;
  state.blocked.set(callId, {
    id: callId,
    args,
    fingerprint: fp,
    at: new Date().toISOString(),
  });
  log("slack.blocked", {
    call: callId,
    channel: args.channel,
    text: args.text,
    errorStyle: ERROR_STYLE,
  });
  return blockedResult(
    [
      `Blocked by Ask policy "External Slack posts" (blocked call ${callId}).`,
      `Posting to ${args.channel} in the external Slack workspace needs approval from the FDE team.`,
      "",
      `To continue: call request_approval on the approvals MCP server with blocked_call_id "${callId}" and a one-sentence reason.`,
      "Then follow the instructions it returns to wait for the decision, and retry this exact send_message call (same channel and text) once it is approved.",
    ].join("\n"),
  );
}

function callSlackTool(name, args) {
  if (name === "send_message") return sendMessage(args);
  return toolError(`Unknown tool ${name}`);
}

// ---------- approvals ----------

const approvalTools = [
  {
    name: "request_approval",
    description:
      "Ask the approvers to allow a tool call that an Ask policy blocked. Returns how to wait for the decision without polling.",
    inputSchema: {
      type: "object",
      properties: {
        blocked_call_id: {
          type: "string",
          description: "The blocked call id from the error, for example call_1",
        },
        reason: {
          type: "string",
          description: "One sentence on why the call is needed",
        },
      },
      required: ["blocked_call_id", "reason"],
    },
  },
];

function decide(id, status, by) {
  const ask = state.asks.get(id);
  if (!ask || ask.status !== "pending") return false;
  ask.status = status;
  ask.decidedAt = new Date().toISOString();
  ask.decidedBy = by;
  clearTimeout(ask.approveTimer);
  clearTimeout(ask.expireTimer);
  log(`ask.${status}`, { ask: id, by });
  log("slackbot.update", {
    channel: APPROVAL_CHANNEL,
    text: `Ask ${id} ${status} by ${by}.`,
  });
  return true;
}

function waitInstructions(ask, reused) {
  const url = statusUrl(ask);
  const lines = [
    reused
      ? `An approval request already exists for this call: ${ask.id} (status: ${ask.status}).`
      : `Approval requested: ${ask.id}, for send_message to ${ask.args.channel} (blocked call ${ask.blockedCallId}).`,
    `Approvers in ${APPROVAL_CHANNEL} have been notified. Do not retry send_message until the ask is approved.`,
    "",
    `Status URL (plain text: pending, approved, denied or expired): ${url}`,
    "",
    "How to wait. Do not poll by hand and do not sleep:",
    `1. If ${WATCHER_PATH} does not exist, write this script to that path verbatim:`,
    "```bash",
    WATCHER_SOURCE.trimEnd(),
    "```",
    "2. Start the Monitor tool with:",
    `   command: bash ${WATCHER_PATH} ${url}`,
    `   description: Waiting for approval of ${ask.id}`,
    "   timeout_ms: 3600000",
    "   The watcher prints one line per status change and exits when the ask is decided.",
    "3. When it prints ASK APPROVED, retry send_message with exactly the same channel and text.",
    "   If it prints ASK DENIED or ASK EXPIRED, do not retry; tell the user.",
    "You can keep working on other things while you wait.",
  ];
  return lines.join("\n");
}

function requestApproval(args, reqHost) {
  const callId = String(args?.blocked_call_id ?? "").trim();
  const blocked = state.blocked.get(callId);
  if (!blocked) {
    const known = [...state.blocked.keys()].join(", ") || "none";
    return toolError(
      `Unknown blocked_call_id "${callId}". Known blocked calls: ${known}.`,
    );
  }
  const open = asksFor(blocked.fingerprint).find(
    (a) => a.status === "pending" || (a.status === "approved" && !a.consumedAt),
  );
  if (open) {
    log("ask.reused", { ask: open.id, call: callId });
    return text(waitInstructions(open, true));
  }

  const ask = {
    id: shortId("ask"),
    blockedCallId: callId,
    tool: "send_message",
    server: "external-slack",
    args: blocked.args,
    fingerprint: blocked.fingerprint,
    reason: String(args?.reason ?? "").trim(),
    status: "pending",
    createdAt: new Date().toISOString(),
    statusBase: reqHost ? `http://${reqHost}` : undefined,
  };
  state.asks.set(ask.id, ask);
  log("ask.created", { ask: ask.id, call: callId, reason: ask.reason });
  log("slackbot.notify", {
    channel: APPROVAL_CHANNEL,
    text: `Agent wants to post to ${ask.args.channel}: "${ask.args.text}". Reason: ${ask.reason || "none"}. Approve or deny ${ask.id}.`,
  });
  if (AUTO_APPROVE_SECONDS > 0) {
    ask.approveTimer = setTimeout(
      () => decide(ask.id, "approved", "auto-approver"),
      AUTO_APPROVE_SECONDS * 1000,
    );
    log("ask.auto_approve_scheduled", {
      ask: ask.id,
      seconds: AUTO_APPROVE_SECONDS,
    });
  }
  ask.expireTimer = setTimeout(
    () => decide(ask.id, "expired", "ttl"),
    ASK_TTL_SECONDS * 1000,
  );
  return text(waitInstructions(ask, false));
}

function callApprovalTool(name, args, reqHost) {
  if (name === "request_approval") return requestApproval(args, reqHost);
  return toolError(`Unknown tool ${name}`);
}

// ---------- MCP over HTTP ----------

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    req.on("data", (c) => chunks.push(c));
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    req.on("error", reject);
  });
}

function sendJson(res, status, body) {
  res.writeHead(status, { "Content-Type": "application/json" });
  res.end(JSON.stringify(body));
}

function sendText(res, status, body) {
  res.writeHead(status, { "Content-Type": "text/plain; charset=utf-8" });
  res.end(body);
}

function handleRpc(serverName, tools, callTool, msg, reqHost) {
  if (msg?.method && msg.method !== "ping")
    log("mcp.request", {
      server: serverName,
      method: msg.method,
      id: msg.id,
      tool: msg.params?.name,
    });
  if (msg?.id === undefined || msg?.id === null) return null;
  const ok = (result) => ({ jsonrpc: "2.0", id: msg.id, result });
  const fail = (code, message) => ({
    jsonrpc: "2.0",
    id: msg.id,
    error: { code, message },
  });

  switch (msg.method) {
    case "initialize": {
      const asked = msg.params?.protocolVersion;
      const protocolVersion = SUPPORTED_VERSIONS.includes(asked)
        ? asked
        : FALLBACK_VERSION;
      log("mcp.initialize", {
        server: serverName,
        asked,
        answered: protocolVersion,
        client: msg.params?.clientInfo,
      });
      return ok({
        protocolVersion,
        capabilities: { tools: { listChanged: false } },
        serverInfo: { name: serverName, version: "0.1.0" },
      });
    }
    case "ping":
      return ok({});
    case "tools/list":
      return ok({ tools });
    case "tools/call": {
      const out = callTool(
        msg.params?.name,
        msg.params?.arguments ?? {},
        reqHost,
      );
      if (out.rpcError)
        return { jsonrpc: "2.0", id: msg.id, error: out.rpcError };
      return ok(out);
    }
    default:
      return fail(-32601, `Method not found: ${msg.method}`);
  }
}

function mcpServer(serverName, port, tools, callTool, extraRoutes) {
  const server = http.createServer(async (req, res) => {
    const url = new URL(req.url, `http://${req.headers.host ?? HOST}`);
    try {
      if (url.pathname !== "/mcp") {
        if (extraRoutes && (await extraRoutes(req, res, url))) return;
        return sendText(res, 404, "not found\n");
      }
      if (req.method === "GET") {
        res.writeHead(405, { Allow: "POST, DELETE" });
        return res.end();
      }
      if (req.method === "DELETE") {
        res.writeHead(200);
        return res.end();
      }
      if (req.method !== "POST") {
        res.writeHead(405, { Allow: "POST, DELETE" });
        return res.end();
      }
      let parsed;
      try {
        parsed = JSON.parse(await readBody(req));
      } catch {
        return sendJson(res, 400, {
          jsonrpc: "2.0",
          id: null,
          error: { code: -32700, message: "Parse error" },
        });
      }
      const batch = Array.isArray(parsed) ? parsed : [parsed];
      const replies = batch
        .map((m) => handleRpc(serverName, tools, callTool, m, req.headers.host))
        .filter(Boolean);
      if (replies.length === 0) {
        res.writeHead(202);
        return res.end();
      }
      return sendJson(res, 200, Array.isArray(parsed) ? replies : replies[0]);
    } catch (err) {
      log("http.error", { server: serverName, error: String(err) });
      return sendText(res, 500, "internal error\n");
    }
  });
  server.listen(port, BIND, () =>
    log("listening", { server: serverName, url: `http://${HOST}:${port}/mcp` }),
  );
  return server;
}

function publicAsk(a) {
  const { approveTimer, expireTimer, ...rest } = a;
  return rest;
}

async function approvalRoutes(req, res, url) {
  const m = url.pathname.match(
    /^\/asks\/([A-Za-z0-9_]+)(?:\/(approve|deny))?$/,
  );
  if (m) {
    const ask = state.asks.get(m[1]);
    if (!ask) {
      sendText(res, 404, "unknown ask\n");
      return true;
    }
    if (!m[2] && req.method === "GET") {
      sendText(res, 200, ask.status);
      return true;
    }
    if (m[2] && req.method === "POST") {
      const changed = decide(
        ask.id,
        m[2] === "approve" ? "approved" : "denied",
        "manual",
      );
      sendText(
        res,
        changed ? 200 : 409,
        changed
          ? `${ask.id} ${ask.status}\n`
          : `${ask.id} is already ${ask.status}\n`,
      );
      return true;
    }
  }
  if (url.pathname === "/asks" && req.method === "GET") {
    sendJson(res, 200, [...state.asks.values()].map(publicAsk));
    return true;
  }
  if (
    (url.pathname === "/" || url.pathname === "/health") &&
    req.method === "GET"
  ) {
    sendJson(res, 200, {
      ok: true,
      autoApproveSeconds: AUTO_APPROVE_SECONDS,
      errorStyle: ERROR_STYLE,
      blockedCalls: [...state.blocked.values()],
      asks: [...state.asks.values()].map(publicAsk),
      sent: state.sent,
    });
    return true;
  }
  if (url.pathname === "/reset" && req.method === "POST") {
    for (const a of state.asks.values()) {
      clearTimeout(a.approveTimer);
      clearTimeout(a.expireTimer);
    }
    state.blocked.clear();
    state.asks.clear();
    state.sent.length = 0;
    state.callSeq = 0;
    log("state.reset");
    sendText(res, 200, "reset\n");
    return true;
  }
  return false;
}

async function slackRoutes(req, res, url) {
  if (
    (url.pathname === "/" ||
      url.pathname === "/health" ||
      url.pathname === "/sent") &&
    req.method === "GET"
  ) {
    sendJson(res, 200, { ok: true, sent: state.sent });
    return true;
  }
  return false;
}

mcpServer("external-slack", SLACK_PORT, slackTools, callSlackTool, slackRoutes);
mcpServer(
  "approvals",
  APPROVALS_PORT,
  approvalTools,
  callApprovalTool,
  approvalRoutes,
);
log("config", {
  autoApproveSeconds: AUTO_APPROVE_SECONDS,
  ttlSeconds: ASK_TTL_SECONDS,
  errorStyle: ERROR_STYLE,
  watcherPath: WATCHER_PATH,
});
