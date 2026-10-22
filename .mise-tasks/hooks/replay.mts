#!/usr/bin/env -S node --import tsx

//MISE description="Replay a Claude Code session transcript into the local server through hooks, OTel and the Anthropic inference hook"
//MISE dir="{{ config_root }}"

//USAGE arg "<transcript>" help="Claude Code session transcript (~/.claude/projects/<project>/<session>.jsonl)"
//USAGE flag "--project <slug>" default="default" help="Project slug the hooks and OTel paths write into"
//USAGE flag "--actor-email <email>" default="" help="Email the inference frames claim for their actor (default: the dev session's user)"
//USAGE flag "--skip-hooks" help="Do not drive the hooks relay"
//USAGE flag "--skip-otel" help="Do not post OTel log events"
//USAGE flag "--skip-inference" help="Do not post inference frames"
//USAGE flag "--dry-run" help="Print the replay plan and exit without contacting the server"
//USAGE flag "--allow-remote" help="Allow a GRAM_SERVER_URL that is not localhost. Transcripts hold real content; the default refuses to send it anywhere else"

// Replays one transcript through every capture path the real session would
// have reached, in file order, so hooks-vs-inference dedup and rewind handling
// can be exercised without an Anthropic organization or a tunnel. The three
// paths are the ones production uses: the speakeasy-hooks relay with Claude's
// hook payloads on stdin, signed inference frames on the webhook URL the
// aiIntegrations service hands out, and OTLP/JSON log events on the hooks OTel
// endpoint. The parsing lives in ./replay/transcript.mts and is unit-tested.

import { spawn } from "node:child_process";
import crypto from "node:crypto";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { intro, log, outro } from "@clack/prompts";
import { GramCore } from "#gram/client/core.js";
import { aiIntegrationsUpsertAnthropicInferenceConfig } from "#gram/client/funcs/aiIntegrationsUpsertAnthropicInferenceConfig.js";
import { authInfo } from "#gram/client/funcs/authInfo.js";
import { keysCreate } from "#gram/client/funcs/keysCreate.js";
import {
  buildFrame,
  inferenceConversationID,
  isLocalServer,
  newSigningSecret,
  otlpLogsPayload,
  parseTranscript,
  planReplay,
  sessionIDToChatID,
  signFrame,
  type FrameIdentity,
  type FrameStep,
  type HookStep,
  type OtelStep,
  type ReplayPlan,
} from "./replay/transcript.mts";

interface Args {
  transcript: string;
  project: string;
  actorEmail: string;
  skipHooks: boolean;
  skipOtel: boolean;
  skipInference: boolean;
  dryRun: boolean;
  allowRemote: boolean;
}

interface Session {
  sessionId: string;
  organizationId: string;
  organizationSlug: string;
  projectId: string;
  userEmail: string;
}

interface ProcessResult {
  exitCode: number | null;
  stdout: string;
  stderr: string;
}

function fail(message: string): never {
  throw new Error(message);
}

function requireEnv(name: string): string {
  const value = process.env[name];
  if (!value) fail(`${name} is not set`);
  return value;
}

function flag(name: string): boolean {
  const value = process.env[`usage_${name}`];
  return value === "true" || value === "1";
}

function parseArgs(): Args {
  const transcript = process.env.usage_transcript;
  if (!transcript) fail("a transcript path is required");
  return {
    transcript: path.resolve(transcript),
    project: process.env.usage_project || "default",
    actorEmail: process.env.usage_actor_email || "",
    skipHooks: flag("skip_hooks"),
    skipOtel: flag("skip_otel"),
    skipInference: flag("skip_inference"),
    dryRun: flag("dry_run"),
    allowRemote: flag("allow_remote"),
  };
}

async function fetchOrFail(
  input: string | URL,
  init: RequestInit,
  label: string,
): Promise<Response> {
  try {
    return await fetch(input, init);
  } catch (err) {
    const detail = err instanceof Error ? err.message : String(err);
    return fail(`Failed to ${label}: ${detail}`);
  }
}

// authenticateViaDevIDP walks the local dev-idp login the way the dashboard
// does and returns the Gram-Session token. The dev-idp approves without
// credentials, so this needs a running local stack and nothing else.
async function authenticateViaDevIDP(serverURL: string): Promise<string> {
  const loginRes = await fetchOrFail(
    `${serverURL}/rpc/auth.login`,
    { redirect: "manual" },
    `connect to Speakeasy server at ${serverURL}`,
  );
  const authorizeURL = loginRes.headers.get("location");
  if (!authorizeURL) fail("auth.login did not return a redirect");
  const nonceCookie = loginRes.headers
    .getSetCookie()
    .find((c) => c.startsWith("gram_auth_nonce="));
  if (!nonceCookie) fail("auth.login did not set gram_auth_nonce");
  const authorizeRes = await fetchOrFail(
    authorizeURL,
    { redirect: "manual" },
    "authorize through dev-idp",
  );
  const callbackLocation = authorizeRes.headers.get("location");
  if (!callbackLocation)
    fail("dev-idp authorize did not return a callback redirect");
  // The dev-idp sends the browser back to the dashboard origin, whose dev
  // server proxies /rpc/* to the API. The API answers the callback directly,
  // so going to it avoids needing the dashboard daemon for a replay.
  const callbackURL = new URL(callbackLocation);
  const callbackRes = await fetchOrFail(
    new URL(callbackURL.pathname + callbackURL.search, serverURL),
    {
      redirect: "manual",
      headers: { cookie: nonceCookie.split(";")[0] ?? "" },
    },
    "complete dev-idp callback",
  );
  const sessionToken = callbackRes.headers.get("gram-session");
  if (!sessionToken)
    fail("dev-idp callback did not return a gram-session header");
  return sessionToken;
}

async function getSession(
  serverURL: string,
  projectSlug: string,
): Promise<Session> {
  const gram = new GramCore({ serverURL });
  const sessionId = await authenticateViaDevIDP(serverURL);
  const res = await authInfo(gram, undefined, {
    sessionHeaderGramSession: sessionId,
  });
  if (!res.ok) fail(`authInfo failed: ${JSON.stringify(res.error)}`);
  const session = res.value.result;
  const organizationId = session.activeOrganizationId;
  if (!organizationId) fail("No active organization on dev session");
  const org = session.organizations.find((o) => o.id === organizationId);
  if (!org) fail(`Active organization ${organizationId} not found in authInfo`);
  const project = org.projects.find((p) => p.slug === projectSlug);
  if (!project)
    fail(
      `Project ${projectSlug} not found in active organization ${organizationId}`,
    );
  return {
    sessionId,
    organizationId,
    organizationSlug: org.slug,
    projectId: project.id,
    userEmail: session.userEmail,
  };
}

async function runProcess(
  command: string,
  args: string[],
  opts: { input?: string; env?: Record<string, string>; cwd?: string } = {},
): Promise<ProcessResult> {
  return await new Promise((resolve) => {
    const child = spawn(command, args, {
      cwd: opts.cwd,
      env: { ...process.env, ...opts.env },
      stdio: ["pipe", "pipe", "pipe"],
    });
    let stdout = "";
    let stderr = "";
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk: string) => {
      stdout += chunk;
    });
    child.stderr.on("data", (chunk: string) => {
      stderr += chunk;
    });
    child.on("error", (err) =>
      resolve({ exitCode: null, stdout, stderr: stderr || String(err) }),
    );
    child.on("close", (exitCode) => resolve({ exitCode, stdout, stderr }));
    child.stdin.on("error", () => {});
    if (opts.input) child.stdin.write(opts.input);
    child.stdin.end();
  });
}

function sqlString(value: string): string {
  return value.replaceAll("'", "''");
}

// psql rows come back as unit-separated fields, one row per line.
async function psql(sql: string): Promise<string[][]> {
  const databaseURL = requireEnv("GRAM_DATABASE_URL").replace(
    /&search_path=.*$/,
    "",
  );
  const res = await runProcess("psql", [
    databaseURL,
    "-tA",
    "-F",
    "\x1f",
    "-v",
    "ON_ERROR_STOP=1",
    "-c",
    sql,
  ]);
  if (res.exitCode !== 0) fail(`psql failed: ${res.stderr || res.stdout}`);
  return res.stdout
    .split("\n")
    .filter((line) => line !== "")
    .map((line) => line.split("\x1f"));
}

// Hook ingest needs session_capture; telemetry_logs writes need logs. Same
// idempotent enable hooks:test performs.
async function enableCaptureFeatures(organizationId: string): Promise<void> {
  const org = sqlString(organizationId);
  await psql(
    `INSERT INTO organization_features (organization_id, feature_name) VALUES ('${org}', 'session_capture'), ('${org}', 'logs') ON CONFLICT (organization_id, feature_name) WHERE deleted IS FALSE DO NOTHING`,
  );
}

async function mintHooksKey(
  serverURL: string,
  session: Session,
): Promise<string> {
  const gram = new GramCore({ serverURL });
  const res = await keysCreate(
    gram,
    {
      createKeyForm: { name: `hooks-replay-${Date.now()}`, scopes: ["hooks"] },
    },
    { sessionHeaderGramSession: session.sessionId },
  );
  if (!res.ok) fail(`keys.create failed: ${JSON.stringify(res.error)}`);
  if (!res.value.key) fail("keys.create returned no key");
  return res.value.key;
}

interface HooksRuntime {
  binary: string;
  configPath: string;
  env: Record<string, string>;
}

// The relay is the same binary the Claude plugin installs; it reads one hook
// payload on stdin and posts the canonical envelope. The identity shim stands
// in for the device agent so rows are attributed to the chosen user, as in
// hooks:test.
async function prepareHooksRuntime(
  rootDir: string,
  serverURL: string,
  session: Session,
  apiKey: string,
  actorEmail: string,
  projectSlug: string,
): Promise<HooksRuntime> {
  const binary = path.join(rootDir, "bin", "speakeasy-hooks");
  await fs.mkdir(path.dirname(binary), { recursive: true });
  const build = await runProcess("go", [
    "build",
    "-o",
    binary,
    "./hooks/cmd/speakeasy-hooks",
  ]);
  if (build.exitCode !== 0) fail(`build speakeasy-hooks: ${build.stderr}`);
  const configPath = path.join(rootDir, "speakeasy.json");
  await fs.writeFile(
    configPath,
    JSON.stringify({
      server_url: serverURL,
      project: projectSlug,
      org: session.organizationId,
    }),
  );
  const shim = path.join(rootDir, "identity-shim");
  await fs.writeFile(
    shim,
    `#!/usr/bin/env bash\nprintf '%s\\n' "\${GRAM_HOOKS_TEST_RESOLVED_USER_EMAIL:-}"\n`,
    { mode: 0o755 },
  );
  return {
    binary,
    configPath,
    env: {
      GRAM_HOOKS_API_KEY: apiKey,
      GRAM_HOOKS_DISABLE_LOCAL_AUTH: "1",
      GRAM_HOOKS_TEST_RESOLVED_USER_EMAIL: actorEmail,
      GRAM_DEVICE_AGENT_COMMANDS: shim,
    },
  };
}

async function sendHook(
  runtime: HooksRuntime,
  step: HookStep,
  cwd: string,
): Promise<ProcessResult> {
  return await runProcess(
    runtime.binary,
    [
      `--config=${runtime.configPath}`,
      "agenthooks",
      "run",
      "--provider=claude-code",
    ],
    {
      input: JSON.stringify(step.payload),
      env: runtime.env,
      cwd,
    },
  );
}

interface InferenceEndpoint {
  url: string;
  secret: string;
  configId: string;
  boundProjectId: string;
}

// The aiIntegrations service mints the webhook URL and stores the signing
// secret, exactly as the dashboard's Connect flow does. The URL binds to the
// organization's first active project, which may differ from --project.
async function connectInference(
  serverURL: string,
  session: Session,
): Promise<InferenceEndpoint> {
  const secret = newSigningSecret(crypto.randomBytes(32));
  const gram = new GramCore({ serverURL });
  const res = await aiIntegrationsUpsertAnthropicInferenceConfig(
    gram,
    {
      upsertAnthropicInferenceConfigRequestBody: {
        signingSecret: secret,
        enabled: true,
      },
    },
    { sessionHeaderGramSession: session.sessionId },
  );
  if (!res.ok)
    fail(
      `aiIntegrations.upsertAnthropicInferenceConfig failed: ${JSON.stringify(res.error)}`,
    );
  const config = res.value;
  if (!config.webhookPath || !config.id)
    fail("inference config has no webhook path");
  const rows = await psql(
    `SELECT project_id FROM ai_integration_configs WHERE id = '${sqlString(config.id)}'`,
  );
  const boundProjectId = rows[0]?.[0] ?? session.projectId;
  return {
    url: `${serverURL}${config.webhookPath}`,
    secret,
    configId: config.id,
    boundProjectId,
  };
}

async function sendFrame(
  endpoint: InferenceEndpoint,
  step: FrameStep,
  identity: FrameIdentity,
): Promise<{ status: number; action: string }> {
  const body = JSON.stringify(buildFrame(step, identity));
  const res = await fetchOrFail(
    endpoint.url,
    {
      method: "POST",
      headers: {
        "content-type": "application/json",
        ...signFrame(endpoint.secret, step.requestId, body, new Date()),
      },
      body,
    },
    `post inference frame ${step.requestId}`,
  );
  let action = "";
  try {
    const verdict = (await res.json()) as { action?: string };
    action = verdict.action ?? "";
  } catch {
    action = "";
  }
  return { status: res.status, action };
}

async function sendOtel(
  serverURL: string,
  apiKey: string,
  projectSlug: string,
  steps: OtelStep[],
  userEmail: string,
): Promise<number> {
  const res = await fetchOrFail(
    `${serverURL}/rpc/hooks.otel/v1/logs`,
    {
      method: "POST",
      headers: {
        "content-type": "application/json",
        "Gram-Key": apiKey,
        "Gram-Project": projectSlug,
      },
      body: JSON.stringify(otlpLogsPayload(steps, { "user.email": userEmail })),
    },
    "post OTel log events",
  );
  return res.status;
}

function describePlan(plan: ReplayPlan): string {
  const count = (kind: string) =>
    plan.steps.filter((s) => s.kind === kind).length;
  const hookEvents = new Map<string, number>();
  for (const step of plan.steps) {
    if (step.kind === "hook")
      hookEvents.set(step.event, (hookEvents.get(step.event) ?? 0) + 1);
  }
  const events = [...hookEvents]
    .map(([event, n]) => `${event}×${n}`)
    .join(", ");
  return [
    `session ${plan.sessionId}`,
    `hooks: ${count("hook")} (${events})`,
    `inference frames: ${count("frame")}`,
    `otel events: ${count("otel")}`,
    `forks in the transcript: ${plan.forks.length}`,
  ].join("\n");
}

interface Counters {
  hooksOk: number;
  hooksFailed: number;
  framesAllow: number;
  framesDeny: number;
  framesFailed: number;
  otelOk: number;
  otelFailed: number;
}

async function summarize(
  plan: ReplayPlan,
  hooksProjectId: string,
  inference: InferenceEndpoint | null,
  identity: FrameIdentity,
): Promise<string[]> {
  const chats = [
    {
      label: "hooks chat",
      id: sessionIDToChatID(plan.sessionId),
      projectId: hooksProjectId,
    },
  ];
  if (inference) {
    chats.push({
      label: "inference chat",
      id: inferenceConversationID(inference.boundProjectId, identity),
      projectId: inference.boundProjectId,
    });
  }
  const columns = await psql(
    "SELECT 1 FROM information_schema.columns WHERE table_name = 'chat_messages' AND column_name = 'primary_message_id'",
  );
  const hasPrimary = columns.length > 0;
  const lines: string[] = [];
  for (const chat of chats) {
    const rows = await psql(
      `SELECT count(m.id), ${hasPrimary ? "count(m.primary_message_id)" : "0"}, count(DISTINCT m.id) FILTER (WHERE m.risk_analyzed_at IS NULL) FROM chat_messages m WHERE m.chat_id = '${sqlString(chat.id)}' AND m.project_id = '${sqlString(chat.projectId)}'`,
    );
    const [messages = "0", secondaries = "0", unanalyzed = "0"] = rows[0] ?? [];
    const secondaryNote = hasPrimary
      ? `, ${secondaries} secondary`
      : ", (no primary_message_id column on this schema)";
    lines.push(
      `${chat.label} ${chat.id}: ${messages} messages${secondaryNote}, ${unanalyzed} awaiting risk analysis`,
    );
  }
  return lines;
}

async function main(): Promise<void> {
  const args = parseArgs();
  const serverURL = requireEnv("GRAM_SERVER_URL");
  if (!isLocalServer(serverURL) && !args.allowRemote) {
    fail(
      `GRAM_SERVER_URL ${serverURL} is not localhost; transcripts hold real content. Pass --allow-remote to send it anyway.`,
    );
  }

  const records = parseTranscript(await fs.readFile(args.transcript, "utf8"));
  const plan = planReplay(records, { transcriptPath: args.transcript });
  if (!plan.sessionId) fail("the transcript carries no sessionId");

  intro(`Replay ${path.basename(args.transcript)}`);
  log.info(describePlan(plan));
  for (const warning of plan.warnings.slice(0, 5)) log.warn(warning);
  if (plan.warnings.length > 5)
    log.warn(`${plan.warnings.length - 5} more warnings`);
  if (args.dryRun) {
    outro("Dry run: nothing was sent.");
    return;
  }

  const session = await getSession(serverURL, args.project);
  const actorEmail = args.actorEmail || session.userEmail;
  const identity: FrameIdentity = {
    sessionId: plan.sessionId,
    actorId: actorEmail,
    actorEmail,
    tenantId: "",
  };
  await enableCaptureFeatures(session.organizationId);
  const apiKey = await mintHooksKey(serverURL, session);
  const rootDir = await fs.mkdtemp(
    path.join(os.tmpdir(), "gram-hooks-replay-"),
  );
  const workdir = path.join(rootDir, "workspace");
  await fs.mkdir(workdir, { recursive: true });

  let hooks: HooksRuntime | null = null;
  if (!args.skipHooks) {
    hooks = await prepareHooksRuntime(
      rootDir,
      serverURL,
      session,
      apiKey,
      actorEmail,
      args.project,
    );
    log.step("hooks relay built");
  }
  let inference: InferenceEndpoint | null = null;
  if (!args.skipInference) {
    inference = await connectInference(serverURL, session);
    log.step(
      `inference hook connected (${inference.configId}, bound to project ${inference.boundProjectId})`,
    );
  }

  const counters: Counters = {
    hooksOk: 0,
    hooksFailed: 0,
    framesAllow: 0,
    framesDeny: 0,
    framesFailed: 0,
    otelOk: 0,
    otelFailed: 0,
  };
  try {
    for (const step of plan.steps) {
      if (step.kind === "hook" && hooks) {
        const res = await sendHook(hooks, step, workdir);
        if (res.exitCode === 0) counters.hooksOk++;
        else {
          counters.hooksFailed++;
          log.warn(
            `${step.event} exited ${res.exitCode}: ${(res.stderr || res.stdout).trim().split("\n")[0] ?? ""}`,
          );
        }
      } else if (step.kind === "frame" && inference) {
        const res = await sendFrame(inference, step, identity);
        if (res.status !== 200) {
          counters.framesFailed++;
          log.warn(`frame ${step.requestId} returned ${res.status}`);
        } else if (res.action === "deny") counters.framesDeny++;
        else counters.framesAllow++;
      } else if (step.kind === "otel" && !args.skipOtel) {
        const status = await sendOtel(
          serverURL,
          apiKey,
          args.project,
          [step],
          actorEmail,
        );
        if (status >= 200 && status < 300) counters.otelOk++;
        else {
          counters.otelFailed++;
          log.warn(`otel ${step.attributes["event.name"]} returned ${status}`);
        }
      }
    }
  } finally {
    await fs.rm(rootDir, { recursive: true, force: true });
  }

  log.info(
    [
      `hooks: ${counters.hooksOk} delivered, ${counters.hooksFailed} failed`,
      `frames: ${counters.framesAllow} allowed, ${counters.framesDeny} denied, ${counters.framesFailed} failed`,
      `otel: ${counters.otelOk} delivered, ${counters.otelFailed} failed`,
    ].join("\n"),
  );
  for (const line of await summarize(
    plan,
    session.projectId,
    inference,
    identity,
  ))
    log.info(line);
  outro("Replay finished.");
}

main().catch((err: unknown) => {
  console.error(err instanceof Error ? err.message : String(err));
  process.exit(1);
});
