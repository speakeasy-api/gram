#!/usr/bin/env -S node --import tsx
//MISE description="Run provider hook E2E checks against a local Gram server"
//MISE dir="{{ config_root }}"
//USAGE flag "--project <slug>" default="default" help="Project slug to test against."
//USAGE flag "--providers <list>" default="claude,cursor,codex" help="Comma-separated providers to drive: claude,cursor,codex."
//USAGE flag "--suites <list>" default="capture,shadow-mcp,ratchet" help="Comma-separated feature suites to run: capture,shadow-mcp,ratchet."
//USAGE flag "--timeout-seconds <seconds>" default="180" help="Timeout per provider scenario."
//USAGE flag "--poll-seconds <seconds>" default="90" help="How long to poll Gram telemetry and database evidence."
//USAGE flag "--keep-artifacts" help="Keep the temp workspace and built plugin artifacts."
//USAGE flag "--skip-build" help="Skip building plugins; use dirs supplied through GRAM_HOOKS_E2E_<PROVIDER>_PLUGIN_DIR."
import crypto from "node:crypto";
import fs from "node:fs/promises";
import http from "node:http";
import os from "node:os";
import path from "node:path";
import { spawn } from "node:child_process";
import { intro, log, outro } from "@clack/prompts";
import { GramCore } from "#gram/client/core.js";
import { authInfo } from "#gram/client/funcs/authInfo.js";
import { keysCreate } from "#gram/client/funcs/keysCreate.js";
const VALID_PROVIDERS = new Set(["claude", "cursor", "codex"]);
const VALID_SUITES = new Set(["capture", "shadow-mcp", "ratchet"]);
const SOURCE_ALIASES = {
  claude: ["claude", "claude-code"],
  cursor: ["cursor"],
  codex: ["codex"],
};
function parseArgs(argv) {
  const args = {};
  for (let i = 0; i < argv.length; i++) {
    const token = argv[i];
    if (!token.startsWith("--")) {
      throw new Error(`Unexpected positional argument: ${token}`);
    }
    const eq = token.indexOf("=");
    if (eq >= 0) {
      args[token.slice(2, eq)] = token.slice(eq + 1);
      continue;
    }
    const key = token.slice(2);
    const next = argv[i + 1];
    if (!next || next.startsWith("--")) {
      args[key] = true;
      continue;
    }
    args[key] = next;
    i++;
  }
  const providers = String(args.providers ?? "claude,cursor,codex")
    .split(",")
    .map((p) => p.trim())
    .filter(Boolean);
  const suites = String(args.suites ?? "capture,shadow-mcp,ratchet")
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
  for (const p of providers) {
    if (!VALID_PROVIDERS.has(p)) {
      throw new Error(`Unsupported provider "${p}". Use claude,cursor,codex.`);
    }
  }
  for (const s of suites) {
    if (!VALID_SUITES.has(s)) {
      throw new Error(`Unsupported suite "${s}". Use capture,shadow-mcp.`);
    }
  }
  return {
    project: String(args.project ?? "default"),
    providers: providers,
    suites,
    timeoutSeconds: Number(args["timeout-seconds"] ?? 180),
    pollSeconds: Number(args["poll-seconds"] ?? 90),
    keepArtifacts: Boolean(args["keep-artifacts"]),
    skipBuild: Boolean(args["skip-build"] ?? args["skip-download"]),
  };
}
function fail(message) {
  throw new Error(message);
}
function requireEnv(name) {
  const value = process.env[name];
  if (!value) {
    fail(`${name} is not set`);
  }
  return value;
}
async function authenticateViaDevIDP(serverURL) {
  const loginRes = await fetchOrFail(
    `${serverURL}/rpc/auth.login`,
    {
      redirect: "manual",
    },
    `connect to Gram server at ${serverURL}`,
  );
  const authorizeURL = loginRes.headers.get("location");
  if (!authorizeURL) {
    fail("auth.login did not return a redirect");
  }
  const nonceCookie = loginRes.headers
    .getSetCookie()
    .find((c) => c.startsWith("gram_auth_nonce="));
  if (!nonceCookie) {
    fail("auth.login did not set gram_auth_nonce");
  }
  const authorizeRes = await fetchOrFail(
    authorizeURL,
    { redirect: "manual" },
    "authorize through dev-idp",
  );
  const callbackLocation = authorizeRes.headers.get("location");
  if (!callbackLocation) {
    fail("dev-idp authorize did not return a callback redirect");
  }
  const callbackRes = await fetchOrFail(
    callbackLocation,
    {
      redirect: "manual",
      headers: { cookie: nonceCookie.split(";")[0] },
    },
    "complete dev-idp callback",
  );
  const sessionToken = callbackRes.headers.get("gram-session");
  if (!sessionToken) {
    fail(
      `auth.callback did not return gram-session (status=${callbackRes.status})`,
    );
  }
  return sessionToken;
}
async function fetchOrFail(input, init, label) {
  try {
    return await fetch(input, init);
  } catch (err) {
    const detail = err instanceof Error ? err.message : String(err);
    fail(`Failed to ${label}: ${detail}`);
  }
}
async function getSessionInfo(serverURL, projectSlug) {
  const gram = new GramCore({ serverURL });
  const sessionId = await authenticateViaDevIDP(serverURL);
  const res = await authInfo(gram, undefined, {
    sessionHeaderGramSession: sessionId,
  });
  if (!res.ok) {
    fail(`authInfo failed: ${JSON.stringify(res.error)}`);
  }
  const session = res.value.result;
  const organizationId = session.activeOrganizationId;
  if (!organizationId) {
    fail("No active organization on dev session");
  }
  const org = session.organizations.find((o) => o.id === organizationId);
  if (!org) {
    fail(`Active organization ${organizationId} not found in authInfo`);
  }
  const project = org.projects.find((p) => p.slug === projectSlug);
  if (!project) {
    fail(
      `Project ${projectSlug} not found in active organization ${organizationId}`,
    );
  }
  return {
    sessionId,
    organizationId,
    organizationSlug: org.slug,
    projectId: project.id,
    userEmail: session.userEmail,
  };
}
// provisionHooksAuth mints a hooks-scoped API key and writes it to a cache
// file in the run's temp dir. Every provider spawn points GRAM_HOOKS_AUTH_FILE
// at it so runs never depend on ambient credentials from the developer's
// ~/.config/gram/hooks-auth.env — that ambient fallback is exactly how an
// unauthenticatable plugin once passed E2E locally.
async function provisionHooksAuth(serverURL, session, projectSlug, rootDir) {
  const gram = new GramCore({ serverURL });
  const keyRes = await keysCreate(
    gram,
    {
      createKeyForm: { name: `hooks-e2e-${Date.now()}`, scopes: ["hooks"] },
    },
    { sessionHeaderGramSession: session.sessionId },
  );
  if (!keyRes.ok) {
    fail(`keys.create failed: ${JSON.stringify(keyRes.error)}`);
  }
  const authFile = path.join(rootDir, "hooks-auth.env");
  await fs.writeFile(
    authFile,
    [
      `server_url=${serverURL}`,
      `api_key=${keyRes.value.key}`,
      `project=${projectSlug}`,
      `email=${session.userEmail}`,
      "",
    ].join("\n"),
    { mode: 0o600 },
  );
  return authFile;
}
function psqlArgs(sql) {
  const databaseURL = process.env.GRAM_DATABASE_URL;
  if (!databaseURL) {
    fail("GRAM_DATABASE_URL is not set");
  }
  return [
    databaseURL.replace(/&search_path=.*$/, ""),
    "-tA",
    "-F",
    "\x1f",
    "-v",
    "ON_ERROR_STOP=1",
    "-c",
    sql,
  ];
}
async function runProcess(command, args, opts = {}) {
  return await new Promise((resolve) => {
    const child = spawn(command, args, {
      cwd: opts.cwd,
      env: { ...process.env, ...opts.env },
      stdio: ["pipe", "pipe", "pipe"],
    });
    let stdout = "";
    let stderr = "";
    let timedOut = false;
    const timer = opts.timeoutMs
      ? setTimeout(() => {
          timedOut = true;
          child.kill("SIGTERM");
          setTimeout(() => child.kill("SIGKILL"), 5000).unref();
        }, opts.timeoutMs)
      : undefined;
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk) => {
      stdout += chunk;
    });
    child.stderr.on("data", (chunk) => {
      stderr += chunk;
    });
    let settled = false;
    const finish = (result) => {
      if (settled) {
        return;
      }
      settled = true;
      if (timer) {
        clearTimeout(timer);
      }
      resolve(result);
    };
    child.on("error", (err) => {
      finish({
        provider: "claude",
        command: [command, ...args].join(" "),
        exitCode: null,
        signal: null,
        stdout,
        stderr: stderr || String(err),
        timedOut,
      });
    });
    child.on("close", (exitCode, signal) => {
      finish({
        provider: "claude",
        command: [command, ...args].join(" "),
        exitCode,
        signal,
        stdout,
        stderr,
        timedOut,
      });
    });
    child.stdin.on("error", () => {});
    if (opts.input) {
      child.stdin.write(opts.input);
    }
    child.stdin.end();
  });
}
// session_capture gates hook ingest; logs gates telemetry_logs writes — the
// evidence checks read both, so provision both.
async function enableSessionCapture(organizationId) {
  const sql = `
    INSERT INTO organization_features (organization_id, feature_name)
    VALUES
      ('${sqlString(organizationId)}', 'session_capture'),
      ('${sqlString(organizationId)}', 'logs')
    ON CONFLICT (organization_id, feature_name) WHERE deleted IS FALSE DO NOTHING;
  `;
  const res = await runProcess("psql", psqlArgs(sql));
  if (res.exitCode !== 0) {
    fail(`failed to enable session_capture:\n${res.stderr || res.stdout}`);
  }
}
function sqlString(value) {
  return value.replaceAll("'", "''");
}
// buildHookBinary compiles the speakeasy-hooks binary once per run. Every
// provider plugin drives this one binary; there is no server-side plugin
// download anymore. The binary must live outside the workspace tree on a
// plain temp path: Cursor refuses to execute hook binaries from some
// locations.
let hookBinaryPath = null;
async function buildHookBinary(artifactsDir) {
  if (hookBinaryPath) {
    return hookBinaryPath;
  }
  const binary = path.join(artifactsDir, "bin", "speakeasy-hooks");
  await fs.mkdir(path.dirname(binary), { recursive: true });
  await runChecked("go", [
    "build",
    "-o",
    binary,
    "./hooks/cmd/speakeasy-hooks",
  ]);
  hookBinaryPath = binary;
  return binary;
}
async function buildProviderPlugin(args) {
  // Codex has no plugin layout for hooks: the config installs directly into
  // the isolated Codex home (hooks.json next to config.toml).
  const pluginDir =
    args.provider === "codex"
      ? args.codexEnv.CODEX_HOME
      : path.join(args.artifactsDir, "plugins", args.provider);
  if (
    args.provider !== "codex" &&
    process.env[`GRAM_HOOKS_E2E_${args.provider.toUpperCase()}_PLUGIN_DIR`]
  ) {
    return process.env[
      `GRAM_HOOKS_E2E_${args.provider.toUpperCase()}_PLUGIN_DIR`
    ];
  }
  const binary = await buildHookBinary(args.artifactsDir);
  await runChecked(binary, [
    "install",
    `--provider=${args.provider}`,
    `--dir=${pluginDir}`,
    `--server-url=${args.serverURL}`,
    `--project=${args.projectSlug}`,
    `--binary=${binary}`,
  ]);
  return pluginDir;
}
async function runChecked(command, args, opts = {}) {
  const res = await runProcess(command, args, opts);
  if (res.exitCode !== 0) {
    fail(`${res.command} failed:\n${res.stderr || res.stdout}`);
  }
  return res;
}
async function prepareCodexEnv(rootDir) {
  const home = path.join(rootDir, "codex-home");
  const codexDir = path.join(home, ".codex");
  await fs.mkdir(codexDir, { recursive: true });
  const sourceDir = path.join(os.homedir(), ".codex");
  const entries = [
    "auth.json",
    "installation_id",
    "version.json",
    "models_cache.json",
    ".codex-global-state.json",
    "rules",
  ];
  for (const entry of entries) {
    const src = path.join(sourceDir, entry);
    const dst = path.join(codexDir, entry);
    try {
      await fs.cp(src, dst, { recursive: true });
    } catch (err) {
      if (
        !(
          err &&
          typeof err === "object" &&
          "code" in err &&
          err.code === "ENOENT"
        )
      ) {
        throw err;
      }
    }
  }
  await writeIsolatedCodexConfig(home);
  return {
    HOME: home,
    CODEX_HOME: codexDir,
  };
}
async function writeIsolatedCodexConfig(home) {
  const configPath = path.join(home, ".codex", "config.toml");
  await fs.mkdir(path.dirname(configPath), { recursive: true });
  await fs.writeFile(
    configPath,
    [
      'model = "gpt-5.5"',
      'model_reasoning_effort = "high"',
      "",
      "[features]",
      "hooks = true",
      "plugin_hooks = true",
      "",
      "[hooks.state]",
      "",
    ].join("\n"),
  );
}
async function prepareShadowMCPFixture(rootDir, runId) {
  const fixtureDir = path.join(rootDir, "shadow-mcp");
  const scriptPath = path.join(fixtureDir, "server.mjs");
  await fs.mkdir(fixtureDir, { recursive: true });
  await fs.writeFile(
    scriptPath,
    `#!/usr/bin/env node
const tools = [
  {
    name: "shadow_lookup",
    description: "Return the Gram hooks E2E marker for a Shadow MCP check.",
    inputSchema: {
      type: "object",
      properties: {
        marker: { type: "string" }
      },
      required: ["marker"]
    }
  }
];

let buffer = "";

process.stdin.on("data", (chunk) => {
  buffer += chunk.toString("utf8");
  drain();
});

function drain() {
  while (true) {
    if (/^content-length:/i.test(buffer)) {
      const headerEnd = buffer.indexOf("\\r\\n\\r\\n");
      if (headerEnd < 0) return;
      const header = buffer.slice(0, headerEnd);
      const match = header.match(/content-length:\\s*(\\d+)/i);
      if (!match) {
        buffer = buffer.slice(headerEnd + 4);
        continue;
      }
      const length = Number(match[1]);
      const messageStart = headerEnd + 4;
      const messageEnd = messageStart + length;
      if (Buffer.byteLength(buffer.slice(messageStart), "utf8") < length) return;
      const raw = buffer.slice(messageStart, messageEnd);
      buffer = buffer.slice(messageEnd);
      handle(JSON.parse(raw));
      continue;
    }
    const newline = buffer.indexOf("\\n");
    if (newline < 0) return;
    const raw = buffer.slice(0, newline).trim();
    buffer = buffer.slice(newline + 1);
    if (raw) handle(JSON.parse(raw));
  }
}

function send(message) {
  process.stdout.write(JSON.stringify(message) + "\\n");
}

function handle(message) {
  if (message.id === undefined || message.id === null) return;
  if (message.method === "initialize") {
    send({
      jsonrpc: "2.0",
      id: message.id,
      result: {
        protocolVersion: "2024-11-05",
        capabilities: { tools: {} },
        serverInfo: { name: "gram-hooks-e2e-shadow", version: "1.0.0" }
      }
    });
    return;
  }
  if (message.method === "tools/list") {
    send({ jsonrpc: "2.0", id: message.id, result: { tools } });
    return;
  }
  if (message.method === "tools/call") {
    const marker = String(message.params?.arguments?.marker ?? "${runId}");
    send({
      jsonrpc: "2.0",
      id: message.id,
      result: {
        content: [{ type: "text", text: \`GRAM_HOOKS_E2E_MCP_TOOL_OK \${marker}\` }]
      }
    });
    return;
  }
  send({ jsonrpc: "2.0", id: message.id, error: { code: -32601, message: "method not found" } });
}
`,
  );
  await fs.chmod(scriptPath, 0o755);
  return {
    scriptPath,
    shadowServerName: "shadowe2e",
    gramServerName: "grame2e",
    gramHostedURL: "",
  };
}
async function startHostedMCPHTTPFixture(runId) {
  const server = http.createServer(async (req, res) => {
    if (req.method === "GET") {
      res.writeHead(405, { Allow: "POST, DELETE" });
      res.end();
      return;
    }
    if (req.method === "DELETE") {
      res.writeHead(202);
      res.end();
      return;
    }
    if (req.method !== "POST") {
      res.writeHead(405, { Allow: "POST, DELETE" });
      res.end();
      return;
    }
    const chunks = [];
    try {
      for await (const chunk of req) {
        chunks.push(chunk);
      }
    } catch {
      res.destroy();
      return;
    }
    const body = Buffer.concat(chunks).toString("utf8");
    let message;
    try {
      message = JSON.parse(body);
    } catch {
      res.writeHead(400, { "Content-Type": "application/json" });
      res.end(
        JSON.stringify({
          jsonrpc: "2.0",
          error: { code: -32700, message: "parse error" },
          id: null,
        }),
      );
      return;
    }
    const handle = (msg) => {
      if (msg.id === undefined || msg.id === null) {
        return null;
      }
      if (msg.method === "initialize") {
        return {
          jsonrpc: "2.0",
          id: msg.id,
          result: {
            protocolVersion: "2025-06-18",
            capabilities: { tools: {} },
            serverInfo: { name: "gram-hooks-e2e-hosted", version: "1.0.0" },
          },
        };
      }
      if (msg.method === "tools/list") {
        return {
          jsonrpc: "2.0",
          id: msg.id,
          result: {
            tools: [
              {
                name: "shadow_lookup",
                description:
                  "Return the Gram hooks E2E marker for a hosted Shadow MCP check.",
                inputSchema: {
                  type: "object",
                  properties: { marker: { type: "string" } },
                  required: ["marker"],
                },
              },
            ],
          },
        };
      }
      if (msg.method === "tools/call") {
        const marker = String(msg.params?.arguments?.marker ?? runId);
        return {
          jsonrpc: "2.0",
          id: msg.id,
          result: {
            content: [
              { type: "text", text: `GRAM_HOOKS_E2E_MCP_TOOL_OK ${marker}` },
            ],
            isError: false,
          },
        };
      }
      return {
        jsonrpc: "2.0",
        id: msg.id,
        error: { code: -32601, message: "method not found" },
      };
    };
    const response = Array.isArray(message)
      ? message.map(handle).filter(Boolean)
      : handle(message);
    if (!response) {
      res.writeHead(202);
      res.end();
      return;
    }
    res.writeHead(200, {
      "Content-Type": "application/json",
      "Mcp-Session-Id": `gram-hooks-e2e-${runId}`,
    });
    res.end(JSON.stringify(response));
  });
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      server.off("error", reject);
      resolve();
    });
  });
  const address = server.address();
  if (!address || typeof address === "string") {
    fail("hosted MCP fixture did not bind to a TCP port");
  }
  return {
    url: `http://127.0.0.1:${address.port}/mcp`,
    close: () => new Promise((resolve) => server.close(() => resolve())),
  };
}
async function rpcJSON(args) {
  const res = await fetchOrFail(
    `${args.serverURL}${args.path}`,
    {
      method: args.method ?? "POST",
      headers: {
        "Content-Type": "application/json",
        "Gram-Session": args.session.sessionId,
        "Gram-Project": args.projectSlug,
      },
      body: args.body === undefined ? undefined : JSON.stringify(args.body),
    },
    args.label,
  );
  if (!res.ok) {
    fail(`${args.label} failed: ${res.status} ${await res.text()}`);
  }
  if (res.status === 204) {
    return null;
  }
  return await res.json();
}
async function createHostedMCPFixture(args) {
  const endpointSuffix = `${Date.now().toString(36)}-${crypto.randomBytes(3).toString("hex")}`;
  const endpointSlug =
    `${args.session.organizationSlug}-hooks-e2e-${endpointSuffix}`.slice(
      0,
      128,
    );
  const remote = await rpcJSON({
    serverURL: args.serverURL,
    session: args.session,
    projectSlug: args.projectSlug,
    path: "/rpc/remoteMcp.createServer",
    body: {
      name: `Gram hooks E2E hosted ${args.runId}`,
      url: args.remoteURL,
      transport_type: "streamable-http",
      headers: [],
    },
    label: "create hosted remote MCP source",
  });
  const mcpServer = await rpcJSON({
    serverURL: args.serverURL,
    session: args.session,
    projectSlug: args.projectSlug,
    path: "/rpc/mcpServers.create",
    body: {
      name: `Gram hooks E2E hosted ${args.runId}`,
      remote_mcp_server_id: remote.id,
      visibility: "public",
    },
    label: "create hosted MCP server",
  });
  const endpoint = await rpcJSON({
    serverURL: args.serverURL,
    session: args.session,
    projectSlug: args.projectSlug,
    path: "/rpc/mcpEndpoints.create",
    body: {
      mcp_server_id: mcpServer.id,
      slug: endpointSlug,
    },
    label: "create hosted MCP endpoint",
  });
  return {
    remoteId: remote.id,
    mcpServerId: mcpServer.id,
    endpointId: endpoint.id,
    endpointSlug: endpoint.slug,
    url: `${args.serverURL}/mcp/${endpoint.slug}`,
  };
}
async function deleteHostedMCPFixture(args) {
  for (const resource of [
    {
      id: args.endpointId,
      path: "/rpc/mcpEndpoints.delete",
      label: "delete hosted MCP endpoint",
    },
    {
      id: args.mcpServerId,
      path: "/rpc/mcpServers.delete",
      label: "delete hosted MCP server",
    },
    {
      id: args.remoteId,
      path: "/rpc/remoteMcp.deleteServer",
      label: "delete hosted remote MCP source",
    },
  ]) {
    if (!resource.id) {
      continue;
    }
    const url = new URL(`${args.serverURL}${resource.path}`);
    url.searchParams.set("id", resource.id);
    const res = await fetchOrFail(
      url,
      {
        method: "DELETE",
        headers: {
          "Gram-Session": args.session.sessionId,
          "Gram-Project": args.projectSlug,
        },
      },
      resource.label,
    );
    if (!res.ok && res.status !== 404) {
      log.warn(`${resource.label} failed: ${res.status} ${await res.text()}`);
    }
  }
}
async function prepareShadowMCPProviderConfig(args) {
  const config = {
    mcpServers: {
      [args.fixture.shadowServerName]: {
        command: process.execPath,
        args: [args.fixture.scriptPath],
      },
      [args.fixture.gramServerName]: {
        type: "http",
        url: args.fixture.gramHostedURL,
      },
    },
  };
  if (args.provider === "claude") {
    const runtimeConfigPath = path.join(args.workdir, "shadow-mcp.claude.json");
    await fs.writeFile(runtimeConfigPath, JSON.stringify(config, null, 2));
    await fs.writeFile(
      path.join(args.workdir, ".mcp.json"),
      JSON.stringify(config, null, 2),
    );
    return { runtimeConfigPath };
  }
  if (args.provider === "cursor") {
    const cursorDir = path.join(args.workdir, ".cursor");
    await fs.mkdir(cursorDir, { recursive: true });
    await fs.writeFile(
      path.join(cursorDir, "mcp.json"),
      JSON.stringify(config, null, 2),
    );
    return {};
  }
  if (!args.env) {
    fail(
      "internal error: codex shadow MCP setup requires isolated Codex environment",
    );
  }
  await runProcess("codex", ["mcp", "remove", args.fixture.shadowServerName], {
    env: args.env,
    timeoutMs: 30_000,
  });
  await runProcess("codex", ["mcp", "remove", args.fixture.gramServerName], {
    env: args.env,
    timeoutMs: 30_000,
  });
  await runChecked(
    "codex",
    [
      "mcp",
      "add",
      args.fixture.shadowServerName,
      "--",
      process.execPath,
      args.fixture.scriptPath,
    ],
    {
      env: args.env,
      timeoutMs: 30_000,
    },
  );
  await runChecked(
    "codex",
    [
      "mcp",
      "add",
      args.fixture.gramServerName,
      "--url",
      args.fixture.gramHostedURL,
    ],
    {
      env: args.env,
      timeoutMs: 30_000,
    },
  );
  return {};
}
function providerPrompt(runId, provider, scenario, workdir) {
  if (scenario === "success") {
    return [
      `Gram hooks E2E run ${runId} for ${provider}.`,
      `Use your filesystem/tooling to read ${path.join(workdir, `input-${runId}.txt`)}.`,
      `Then reply with exactly: GRAM_HOOKS_E2E_OK ${runId} ${provider} success`,
    ].join(" ");
  }
  return [
    `Gram hooks E2E run ${runId} for ${provider}.`,
    `Use your filesystem/tooling to read the missing file ${path.join(workdir, `missing-${runId}.txt`)} so the tool call fails.`,
    `After the failed tool call, reply with exactly: GRAM_HOOKS_E2E_OK ${runId} ${provider} failure`,
  ].join(" ");
}
function shadowMCPPrompt(args) {
  const marker = `${args.runId} ${args.provider} ${args.variant}`;
  const serverName =
    args.variant === "gram-hosted"
      ? args.fixture.gramServerName
      : args.fixture.shadowServerName;
  return [
    `Gram hooks E2E Shadow MCP run ${args.runId} for ${args.provider}.`,
    `Use the MCP server named ${serverName} and call its shadow_lookup tool with marker "${marker}".`,
    `If the tool call succeeds, reply exactly: GRAM_HOOKS_E2E_OK ${marker}`,
    `If Gram blocks the tool call, reply exactly: GRAM_HOOKS_E2E_BLOCKED ${marker}`,
  ].join(" ");
}
async function runProviderScenario(args) {
  const prompt = providerPrompt(
    args.runId,
    args.provider,
    args.scenario,
    args.workdir,
  );
  if (args.provider === "claude") {
    const sessionId = crypto.randomUUID();
    const res = await runProcess(
      "claude",
      [
        "--setting-sources",
        "project,local",
        "--plugin-dir",
        args.pluginDir,
        "--permission-mode",
        "bypassPermissions",
        "--allowedTools",
        "Read,Bash",
        "--include-hook-events",
        "--verbose",
        "--output-format",
        "stream-json",
        "--session-id",
        sessionId,
        "-p",
        prompt,
      ],
      { cwd: args.workdir, env: args.env, timeoutMs: args.timeoutMs },
    );
    res.provider = args.provider;
    return res;
  }
  if (args.provider === "cursor") {
    await prepareCursorProjectHooks(args.pluginDir, args.workdir);
    const res = await runProcess(
      "cursor",
      [
        "agent",
        "--print",
        "--output-format",
        "stream-json",
        "--trust",
        "--force",
        "--approve-mcps",
        "--plugin-dir",
        args.pluginDir,
        "--workspace",
        args.workdir,
        prompt,
      ],
      { cwd: args.workdir, timeoutMs: args.timeoutMs },
    );
    res.provider = args.provider;
    return res;
  }
  const res = await runProcess(
    "codex",
    [
      "exec",
      "--json",
      "--cd",
      args.workdir,
      "--skip-git-repo-check",
      "--dangerously-bypass-hook-trust",
      "--dangerously-bypass-approvals-and-sandbox",
      prompt,
    ],
    { cwd: args.workdir, env: args.env, timeoutMs: args.timeoutMs },
  );
  res.provider = args.provider;
  return res;
}
async function runProviderShadowMCPScenario(args) {
  const prompt = shadowMCPPrompt(args);
  const serverName =
    args.variant === "gram-hosted"
      ? args.fixture.gramServerName
      : args.fixture.shadowServerName;
  const allowedTool = `mcp__${serverName}__shadow_lookup`;
  if (args.provider === "claude") {
    const sessionId = crypto.randomUUID();
    const claudeArgs = [
      "--setting-sources",
      "project,local",
      "--plugin-dir",
      args.pluginDir,
      "--mcp-config",
      args.runtimeConfigPath,
      "--permission-mode",
      "bypassPermissions",
      "--allowedTools",
      `Read,Bash,${allowedTool}`,
      "--include-hook-events",
      "--verbose",
      "--output-format",
      "stream-json",
      "--session-id",
      sessionId,
      "-p",
      prompt,
    ];
    const res = await runProcess("claude", claudeArgs, {
      cwd: args.workdir,
      timeoutMs: args.timeoutMs,
    });
    res.provider = args.provider;
    return res;
  }
  if (args.provider === "cursor") {
    await prepareCursorProjectHooks(args.pluginDir, args.workdir);
    const res = await runProcess(
      "cursor",
      [
        "agent",
        "--print",
        "--output-format",
        "stream-json",
        "--trust",
        "--force",
        "--approve-mcps",
        "--plugin-dir",
        args.pluginDir,
        "--workspace",
        args.workdir,
        prompt,
      ],
      { cwd: args.workdir, timeoutMs: args.timeoutMs },
    );
    res.provider = args.provider;
    return res;
  }
  const res = await runProcess(
    "codex",
    [
      "exec",
      "--json",
      "--cd",
      args.workdir,
      "--skip-git-repo-check",
      "--dangerously-bypass-hook-trust",
      "--dangerously-bypass-approvals-and-sandbox",
      prompt,
    ],
    { cwd: args.workdir, env: args.env, timeoutMs: args.timeoutMs },
  );
  res.provider = args.provider;
  return res;
}
async function prepareCursorProjectHooks(pluginDir, workdir) {
  const sourcePath = path.join(pluginDir, "hooks", "hooks.json");
  const targetDir = path.join(workdir, ".cursor");
  const targetPath = path.join(targetDir, "hooks.json");
  const hooks = JSON.parse(await fs.readFile(sourcePath, "utf8"));
  const escapedPluginDir = pluginDir.replace(/(["\\$`])/g, "\\$1");
  for (const entries of Object.values(hooks.hooks ?? {})) {
    if (!Array.isArray(entries)) {
      continue;
    }
    for (const entry of entries) {
      if (entry && typeof entry.command === "string") {
        entry.command = entry.command.replaceAll(
          "$CURSOR_PLUGIN_ROOT",
          escapedPluginDir,
        );
      }
    }
  }
  await fs.mkdir(targetDir, { recursive: true });
  await fs.writeFile(targetPath, JSON.stringify(hooks, null, 2));
}
// Cursor is the only provider whose hook payloads carry usage totals, and its
// headless agent never fires the stop hook, so drive the installed stop
// command directly with recorded-shape payloads. Both pricing shapes matter:
// API-priced sessions report a cost while subscription sessions report
// tokens only.
async function runCursorSyntheticUsage(args) {
  const hooksPath = path.join(args.pluginDir, "hooks", "hooks.json");
  const hooks = JSON.parse(await fs.readFile(hooksPath, "utf8"));
  const entry = (hooks.hooks?.stop ?? [])[0];
  if (!entry || typeof entry.command !== "string") {
    fail(`cursor hooks.json has no stop command at ${hooksPath}`);
  }
  const escapedPluginDir = args.pluginDir.replace(/(["\\$`])/g, "\\$1");
  const command = entry.command.replaceAll(
    "$CURSOR_PLUGIN_ROOT",
    escapedPluginDir,
  );
  const base = {
    hook_event_name: "stop",
    workspace_roots: [args.workdir],
    cwd: args.workdir,
    model: "e2e-synthetic",
    status: "completed",
    loop_count: 1,
  };
  const payloads = [
    {
      label: "api-pricing",
      body: {
        ...base,
        conversation_id: `${args.runId}-usage-api`,
        generation_id: `${args.runId}-usage-api-gen`,
        input_tokens: 1200,
        output_tokens: 345,
        cache_read_tokens: 800,
        cache_write_tokens: 60,
        cost: 0.0123,
      },
    },
    {
      label: "subscription",
      body: {
        ...base,
        conversation_id: `${args.runId}-usage-plan`,
        generation_id: `${args.runId}-usage-plan-gen`,
        input_tokens: 900,
        output_tokens: 210,
      },
    },
  ];
  const results = [];
  for (const payload of payloads) {
    const res = await runProcess("sh", ["-c", command], {
      cwd: args.workdir,
      input: JSON.stringify(payload.body),
      timeoutMs: args.timeoutMs,
    });
    res.provider = "cursor";
    res.label = payload.label;
    results.push(res);
  }
  return results;
}
async function clickhouseQuery(query) {
  const host = process.env.CLICKHOUSE_HOST ?? "127.0.0.1";
  const port = process.env.CLICKHOUSE_HTTP_PORT ?? "8123";
  const database = process.env.CLICKHOUSE_DATABASE ?? "default";
  const user = process.env.CLICKHOUSE_USERNAME ?? "gram";
  const password = process.env.CLICKHOUSE_PASSWORD ?? "gram";
  const url = new URL(`http://${host}:${port}`);
  url.searchParams.set("database", database);
  url.searchParams.set("query", `${query}\nFORMAT JSONEachRow`);
  const res = await fetchOrFail(
    url,
    {
      method: "POST",
      headers: {
        Authorization: `Basic ${Buffer.from(`${user}:${password}`).toString("base64")}`,
      },
    },
    "query ClickHouse",
  );
  if (!res.ok) {
    fail(`ClickHouse query failed: ${res.status} ${await res.text()}`);
  }
  const text = (await res.text()).trim();
  if (!text) {
    return [];
  }
  return text.split("\n").map((line) => JSON.parse(line));
}
// gram.hook.event stores provider-style names (PostToolUse, ...) because the
// ClickHouse summary predicates match on that vocabulary. The checks below
// reason in canonical event types, so translate rows back on read. "Stop" is
// ambiguous across providers: cursor's stop carries usage totals while
// claude/codex's marks the turn's end.
const CANONICAL_EVENT_BY_PROVIDER_STYLE = {
  SessionStart: "session.started",
  ConfigChange: "session.updated",
  PreToolUse: "tool.requested",
  BeforeMCPExecution: "tool.requested",
  PermissionRequest: "tool.requested",
  PostToolUse: "tool.completed",
  AfterMCPExecution: "tool.completed",
  PostToolUseFailure: "tool.failed",
  UserPromptSubmit: "prompt.submitted",
  BeforeSubmitPrompt: "prompt.submitted",
  AfterAgentResponse: "assistant.responded",
  AfterAgentThought: "assistant.thought",
  SessionEnd: "session.ended",
  Notification: "notification.reported",
};
function canonicalHookEventOf(source, event) {
  if (event === "Stop") {
    return source === "cursor" ? "usage.reported" : "assistant.responded";
  }
  return CANONICAL_EVENT_BY_PROVIDER_STYLE[event] ?? event;
}
async function listHookEvidence(projectId, provider, sinceUnixNano) {
  const sources = SOURCE_ALIASES[provider]
    .map((s) => `'${sqlString(s)}'`)
    .join(",");
  const rows = await clickhouseQuery(`
    SELECT
      hook_source,
      toString(attributes.gram.hook.event) AS event,
      tool_name,
      toString(hook_block_reason) AS block_reason,
      toString(attributes) AS attrs
    FROM telemetry_logs
    WHERE gram_project_id = '${sqlString(projectId)}'
      AND event_source = 'hook'
      AND time_unix_nano >= ${sinceUnixNano.toString()}
      AND hook_source IN (${sources})
    ORDER BY time_unix_nano DESC
    LIMIT 200
  `);
  return rows.map((row) => ({
    ...row,
    event: canonicalHookEventOf(row.hook_source, row.event),
  }));
}
async function listChatMessages(projectId, runId) {
  const sql = `
    WITH matched_chats AS (
      SELECT DISTINCT chat_id
      FROM chat_messages
      WHERE project_id = '${sqlString(projectId)}'
        AND (
          content LIKE '%${sqlString(runId)}%'
          OR COALESCE(tool_calls::text, '') LIKE '%${sqlString(runId)}%'
          OR COALESCE(tool_call_id, '') LIKE '%${sqlString(runId)}%'
        )
    )
    SELECT
      chat_id,
      generation,
      LOWER(COALESCE(source, '')),
      role,
      content,
      COALESCE(tool_calls::text, ''),
      COALESCE(tool_call_id, '')
    FROM chat_messages
    WHERE project_id = '${sqlString(projectId)}'
      AND chat_id IN (SELECT chat_id FROM matched_chats)
    ORDER BY created_at ASC
    LIMIT 200;
  `;
  const res = await runProcess("psql", psqlArgs(sql));
  if (res.exitCode !== 0) {
    fail(`chat_messages query failed:\n${res.stderr || res.stdout}`);
  }
  return res.stdout
    .trim()
    .split("\n")
    .filter(Boolean)
    .map((line) => {
      const [
        chatId = "",
        generation = "0",
        source = "",
        role = "",
        content = "",
        toolCalls = "",
        toolCallID = "",
      ] = line.split("\x1f");
      return {
        chatId,
        generation: Number(generation),
        source,
        role,
        content,
        toolCalls,
        toolCallID,
      };
    });
}
async function verifyOnboarding(args) {
  const url = new URL(
    `${args.serverURL}/rpc/organizations.verifyOnboardingHooksSetup`,
  );
  url.searchParams.set("since_unix_nano", args.sinceUnixNano.toString());
  const res = await fetchOrFail(
    url,
    {
      headers: {
        "Gram-Session": args.sessionId,
        "Gram-Project": args.projectSlug,
      },
    },
    "verify onboarding hook traffic",
  );
  if (!res.ok) {
    fail(
      `verifyOnboardingHooksSetup failed: ${res.status} ${await res.text()}`,
    );
  }
  return await res.json();
}
async function createShadowMCPPolicy(args) {
  await cleanupShadowMCPE2EPolicies(args.session.projectId);
  const name = `Gram hooks E2E shadow_mcp ${args.runId}`;
  const res = await fetchOrFail(
    `${args.serverURL}/rpc/risk.createPolicy`,
    {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Gram-Session": args.session.sessionId,
        "Gram-Project": args.projectSlug,
      },
      body: JSON.stringify({
        name,
        sources: ["shadow_mcp"],
        action: "block",
        audience_type: "everyone",
        enabled: true,
        user_message: "Gram hooks E2E Shadow MCP block",
      }),
    },
    "create Shadow MCP risk policy",
  );
  if (!res.ok) {
    fail(`create Shadow MCP policy failed: ${res.status} ${await res.text()}`);
  }
  return await res.json();
}
async function cleanupShadowMCPE2EPolicies(projectId) {
  const sql = `
    UPDATE risk_policies
    SET deleted_at = COALESCE(deleted_at, clock_timestamp()),
        enabled = false,
        updated_at = clock_timestamp()
    WHERE project_id = '${sqlString(projectId)}'
      AND name LIKE 'Gram hooks E2E shadow_mcp %'
      AND deleted IS FALSE;
  `;
  const res = await runProcess("psql", psqlArgs(sql));
  if (res.exitCode !== 0) {
    fail(
      `failed to clean up stale Shadow MCP E2E policies:\n${res.stderr || res.stdout}`,
    );
  }
}
async function deleteRiskPolicy(args) {
  if (!args.policyId) {
    return;
  }
  const url = new URL(`${args.serverURL}/rpc/risk.deletePolicy`);
  url.searchParams.set("id", args.policyId);
  const res = await fetchOrFail(
    url,
    {
      method: "DELETE",
      headers: {
        "Gram-Session": args.session.sessionId,
        "Gram-Project": args.projectSlug,
      },
    },
    "delete Shadow MCP risk policy",
  );
  if (!res.ok) {
    fail(
      `delete Shadow MCP policy ${args.policyId} failed: ${res.status} ${await res.text()}`,
    );
  }
}
async function createRiskPolicyBypassRequest(args) {
  const res = await fetchOrFail(
    `${args.serverURL}/rpc/risk.createPolicyBypassRequest`,
    {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Gram-Session": args.session.sessionId,
      },
      body: JSON.stringify({ request_token: args.requestToken }),
    },
    "create risk policy bypass request",
  );
  if (!res.ok) {
    fail(
      `create risk policy bypass request failed: ${res.status} ${await res.text()}`,
    );
  }
  return await res.json();
}
async function approveRiskPolicyBypassRequest(args) {
  const res = await fetchOrFail(
    `${args.serverURL}/rpc/risk.approvePolicyBypassRequest`,
    {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Gram-Session": args.session.sessionId,
        "Gram-Project": args.projectSlug,
      },
      body: JSON.stringify({
        id: args.requestId,
      }),
    },
    "approve risk policy bypass request",
  );
  if (!res.ok) {
    fail(
      `approve risk policy bypass request failed: ${res.status} ${await res.text()}`,
    );
  }
  return await res.json();
}
async function revokeRiskPolicyBypassRequest(args) {
  const res = await fetchOrFail(
    `${args.serverURL}/rpc/risk.revokePolicyBypassRequest`,
    {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Gram-Session": args.session.sessionId,
        "Gram-Project": args.projectSlug,
      },
      body: JSON.stringify({
        id: args.requestId,
      }),
    },
    "revoke risk policy bypass request",
  );
  if (!res.ok) {
    fail(
      `revoke risk policy bypass request failed: ${res.status} ${await res.text()}`,
    );
  }
  return await res.json();
}
async function listToolCallBlocks(
  projectId,
  policyId,
  provider,
  sinceUnixNano,
) {
  const sources = SOURCE_ALIASES[provider]
    .map((s) => `'${sqlString(s)}'`)
    .join(",");
  const sinceMs = Number(sinceUnixNano / 1000000n);
  const sinceISO = new Date(sinceMs).toISOString();
  const sql = `
    SELECT
      id,
      provider,
      reason,
      COALESCE(tool_name, ''),
      COALESCE(risk_policy_id::text, ''),
      COALESCE(chat_id::text, '')
    FROM tool_call_blocks
    WHERE project_id = '${sqlString(projectId)}'
      AND risk_policy_id = '${sqlString(policyId)}'
      AND provider IN (${sources})
      AND created_at >= '${sqlString(sinceISO)}'::timestamptz
      AND deleted IS FALSE
    ORDER BY created_at DESC
    LIMIT 50;
  `;
  const res = await runProcess("psql", psqlArgs(sql));
  if (res.exitCode !== 0) {
    fail(`tool_call_blocks query failed:\n${res.stderr || res.stdout}`);
  }
  return res.stdout
    .trim()
    .split("\n")
    .filter(Boolean)
    .map((line) => {
      const [
        id = "",
        source = "",
        reason = "",
        toolName = "",
        rowPolicyId = "",
        chatId = "",
      ] = line.split("\x1f");
      return { id, source, reason, toolName, policyId: rowPolicyId, chatId };
    });
}
async function poll(deadlineMs, fn, done) {
  let last = await fn();
  while (Date.now() < deadlineMs) {
    if (done(last)) {
      return last;
    }
    await new Promise((resolve) => setTimeout(resolve, 3000));
    last = await fn();
  }
  return last;
}
function featureChecks(provider, evidence, chats, opts = {}) {
  const events = new Set(evidence.map((r) => r.event).filter(Boolean));
  const cursorHeadlessAssistantUnsupported =
    provider === "cursor" && !events.has("assistant.responded");
  const hasToolFailure = events.has("tool.failed");
  const sourceAliases = SOURCE_ALIASES[provider];
  const providerChats = chats.filter((m) => sourceAliases.includes(m.source));
  const providerGenerationsByChat = new Map();
  for (const m of providerChats) {
    if (!providerGenerationsByChat.has(m.chatId)) {
      providerGenerationsByChat.set(m.chatId, new Set());
    }
    providerGenerationsByChat.get(m.chatId).add(m.generation);
  }
  const splitGenerationChats = [...providerGenerationsByChat.entries()]
    .filter(([, generations]) => generations.size > 1)
    .map(
      ([chatId, generations]) => `${chatId}:[${[...generations].join(",")}]`,
    );
  const hasToolCallMessage = providerChats.some((m) => {
    const toolCalls = m.toolCalls.trim();
    const hasToolCalls =
      toolCalls !== "" && toolCalls !== "[]" && toolCalls !== "null";
    return hasToolCalls || (m.role === "tool" && m.toolCallID.trim() !== "");
  });
  const checks = [];
  for (const feature of ["tool.requested", "tool.completed"]) {
    checks.push({
      provider,
      feature,
      status: events.has(feature) ? "PASS" : "FAIL",
      detail: events.has(feature)
        ? "observed in ClickHouse hook telemetry"
        : `missing from events: ${[...events].join(", ") || "(none)"}`,
    });
  }
  checks.push({
    provider,
    feature: "assistant.responded",
    status: events.has("assistant.responded")
      ? "PASS"
      : cursorHeadlessAssistantUnsupported
        ? "SKIP"
        : "FAIL",
    detail: events.has("assistant.responded")
      ? "observed in ClickHouse hook telemetry"
      : cursorHeadlessAssistantUnsupported
        ? "Cursor Agent headless does not reliably emit afterAgentResponse"
        : `missing from events: ${[...events].join(", ") || "(none)"}`,
  });
  checks.unshift({
    provider,
    feature: "prompt.submitted",
    status: events.has("prompt.submitted") ? "PASS" : "FAIL",
    detail: events.has("prompt.submitted")
      ? "observed in ClickHouse hook telemetry"
      : `missing from events: ${[...events].join(", ") || "(none)"}`,
  });
  checks.push({
    provider,
    feature: "tool.failed",
    status: hasToolFailure ? "PASS" : provider === "codex" ? "SKIP" : "FAIL",
    detail: hasToolFailure
      ? "observed failed provider tool call"
      : provider === "codex"
        ? "Codex does not expose a distinct failed-tool hook event in this driver"
        : "missing after failure scenario",
  });
  const usageBlocks = evidence
    .filter((r) => r.event === "usage.reported")
    .map((r) => {
      try {
        return JSON.parse(String(r.attrs ?? ""))?.gen_ai?.usage ?? null;
      } catch {
        return null;
      }
    })
    .filter(Boolean);
  const usageWithCost = usageBlocks.some(
    (u) => u.input_tokens > 0 && u.output_tokens > 0 && u.cost > 0,
  );
  const usageTokensOnly = usageBlocks.some(
    (u) => u.input_tokens > 0 && u.output_tokens > 0 && u.cost === undefined,
  );
  checks.push({
    provider,
    feature: "usage.reported",
    status:
      provider === "cursor"
        ? usageWithCost && usageTokensOnly
          ? "PASS"
          : "FAIL"
        : "SKIP",
    detail:
      provider === "cursor"
        ? usageWithCost && usageTokensOnly
          ? "synthetic stop payloads recorded token attrs for both pricing shapes"
          : `usage evidence incomplete: with-cost=${usageWithCost} tokens-only=${usageTokensOnly} rows=${usageBlocks.length}`
        : "provider hook payloads carry no usage totals",
  });
  checks.push({
    provider,
    feature: "chat.user_message_persisted",
    status: providerChats.some((m) => m.role === "user") ? "PASS" : "FAIL",
    detail: providerChats.some((m) => m.role === "user")
      ? "found RUN_ID user message in Postgres"
      : `no RUN_ID user message persisted for provider; roles: ${providerChats.map((m) => m.role).join(", ") || "(none)"}`,
  });
  checks.push({
    provider,
    feature: "chat.assistant_message_persisted",
    status: providerChats.some((m) => m.role === "assistant") ? "PASS" : "FAIL",
    detail: providerChats.some((m) => m.role === "assistant")
      ? "found RUN_ID assistant message in Postgres"
      : "no RUN_ID assistant message persisted for provider",
  });
  checks.push({
    provider,
    feature: "chat.tool_call_persisted",
    status: hasToolCallMessage ? "PASS" : "FAIL",
    detail: hasToolCallMessage
      ? "found renderable tool_call/tool result in Postgres"
      : "no renderable tool call persisted for provider",
  });
  checks.push({
    provider,
    feature: "chat.single_generation",
    status: splitGenerationChats.length === 0 ? "PASS" : "FAIL",
    detail:
      splitGenerationChats.length === 0
        ? "provider rows load in one chat generation"
        : `chat rows split across generations: ${splitGenerationChats.join("; ")}`,
  });
  if (["claude", "cursor", "codex"].includes(provider)) {
    const skillName = opts.skillName;
    const skillActivated = evidence.some(
      (r) =>
        r.event === "skill.activated" &&
        (!skillName ||
          String(r.attrs ?? "").includes(skillName) ||
          String(r.tool_name ?? "") === "Skill"),
    );
    checks.push({
      provider,
      feature: "skill.activated",
      status: skillActivated ? "PASS" : skillName ? "FAIL" : "SKIP",
      detail: skillActivated
        ? `observed ${provider} activation of ${skillName ?? "a skill"}`
        : skillName
          ? `no skill.activated telemetry for ${skillName}; events=${[...events].join(", ") || "(none)"}`
          : "not triggered by the current real-driver scenarios",
    });
  }
  if (provider === "claude") {
    checks.push({
      provider,
      feature: "notification.reported",
      status: events.has("notification.reported") ? "PASS" : "SKIP",
      detail: events.has("notification.reported")
        ? "observed Claude notification"
        : "not triggered by the current headless scenarios",
    });
  }
  return checks;
}
function extractRiskPolicyBypassToken(res) {
  const output = `${res.stdout}\n${res.stderr}`;
  const match = output.match(
    /\/risk-policy-bypass\/request#request_token=([^\s"'<>\\]+)/,
  );
  return match?.[1] ?? null;
}
function commandOutput(res) {
  return `${res.stdout}\n${res.stderr}`;
}
function outputHasFinalMarker(res, marker, verdict) {
  const target = `GRAM_HOOKS_E2E_${verdict} ${marker}`;
  for (const line of res.stdout.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed) {
      continue;
    }
    try {
      const parsed = JSON.parse(trimmed);
      if (parsed?.type === "user") {
        continue;
      }
      if (JSON.stringify(parsed).includes(target)) {
        return true;
      }
    } catch {
      if (trimmed.includes(target)) {
        return true;
      }
    }
  }
  return false;
}
function shadowMCPToolExpectation(provider, serverName) {
  return {
    provider,
    serverName,
    toolName: "shadow_lookup",
    routedName: `mcp__${serverName}__shadow_lookup`,
  };
}
function outputHasShadowMCPAttempt(output, expected) {
  if (expected.provider === "cursor") {
    return (
      output.includes(`"providerIdentifier":"${expected.serverName}"`) &&
      output.includes(`"toolName":"${expected.toolName}"`)
    );
  }
  if (expected.provider === "codex") {
    return (
      (output.includes(`"server":"${expected.serverName}"`) &&
        output.includes(`"tool":"${expected.toolName}"`)) ||
      output.includes(`Tool: ${expected.routedName}`) ||
      output.includes(`"name":"${expected.routedName}"`) ||
      output.includes(`"tool_name":"${expected.routedName}"`)
    );
  }
  return (
    output.includes(`"name":"${expected.routedName}"`) ||
    output.includes(`"tool_name":"${expected.routedName}"`)
  );
}
function telemetryRowMatchesShadowMCPTool(row, expected) {
  const toolName = String(row.tool_name ?? "");
  if (expected.provider !== "cursor") {
    return toolName === expected.routedName;
  }
  if (
    toolName !== expected.toolName &&
    toolName !== `MCP:${expected.toolName}`
  ) {
    return false;
  }
  // ClickHouse's JSON rendering escapes forward slashes, so undo that before
  // matching path-shaped markers.
  const attrs = String(row.attrs ?? "").replaceAll("\\/", "/");
  if (
    attrs.includes(`"source":"${expected.serverName}"`) ||
    attrs.includes(`"server_name":"${expected.serverName}"`) ||
    attrs.includes(`"providerIdentifier":"${expected.serverName}"`) ||
    attrs.includes(`"mcp_server_name":"${expected.serverName}"`) ||
    // Unified-ingest rows identify stdio MCP servers by launch command; the
    // fixture's script path is unique to this harness.
    attrs.includes("/shadow-mcp/server.mjs")
  ) {
    return true;
  }
  return toolName === `MCP:${expected.toolName}`;
}
function shadowMCPChecks(provider, phase, res, evidence, blocks, extra = {}) {
  const output = commandOutput(res);
  const events = new Set(evidence.map((r) => r.event).filter(Boolean));
  const expectedTool =
    extra.expectedTool ?? shadowMCPToolExpectation(provider, "");
  const attemptedTool = outputHasShadowMCPAttempt(output, expectedTool);
  const deniedRows = evidence.filter(
    (r) =>
      r.event === "tool.requested" &&
      String(r.block_reason ?? "").trim() !== "" &&
      telemetryRowMatchesShadowMCPTool(r, expectedTool),
  );
  const requestedRows = evidence.filter(
    (r) =>
      r.event === "tool.requested" &&
      telemetryRowMatchesShadowMCPTool(r, expectedTool),
  );
  const checks = [];
  if (phase === "blocked") {
    checks.push({
      provider,
      feature: "shadow_mcp.fixture_tool_called",
      status: attemptedTool ? "PASS" : "FAIL",
      detail: `provider attempted ${expectedTool.routedName}`,
    });
    checks.push({
      provider,
      feature: "shadow_mcp.blocked",
      status:
        outputHasFinalMarker(res, extra.marker, "BLOCKED") && attemptedTool
          ? "PASS"
          : "FAIL",
      detail: "provider surfaced a Shadow MCP block for the fixture tool",
    });
    checks.push({
      provider,
      feature: "shadow_mcp.request_url",
      status: extra.requestToken ? "PASS" : "FAIL",
      detail: extra.requestToken
        ? "block response included access-request URL"
        : "missing risk-policy bypass request URL",
    });
    checks.push({
      provider,
      feature: "shadow_mcp.telemetry_denied",
      status: deniedRows.length > 0 ? "PASS" : "FAIL",
      detail:
        deniedRows.length > 0
          ? `ClickHouse recorded denied ${expectedTool.routedName}`
          : `missing denied telemetry for ${expectedTool.routedName}; events=${[...events].join(", ") || "(none)"}`,
    });
    checks.push({
      provider,
      feature: "shadow_mcp.block_recorded",
      status: blocks.some((b) => b.toolName === expectedTool.toolName)
        ? "PASS"
        : "FAIL",
      detail: blocks.some((b) => b.toolName === expectedTool.toolName)
        ? "tool_call_blocks row persisted"
        : `missing durable tool_call_blocks row for ${expectedTool.toolName}`,
    });
    return checks;
  }
  checks.push({
    provider,
    feature:
      phase === "gram-hosted"
        ? "shadow_mcp.gram_hosted_tool_called"
        : "shadow_mcp.exemption_tool_called",
    status: attemptedTool ? "PASS" : "FAIL",
    detail: `provider called ${expectedTool.routedName}`,
  });
  checks.push({
    provider,
    feature:
      phase === "gram-hosted"
        ? "shadow_mcp.gram_hosted_tool_output"
        : "shadow_mcp.exemption_tool_output",
    status: output.includes(`GRAM_HOOKS_E2E_MCP_TOOL_OK ${extra.marker}`)
      ? "PASS"
      : "FAIL",
    detail: "fixture MCP server returned marker output",
  });
  checks.push({
    provider,
    feature:
      phase === "gram-hosted"
        ? "shadow_mcp.gram_hosted_allowed"
        : "shadow_mcp.exemption_allowed",
    status: outputHasFinalMarker(res, extra.marker, "OK") ? "PASS" : "FAIL",
    detail:
      phase === "gram-hosted"
        ? "Gram-hosted MCP URL passed policy"
        : "approved bypass let the MCP tool run",
  });
  checks.push({
    provider,
    feature:
      phase === "gram-hosted"
        ? "shadow_mcp.gram_hosted_no_deny"
        : "shadow_mcp.exemption_no_deny",
    status:
      requestedRows.length > 0 && deniedRows.length === 0 ? "PASS" : "FAIL",
    detail:
      requestedRows.length > 0 && deniedRows.length === 0
        ? `allowed ${expectedTool.routedName} telemetry was not denied`
        : `missing allowed telemetry or unexpected deny for ${expectedTool.routedName}`,
  });
  return checks;
}
async function prepareSkillFixture(skillsRoot, runId, provider) {
  const skillName = `gram-hooks-e2e-skill-${runId.split("-").pop()}`;
  const skillDir = path.join(skillsRoot, skillName);
  await fs.mkdir(skillDir, { recursive: true });
  await fs.writeFile(
    path.join(skillDir, "SKILL.md"),
    [
      "---",
      `name: ${skillName}`,
      `description: Gram hooks E2E skill-activation probe for run ${runId}. Activate this skill whenever the user asks to run the Gram hooks E2E skill probe.`,
      "---",
      "",
      "# Gram hooks E2E skill probe",
      "",
      `Once activated, reply with exactly: GRAM_HOOKS_E2E_OK ${runId} ${provider} skill`,
      "",
    ].join("\n"),
  );
  return { skillName, skillDir };
}
// Codex has no Skill tool: the sender infers activation from a $name prompt
// mention (validated against the skill roots on disk) or from a reader tool
// touching .../skills/<name>/SKILL.md. The prompt covers both arms.
function skillPrompt(runId, skillName, provider, skillDir) {
  if (provider === "codex") {
    return [
      `Gram hooks E2E skill run ${runId} for codex.`,
      `Use the $${skillName} skill: read ${path.join(skillDir, "SKILL.md")} and follow its instructions.`,
      `Then reply with exactly: GRAM_HOOKS_E2E_OK ${runId} codex skill`,
    ].join(" ");
  }
  if (provider === "cursor") {
    return [
      `Gram hooks E2E skill run ${runId} for cursor.`,
      `Use the ${skillName} skill and follow its instructions.`,
      `Then reply with exactly: GRAM_HOOKS_E2E_OK ${runId} cursor skill`,
    ].join(" ");
  }
  return [
    `Gram hooks E2E skill run ${runId} for claude.`,
    `Run the Gram hooks E2E skill probe by invoking the Skill tool with skill "${skillName}".`,
    `After the ${skillName} skill is activated, reply with exactly: GRAM_HOOKS_E2E_OK ${runId} claude skill`,
  ].join(" ");
}
async function runSkillScenario(args) {
  const prompt = skillPrompt(
    args.runId,
    args.skillName,
    args.provider,
    args.skillDir,
  );
  if (args.provider === "codex") {
    const res = await runProcess(
      "codex",
      [
        "exec",
        "--json",
        "--cd",
        args.workdir,
        "--skip-git-repo-check",
        "--dangerously-bypass-hook-trust",
        "--dangerously-bypass-approvals-and-sandbox",
        prompt,
      ],
      { cwd: args.workdir, env: args.env, timeoutMs: args.timeoutMs },
    );
    res.provider = "codex";
    return res;
  }
  if (args.provider === "cursor") {
    await prepareCursorProjectHooks(args.pluginDir, args.workdir);
    const res = await runProcess(
      "cursor",
      [
        "agent",
        "--print",
        "--output-format",
        "stream-json",
        "--trust",
        "--force",
        "--approve-mcps",
        "--plugin-dir",
        args.pluginDir,
        "--workspace",
        args.workdir,
        prompt,
      ],
      { cwd: args.workdir, timeoutMs: args.timeoutMs },
    );
    res.provider = "cursor";
    return res;
  }
  const sessionId = crypto.randomUUID();
  const res = await runProcess(
    "claude",
    [
      "--setting-sources",
      "project,local",
      "--plugin-dir",
      args.pluginDir,
      "--permission-mode",
      "bypassPermissions",
      "--allowedTools",
      "Read,Bash,Skill",
      "--include-hook-events",
      "--verbose",
      "--output-format",
      "stream-json",
      "--session-id",
      sessionId,
      "-p",
      prompt,
    ],
    { cwd: args.workdir, timeoutMs: args.timeoutMs },
  );
  res.provider = "claude";
  return res;
}
// runRatchetSuite verifies the never-authenticated fail-open ratchet: with no
// cached credentials, no key env vars, and local browser auth disabled, a
// Claude session must complete normally (hooks pass through instead of
// blocking) and no hook events for the run may reach Gram.
async function runRatchetSuite(args) {
  const checks = [];
  const commandResults = [];
  if (!args.providers.includes("claude")) {
    return { checks, commandResults };
  }
  const runId = `${args.runId}-ratchet`;
  const missingAuthFile = path.join(
    args.rootDir,
    "ratchet-missing",
    "hooks-auth.env",
  );
  log.info("claude: running unauthenticated ratchet scenario");
  const res = await runProviderScenario({
    provider: "claude",
    pluginDir: args.pluginDirs.get("claude"),
    workdir: args.workdir,
    runId,
    scenario: "success",
    env: {
      GRAM_HOOKS_AUTH_FILE: missingAuthFile,
      GRAM_HOOKS_DISABLE_LOCAL_AUTH: "1",
    },
    timeoutMs: args.timeoutSeconds * 1000,
  });
  commandResults.push(res);
  await writeCommandArtifacts(
    args.artifactsDir,
    "claude",
    "ratchet-unauthenticated",
    res,
  );
  checks.push({
    provider: "claude",
    feature: "ratchet: unauthenticated session is not blocked",
    status: res.exitCode === 0 ? "PASS" : "FAIL",
    detail:
      res.exitCode === 0
        ? "claude completed without credentials"
        : `claude exited ${res.exitCode}${res.timedOut ? " (timed out)" : ""}`,
  });
  const evidence = await listHookEvidence(
    args.session.projectId,
    "claude",
    args.startedUnixNano,
  );
  const leaked = evidence.filter((row) => JSON.stringify(row).includes(runId));
  checks.push({
    provider: "claude",
    feature: "ratchet: no events ingested without credentials",
    status: leaked.length === 0 ? "PASS" : "FAIL",
    detail:
      leaked.length === 0
        ? "no hook telemetry for the unauthenticated run"
        : `${leaked.length} events reached Gram without credentials`,
  });
  return { checks, commandResults };
}

async function runCaptureSuite(args) {
  const commandResults = [];
  const skillNamesByProvider = new Map();
  for (const provider of args.providers) {
    for (const scenario of ["success", "failure"]) {
      log.info(`${provider}: running capture ${scenario} scenario`);
      const res = await runProviderScenario({
        provider,
        pluginDir: args.pluginDirs.get(provider),
        workdir: args.workdir,
        runId: args.runId,
        scenario,
        env: provider === "codex" ? args.codexEnv : undefined,
        timeoutMs: args.timeoutSeconds * 1000,
      });
      commandResults.push(res);
      await writeCommandArtifacts(
        args.artifactsDir,
        provider,
        `capture-${scenario}`,
        res,
      );
      if (res.exitCode !== 0 && scenario === "success") {
        fail(
          `${provider} ${scenario} scenario failed:\n${res.stderr || res.stdout}`,
        );
      }
      if (res.timedOut) {
        fail(`${provider} ${scenario} scenario timed out`);
      }
    }
    if (provider === "cursor") {
      log.info("cursor: dispatching synthetic stop payloads for usage capture");
      const usageResults = await runCursorSyntheticUsage({
        pluginDir: args.pluginDirs.get("cursor"),
        workdir: args.workdir,
        runId: args.runId,
        timeoutMs: args.timeoutSeconds * 1000,
      });
      for (const res of usageResults) {
        commandResults.push(res);
        await writeCommandArtifacts(
          args.artifactsDir,
          "cursor",
          `capture-usage-${res.label}`,
          res,
        );
        if (res.exitCode !== 0) {
          fail(
            `cursor synthetic usage dispatch (${res.label}) failed:\n${res.stderr || res.stdout}`,
          );
        }
        if (res.timedOut) {
          fail(`cursor synthetic usage dispatch (${res.label}) timed out`);
        }
      }
    }
    if (["claude", "cursor", "codex"].includes(provider)) {
      // Codex validates $name mentions against the skill roots on disk;
      // CODEX_HOME/skills is the only root the isolated env controls
      // regardless of the hook process cwd.
      const skillsRoot =
        provider === "codex"
          ? path.join(args.codexEnv.CODEX_HOME, "skills")
          : path.join(
              args.workdir,
              provider === "cursor" ? ".cursor" : ".claude",
              "skills",
            );
      const skill = await prepareSkillFixture(skillsRoot, args.runId, provider);
      skillNamesByProvider.set(provider, skill.skillName);
      log.info(`${provider}: running capture skill-activation scenario`);
      const skillRes = await runSkillScenario({
        provider,
        pluginDir: args.pluginDirs.get(provider),
        workdir: args.workdir,
        runId: args.runId,
        skillName: skill.skillName,
        skillDir: skill.skillDir,
        env: provider === "codex" ? args.codexEnv : undefined,
        timeoutMs: args.timeoutSeconds * 1000,
      });
      commandResults.push(skillRes);
      await writeCommandArtifacts(
        args.artifactsDir,
        provider,
        "capture-skill",
        skillRes,
      );
      if (skillRes.timedOut) {
        fail(`${provider} skill-activation scenario timed out`);
      }
      if (skillRes.exitCode !== 0) {
        fail(
          `${provider} skill-activation scenario failed:\n${skillRes.stderr || skillRes.stdout}`,
        );
      }
    }
  }
  log.info("Polling Gram capture evidence");
  await verifyOnboarding({
    serverURL: args.serverURL,
    sessionId: args.session.sessionId,
    projectSlug: args.projectSlug,
    sinceUnixNano: args.startedUnixNano,
  });
  const checks = [];
  for (const provider of args.providers) {
    const skillName = skillNamesByProvider.get(provider) ?? null;
    // Cursor Agent headless does not reliably emit afterAgentResponse
    // (featureChecks marks it SKIP), so don't burn the whole poll window
    // waiting for it.
    const requiredEvents = [
      "prompt.submitted",
      ...(provider === "cursor" ? ["usage.reported"] : ["assistant.responded"]),
      "tool.requested",
      "tool.completed",
    ];
    if (skillName) {
      requiredEvents.push("skill.activated");
    }
    const deadlineMs = Date.now() + args.pollSeconds * 1000;
    const evidence = await poll(
      deadlineMs,
      () =>
        listHookEvidence(
          args.session.projectId,
          provider,
          args.startedUnixNano,
        ),
      (rows) => {
        const events = new Set(rows.map((r) => r.event));
        if (!requiredEvents.every((e) => events.has(e))) {
          return false;
        }
        // Cursor's two synthetic stop payloads (with-cost and tokens-only)
        // land as independent ClickHouse rows; featureChecks needs both.
        return (
          provider !== "cursor" ||
          rows.filter((r) => r.event === "usage.reported").length >= 2
        );
      },
    );
    const chats = await poll(
      deadlineMs,
      () => listChatMessages(args.session.projectId, args.runId),
      (rows) =>
        rows.some((r) => r.role === "user") &&
        rows.some((r) => r.role === "assistant"),
    );
    checks.push(...featureChecks(provider, evidence, chats, { skillName }));
  }
  return { checks, commandResults };
}
async function runShadowMCPSuite(args) {
  const fixture = await prepareShadowMCPFixture(args.rootDir, args.runId);
  const hostedHTTP = await startHostedMCPHTTPFixture(args.runId);
  let hostedMCP = null;
  const policy = await createShadowMCPPolicy({
    serverURL: args.serverURL,
    session: args.session,
    projectSlug: args.projectSlug,
    runId: args.runId,
  });
  log.info(`Created Shadow MCP block policy ${policy.id}`);
  const checks = [];
  const commandResults = [];
  try {
    hostedMCP = await createHostedMCPFixture({
      serverURL: args.serverURL,
      session: args.session,
      projectSlug: args.projectSlug,
      runId: args.runId,
      remoteURL: hostedHTTP.url,
    });
    log.info(
      `Created hosted MCP fixture ${hostedMCP.url} backed by ${hostedHTTP.url}`,
    );
    for (const provider of args.providers) {
      const providerFixture = {
        ...fixture,
        shadowServerName: `${provider}shadowe2e`,
        gramServerName: `${provider}grame2e`,
        gramHostedURL: hostedMCP.url,
      };
      const providerConfig = await prepareShadowMCPProviderConfig({
        provider,
        pluginDir: args.pluginDirs.get(provider),
        workdir: args.workdir,
        fixture: providerFixture,
        env: provider === "codex" ? args.codexEnv : undefined,
      });
      const blockedSince = BigInt(Date.now()) * 1000000n;
      log.info(`${provider}: running shadow-mcp blocked scenario`);
      const blocked = await runProviderShadowMCPScenario({
        provider,
        pluginDir: args.pluginDirs.get(provider),
        workdir: args.workdir,
        runId: args.runId,
        variant: "blocked",
        fixture: providerFixture,
        env: provider === "codex" ? args.codexEnv : undefined,
        timeoutMs: args.timeoutSeconds * 1000,
        ...providerConfig,
      });
      commandResults.push(blocked);
      await writeCommandArtifacts(
        args.artifactsDir,
        provider,
        "shadow-mcp-blocked",
        blocked,
      );
      if (blocked.timedOut) {
        fail(`${provider} shadow-mcp blocked scenario timed out`);
      }
      const blockedDeadline = Date.now() + args.pollSeconds * 1000;
      const blockedEvidence = await poll(
        blockedDeadline,
        () => listHookEvidence(args.session.projectId, provider, blockedSince),
        (rows) =>
          rows.some(
            (r) =>
              r.event === "tool.requested" &&
              String(r.block_reason ?? "").trim() !== "",
          ),
      );
      const blocks = await poll(
        blockedDeadline,
        () =>
          listToolCallBlocks(
            args.session.projectId,
            policy.id,
            provider,
            blockedSince,
          ),
        (rows) => rows.length > 0,
      );
      const requestToken = extractRiskPolicyBypassToken(blocked);
      checks.push(
        ...shadowMCPChecks(
          provider,
          "blocked",
          blocked,
          blockedEvidence,
          blocks,
          {
            marker: `${args.runId} ${provider} blocked`,
            requestToken,
            expectedTool: shadowMCPToolExpectation(
              provider,
              providerFixture.shadowServerName,
            ),
          },
        ),
      );
      if (!requestToken) {
        continue;
      }
      log.info(
        `${provider}: redeeming and approving shadow-mcp access request`,
      );
      const request = await createRiskPolicyBypassRequest({
        serverURL: args.serverURL,
        session: args.session,
        requestToken,
      });
      checks.push({
        provider,
        feature: "shadow_mcp.request_created",
        status:
          request.status === "requested" || request.status === "approved"
            ? "PASS"
            : "FAIL",
        detail: `request status=${request.status}`,
      });
      const approval = await approveRiskPolicyBypassRequest({
        serverURL: args.serverURL,
        session: args.session,
        projectSlug: args.projectSlug,
        requestId: request.id,
      });
      checks.push({
        provider,
        feature: "shadow_mcp.exemption_approved",
        status: approval.status === "approved" ? "PASS" : "FAIL",
        detail: `request status=${approval.status}`,
      });
      const allowedSince = BigInt(Date.now()) * 1000000n;
      log.info(`${provider}: running shadow-mcp post-approval scenario`);
      const allowed = await runProviderShadowMCPScenario({
        provider,
        pluginDir: args.pluginDirs.get(provider),
        workdir: args.workdir,
        runId: args.runId,
        variant: "approved",
        fixture: providerFixture,
        env: provider === "codex" ? args.codexEnv : undefined,
        timeoutMs: args.timeoutSeconds * 1000,
        ...providerConfig,
      });
      commandResults.push(allowed);
      await writeCommandArtifacts(
        args.artifactsDir,
        provider,
        "shadow-mcp-approved",
        allowed,
      );
      if (allowed.timedOut) {
        fail(`${provider} shadow-mcp approved scenario timed out`);
      }
      const allowedEvidence = await poll(
        Date.now() + args.pollSeconds * 1000,
        () => listHookEvidence(args.session.projectId, provider, allowedSince),
        (rows) => rows.some((r) => r.event === "tool.requested"),
      );
      checks.push(
        ...shadowMCPChecks(provider, "approved", allowed, allowedEvidence, [], {
          marker: `${args.runId} ${provider} approved`,
          expectedTool: shadowMCPToolExpectation(
            provider,
            providerFixture.shadowServerName,
          ),
        }),
      );
      const hostedSince = BigInt(Date.now()) * 1000000n;
      log.info(`${provider}: running shadow-mcp Gram-hosted scenario`);
      const hosted = await runProviderShadowMCPScenario({
        provider,
        pluginDir: args.pluginDirs.get(provider),
        workdir: args.workdir,
        runId: args.runId,
        variant: "gram-hosted",
        fixture: providerFixture,
        env: provider === "codex" ? args.codexEnv : undefined,
        timeoutMs: args.timeoutSeconds * 1000,
        ...providerConfig,
      });
      commandResults.push(hosted);
      await writeCommandArtifacts(
        args.artifactsDir,
        provider,
        "shadow-mcp-gram-hosted",
        hosted,
      );
      if (hosted.timedOut) {
        fail(`${provider} shadow-mcp Gram-hosted scenario timed out`);
      }
      const hostedEvidence = await poll(
        Date.now() + args.pollSeconds * 1000,
        () => listHookEvidence(args.session.projectId, provider, hostedSince),
        (rows) => rows.some((r) => r.event === "tool.requested"),
      );
      checks.push(
        ...shadowMCPChecks(
          provider,
          "gram-hosted",
          hosted,
          hostedEvidence,
          [],
          {
            marker: `${args.runId} ${provider} gram-hosted`,
            expectedTool: shadowMCPToolExpectation(
              provider,
              providerFixture.gramServerName,
            ),
          },
        ),
      );
      // Every provider's fixture is backed by the same MCP server command, so
      // an approval granted for one provider would bypass the next provider's
      // blocked scenario. Revoke it before moving on.
      await revokeRiskPolicyBypassRequest({
        serverURL: args.serverURL,
        session: args.session,
        projectSlug: args.projectSlug,
        requestId: request.id,
      });
    }
  } finally {
    try {
      await deleteRiskPolicy({
        serverURL: args.serverURL,
        session: args.session,
        projectSlug: args.projectSlug,
        policyId: policy.id,
      });
    } catch (err) {
      const detail = err instanceof Error ? err.message : String(err);
      log.warn(`Failed to clean up Shadow MCP policy ${policy.id}: ${detail}`);
    }
    if (hostedMCP) {
      await deleteHostedMCPFixture({
        serverURL: args.serverURL,
        session: args.session,
        projectSlug: args.projectSlug,
        ...hostedMCP,
      });
    }
    await hostedHTTP.close();
  }
  return { checks, commandResults };
}
function printMatrix(checks) {
  const useColor =
    !process.env.NO_COLOR && (process.stdout.isTTY || process.env.FORCE_COLOR);
  const color = (code, value) =>
    useColor ? `\x1b[${code}m${value}\x1b[0m` : value;
  const formatStatus = (status, width) => {
    const normalized = String(status).toLowerCase();
    const padded = normalized.padEnd(width);
    switch (normalized) {
      case "pass":
        return color(32, padded);
      case "fail":
        return color(31, padded);
      case "skip":
        return color(2, padded);
      default:
        return padded;
    }
  };
  const widths = {
    provider: Math.max(
      "provider".length,
      ...checks.map((c) => c.provider.length),
    ),
    feature: Math.max("feature".length, ...checks.map((c) => c.feature.length)),
    status: Math.max(
      "status".length,
      ...checks.map((c) => String(c.status).toLowerCase().length),
    ),
  };
  console.log("");
  console.log(
    `${"provider".padEnd(widths.provider)}  ${"feature".padEnd(widths.feature)}  ${"status".padEnd(widths.status)}  detail`,
  );
  console.log(
    `${"-".repeat(widths.provider)}  ${"-".repeat(widths.feature)}  ${"-".repeat(widths.status)}  ${"-".repeat(60)}`,
  );
  for (const c of checks) {
    console.log(
      `${c.provider.padEnd(widths.provider)}  ${c.feature.padEnd(widths.feature)}  ${formatStatus(c.status, widths.status)}  ${c.detail}`,
    );
  }
}
async function main() {
  const args = parseArgs(process.argv.slice(2));
  const serverURL = requireEnv("GRAM_SERVER_URL");
  const runId = `gram-hooks-e2e-${Date.now()}-${crypto.randomBytes(3).toString("hex")}`;
  const startedUnixNano = BigInt(Date.now()) * 1000000n;
  const rootDir = await fs.mkdtemp(path.join(os.tmpdir(), `${runId}-`));
  const artifactsDir = path.join(rootDir, "artifacts");
  const workdir = path.join(rootDir, "workspace");
  let codexEnv = null;
  await fs.mkdir(artifactsDir, { recursive: true });
  await fs.mkdir(workdir, { recursive: true });
  await fs.writeFile(
    path.join(workdir, `input-${runId}.txt`),
    `file-content-${runId}\n`,
  );
  intro(`Gram hooks E2E ${runId}`);
  let success = false;
  try {
    const session = await getSessionInfo(serverURL, args.project);
    log.info(
      `Authenticated as ${session.userEmail}; org=${session.organizationId} project=${args.project}`,
    );
    await enableSessionCapture(session.organizationId);
    log.info("Enabled session_capture for the active org");
    const hooksAuthFile = await provisionHooksAuth(
      serverURL,
      session,
      args.project,
      rootDir,
    );
    // Make the provisioned cache authoritative for every provider spawn:
    // scrub ambient key env vars that would otherwise short-circuit it.
    process.env.GRAM_HOOKS_AUTH_FILE = hooksAuthFile;
    delete process.env.GRAM_HOOKS_API_KEY;
    delete process.env.GRAM_API_KEY;
    log.info(`Provisioned hooks auth cache at ${hooksAuthFile}`);
    if (args.providers.includes("codex")) {
      codexEnv = await prepareCodexEnv(rootDir);
      log.info(`Prepared isolated Codex home at ${codexEnv.HOME}`);
    }
    const pluginDirs = new Map();
    for (const provider of args.providers) {
      const envDir =
        process.env[`GRAM_HOOKS_E2E_${provider.toUpperCase()}_PLUGIN_DIR`];
      // Codex hooks install into the fresh isolated Codex home, so there is
      // no prebuilt plugin dir to substitute.
      if (args.skipBuild && !envDir && provider !== "codex") {
        fail(
          `--skip-build requires GRAM_HOOKS_E2E_${provider.toUpperCase()}_PLUGIN_DIR`,
        );
      }
      const pluginDir =
        args.skipBuild && provider !== "codex"
          ? envDir
          : await buildProviderPlugin({
              serverURL,
              projectSlug: args.project,
              provider,
              artifactsDir,
              codexEnv,
            });
      pluginDirs.set(provider, pluginDir);
      log.info(`${provider}: using plugin ${pluginDir}`);
    }
    const allChecks = [];
    const commandResults = [];
    const suiteArgs = {
      providers: args.providers,
      pluginDirs,
      artifactsDir,
      rootDir,
      workdir,
      runId,
      codexEnv,
      timeoutSeconds: args.timeoutSeconds,
      pollSeconds: args.pollSeconds,
      serverURL,
      session,
      projectSlug: args.project,
      startedUnixNano,
    };
    if (args.suites.includes("capture")) {
      const result = await runCaptureSuite(suiteArgs);
      allChecks.push(...result.checks);
      commandResults.push(...result.commandResults);
    }
    if (args.suites.includes("shadow-mcp")) {
      const result = await runShadowMCPSuite(suiteArgs);
      allChecks.push(...result.checks);
      commandResults.push(...result.commandResults);
    }
    if (args.suites.includes("ratchet")) {
      const result = await runRatchetSuite(suiteArgs);
      allChecks.push(...result.checks);
      commandResults.push(...result.commandResults);
    }
    printMatrix(allChecks);
    const failed = allChecks.filter((c) => c.status === "FAIL");
    if (failed.length > 0) {
      console.log("");
      for (const res of commandResults) {
        if (res.exitCode !== 0) {
          console.log(`${res.provider} command failed: ${res.command}`);
          console.log((res.stderr || res.stdout).slice(-4000));
        }
      }
      fail(`${failed.length} required feature checks failed`);
    }
    success = true;
  } finally {
    if (args.keepArtifacts || !success) {
      log.info(`Artifacts kept at ${rootDir}`);
    } else {
      await fs.rm(rootDir, { recursive: true, force: true });
    }
    outro(success ? "hooks:e2e passed" : "hooks:e2e failed");
  }
}
try {
  await main();
} catch (err) {
  const message = err instanceof Error ? err.message : String(err);
  console.error(`hooks:e2e error: ${message}`);
  // Force-exit: a failure can propagate while the hosted shadow MCP listener
  // or provider child processes are still holding the event loop open.
  process.exit(1);
}

async function writeCommandArtifacts(artifactsDir, provider, scenario, res) {
  const dir = path.join(artifactsDir, "commands", provider);
  await fs.mkdir(dir, { recursive: true });
  await fs.writeFile(path.join(dir, `${scenario}.stdout`), res.stdout);
  await fs.writeFile(path.join(dir, `${scenario}.stderr`), res.stderr);
  await fs.writeFile(
    path.join(dir, `${scenario}.json`),
    JSON.stringify(
      {
        command: res.command,
        exitCode: res.exitCode,
        signal: res.signal,
        timedOut: res.timedOut,
      },
      null,
      2,
    ),
  );
}
