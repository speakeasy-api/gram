// Shared by setup, status, and the supervised listener. Never print config or CLI output.
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync, renameSync } from "node:fs";
import { join } from "node:path";
import { parseTOML } from "confbox";

export interface StripeConfig {
  enabled: boolean;
  key: string;
  secret: string;
  target: string;
}

export function registerStripeListener(root = process.cwd()): void {
  try {
    execFileSync(
      "pitchfork",
      [
        "daemons",
        "add",
        "stripe-listener",
        "--local",
        "--run",
        "mise run stripe:_listen",
        "--ready-cmd",
        "mise run stripe:status --ready",
        "--depends",
        "server",
      ],
      { cwd: root, stdio: "pipe", timeout: 30_000, killSignal: "SIGKILL" },
    );
  } catch {
    throw new Error(
      "Cannot register the listener in pitchfork.local.toml. Check Pitchfork and the local configuration, then rerun mise run stripe:listen.",
    );
  }
}

function stripeListenerEnabled(root: string): boolean {
  let source: string;
  try {
    source = readFileSync(join(root, "pitchfork.local.toml"), "utf8");
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") return false;
    throw new Error("Cannot read pitchfork.local.toml.");
  }
  const local = parseTOML(source) as { daemons?: Record<string, unknown> };
  return local.daemons?.["stripe-listener"] !== undefined;
}

export function persistedStripeConfig(
  root = process.cwd(),
  env = process.env,
): StripeConfig {
  let local: { env?: Record<string, unknown> };
  try {
    local = parseTOML(readFileSync(join(root, "mise.local.toml"), "utf8"));
  } catch {
    throw new Error(
      "Cannot read local Stripe configuration. Run mise run stripe:setup.",
    );
  }
  const value = (name: string): string =>
    typeof local.env?.[name] === "string" ? local.env[name] : "";
  return {
    enabled: stripeListenerEnabled(root),
    key: value("STRIPE_API_KEY"),
    secret: value("STRIPE_WEBHOOK_SECRET"),
    target: localWebhookTarget(
      env.GRAM_SERVER_URL ?? "",
      env.GRAM_SERVER_PORT ?? "",
    ),
  };
}

export function localWebhookTarget(serverURL: string, port: string): string {
  let url: URL;
  try {
    url = new URL(serverURL);
  } catch {
    throw new Error("GRAM_SERVER_URL must be a local HTTP(S) URL.");
  }
  if (
    !["http:", "https:"].includes(url.protocol) ||
    !["localhost", "127.0.0.1", "[::1]"].includes(url.hostname) ||
    url.username ||
    url.password ||
    url.search ||
    url.hash ||
    url.pathname !== "/" ||
    !/^\d+$/.test(port) ||
    Number(port) < 1 ||
    Number(port) > 65535 ||
    (url.port || (url.protocol === "https:" ? "443" : "80")) !== port
  ) {
    throw new Error(
      "Refusing Stripe forwarding: use a loopback GRAM_SERVER_URL matching this worktree GRAM_SERVER_PORT.",
    );
  }
  return new URL("/rpc/stripe.webhook", url).href;
}

export function stripeConfigIssue(config: StripeConfig): string | undefined {
  if (!config.enabled)
    return "Listener disabled. Run mise run stripe:listen to opt in.";
  if (!/^(sk|rk)_test_[A-Za-z0-9_]+$/.test(config.key))
    return "Persist a Stripe sandbox test key with mise run stripe:setup; CLI login credentials are not used.";
  if (!/^whsec_[A-Za-z0-9]+$/.test(config.secret))
    return "Webhook signing secret missing. Run mise run stripe:setup.";
  return undefined;
}

export function stripeFingerprint(config: StripeConfig): string {
  return createHash("sha256").update(JSON.stringify(config)).digest("hex");
}

export type ListenerPhase =
  | "starting"
  | "disconnected"
  | "ready"
  | "delivered"
  | "delivery-failed"
  | "expired"
  | "auth-failed"
  | "secret-mismatch"
  | "stopped";
export interface ListenerState {
  pid: number;
  processStarted?: string;
  fingerprint: string;
  phase: ListenerPhase;
  updatedAt: string;
}
// A recycled PID must not resurrect a previous listener session. Only query
// process start time, never command arguments/environment (which may be secret).
export function listenerProcessStarted(pid: number): string {
  if (!Number.isInteger(pid) || pid < 1) return "";
  try {
    return execFileSync("ps", ["-p", String(pid), "-o", "lstart="], {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
      timeout: 2000,
    }).trim();
  } catch {
    return "";
  }
}

export function stripeStatePath(root = process.cwd()): string {
  const gitdir = execFileSync("git", ["rev-parse", "--absolute-git-dir"], {
    cwd: root,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  }).trim();
  return join(gitdir, "gram-stripe-listener.json");
}
export function writeStripeState(path: string, state: ListenerState): void {
  const temporary = `${path}.${process.pid}.tmp`;
  writeFileSync(temporary, JSON.stringify(state), { mode: 0o600 });
  renameSync(temporary, path);
}

function classifyStripeAuthError(
  text: string,
): "expired" | "auth-failed" | undefined {
  if (/expired_api_key|api key.*expired|expired.*api key/i.test(text))
    return "expired";
  if (
    /invalid_api_key|authentication.*fail|Authorization failed, status=401|invalid api key|401 unauthorized/i.test(
      text,
    )
  )
    return "auth-failed";
  return undefined;
}

// Classify only; never forward raw CLI text into logs or persisted state.
export function classifyStripeOutput(
  line: string,
  secret: string,
): ListenerPhase | "connected" | undefined {
  line = line.trimStart();
  // Stripe CLI v1.50.11 pkg/websocket/client.go emits these at debug level.
  // Match both fields, not payload text: debug output also contains event bodies.
  if (
    /^time="[^"]+" level=debug msg="(?:Disconnected from Stripe|Resetting the connection|Attempting to connect to Stripe|Failed to connect to Stripe\. Retrying\.\.\.)" prefix=websocket\.Client\.Run$/.test(
      line,
    )
  )
    return "disconnected";
  if (
    /^time="[^"]+" level=debug msg="Connected!" prefix=websocket\.Client\.connect$/.test(
      line,
    )
  )
    return "connected";
  // With piped output, the pinned prefixed formatter writes time/level/msg
  // first, then sorted fields. It DOES NOT escape quotes in string values:
  // readPump's message field can contain nested JSON and fake logfmt fields.
  // Only the complete connection records above may affect connection state.
  // The CLI's VisitError logs genuine authentication failures at fatal level;
  // inspect those for auth errors only, never signing secrets or HTTP delivery.
  if (line.startsWith("time=")) {
    const fatal = line.match(/^time="[^"]+" level=fatal msg=([\s\S]*)$/);
    return fatal ? classifyStripeAuthError(fatal[1] ?? "") : undefined;
  }
  const authError = classifyStripeAuthError(line);
  if (authError) return authError;
  const actual = line.match(/whsec_[A-Za-z0-9]+/)?.[0];
  if (actual && actual !== secret) return "secret-mismatch";
  if (actual === secret && /ready/i.test(line)) return "ready";
  if (/\[2\d\d\].*POST.*\/rpc\/stripe\.webhook/i.test(line)) return "delivered";
  if (
    /\[[345]\d\d\].*POST.*\/rpc\/stripe\.webhook|failed to POST|connection refused/i.test(
      line,
    )
  )
    return "delivery-failed";
  return undefined;
}

export interface StripeReadiness {
  ready: boolean;
  deliveryVerified: boolean;
  code: string;
  message: string;
}
export function evaluateStripeReadiness(
  config: StripeConfig,
  state: ListenerState | undefined,
  alive: boolean,
): StripeReadiness {
  const result = (
    code: string,
    message: string,
    ready = false,
    deliveryVerified = false,
  ): StripeReadiness => ({ code, message, ready, deliveryVerified });
  const issue = stripeConfigIssue(config);
  if (issue) return result("configuration", issue);
  if (!state || state.fingerprint !== stripeFingerprint(config))
    return result(
      "not-running",
      "Listener absent or configuration changed. Run mise run stripe:listen.",
    );
  if (state.phase === "expired")
    return result(
      "key-expired",
      "Stripe API key expired (CLI-created keys expire). Run mise run stripe:setup with a fresh sandbox key, reload server/worker and run mise run stripe:listen.",
    );
  if (state.phase === "auth-failed")
    return result(
      "authentication",
      "Stripe rejected the persisted API key. Run mise run stripe:setup with a valid sandbox key.",
    );
  if (state.phase === "secret-mismatch")
    return result(
      "secret-mismatch",
      "Listener signing secret differs from the persisted server secret. Run mise run stripe:setup, reload server/worker and run mise run stripe:listen.",
    );
  if (!alive || state.phase === "stopped")
    return result(
      "not-running",
      "Listener stopped. Run mise run stripe:listen.",
    );
  if (state.phase === "disconnected")
    return result(
      "disconnected",
      "Stripe connection lost or reconnecting. Waiting for the CLI to reconnect; webhook delivery is not verified.",
    );
  if (state.phase === "delivery-failed")
    return result(
      "delivery-failed",
      "Webhook forwarding failed. Check server readiness and signing secret; restart server after setup.",
    );
  if (state.phase === "delivered")
    return result(
      "delivered",
      "Listener ready; Stripe CLI observed a successful webhook HTTP response in this session (not proof of billing side effects).",
      true,
      true,
    );
  if (state.phase === "ready")
    return result(
      "ready",
      "Listener ready with matching signing secret; webhook delivery has NOT yet been verified.",
      true,
    );
  return result(
    "starting",
    "Listener is connecting. Re-run mise run stripe:status shortly.",
  );
}

export function getStripeReadiness(
  root = process.cwd(),
  env = process.env,
): StripeReadiness {
  try {
    const config = persistedStripeConfig(root, env);
    let state: ListenerState | undefined;
    try {
      state = JSON.parse(readFileSync(stripeStatePath(root), "utf8"));
    } catch {
      /* no session */
    }
    let alive = false;
    if (state && Number.isInteger(state.pid) && state.pid > 0) {
      try {
        process.kill(state.pid, 0);
        alive =
          Boolean(state.processStarted) &&
          state.processStarted === listenerProcessStarted(state.pid);
      } catch {
        /* stopped */
      }
    }
    return evaluateStripeReadiness(config, state, alive);
  } catch {
    return {
      ready: false,
      deliveryVerified: false,
      code: "configuration",
      message:
        "Local Stripe configuration or target is invalid. Use a loopback server URL and run mise run stripe:setup.",
    };
  }
}
