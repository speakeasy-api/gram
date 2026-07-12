#!/usr/bin/env -S node

//MISE description="Stream assistant runtime logs from local Docker and Fly.io runtimes"
//MISE hide=true
//USAGE flag "--poll-seconds <seconds>" default="3" help="How often to poll for active assistant runtimes."

import process from "node:process";
import { spawn, type ChildProcessByStdio } from "node:child_process";
import type { Readable } from "node:stream";

type RuntimeRow = {
  runtime_id: string;
  thread_id: string;
  backend: string;
  state: string;
  app_name: string;
  machine_id: string;
  container_name: string;
};

type LogStreamProcess = ChildProcessByStdio<null, Readable, Readable>;

type Subscriber = {
  runtime: RuntimeRow;
  proc: LogStreamProcess;
  stopped: boolean;
};

const pollSeconds = Math.max(
  1,
  Number.parseInt(process.env["usage_poll_seconds"] ?? "3", 10) || 3,
);
// Prefer a user-scoped token for log streaming since deploy tokens returned
// by `fly tokens create deploy` lack `logs:read` and 401 here.
const flyAccessToken =
  process.env["GRAM_ASSISTANT_RUNTIME_FLYCTL_LOGS_TOKEN"] ||
  process.env["GRAM_ASSISTANT_RUNTIME_FLYIO_API_TOKEN"] ||
  "";
const flyAppNamePrefix =
  process.env["GRAM_ASSISTANT_RUNTIME_FLYIO_APP_NAME_PREFIX"] || "gram-asst";
const databaseURL = process.env["GRAM_DATABASE_URL"];

if (!databaseURL) {
  console.error(
    "[watcher]: GRAM_DATABASE_URL is required to stream assistant runtime logs.",
  );
  process.exit(1);
}

const subscribers = new Map<string, Subscriber>();
let shuttingDown = false;
let waitingForRuntimes = false;
let lastQueryError = "";

function psqlConnection() {
  const url = new URL(databaseURL!);
  const searchPath = url.searchParams.get("search_path") || "";
  url.searchParams.delete("search_path");

  const env = { ...process.env };
  if (searchPath) {
    env["PGOPTIONS"] = [env["PGOPTIONS"], `-c search_path=${searchPath}`]
      .filter(Boolean)
      .join(" ");
  }

  return {
    databaseURL: url.toString(),
    env,
  };
}

function linePrefix(runtime: RuntimeRow): string {
  return (
    runtime.container_name ||
    runtime.app_name ||
    `assistant-${runtime.thread_id.split("-")[0]}`
  );
}

function flyAppName(runtime: RuntimeRow): string {
  if (runtime.app_name) {
    return runtime.app_name;
  }
  return `${flyAppNamePrefix}-${runtime.thread_id.toLowerCase()}`;
}

function writeLine(prefix: string, line: string) {
  process.stdout.write(`[${prefix}]: ${line}\n`);
}

function attachLinePrefixer(
  runtime: RuntimeRow,
  proc: LogStreamProcess,
  stream: NodeJS.ReadableStream,
) {
  const prefix = linePrefix(runtime);
  let buffer = "";

  stream.on("data", (chunk: Buffer | string) => {
    buffer += chunk.toString();
    while (true) {
      const newline = buffer.indexOf("\n");
      if (newline === -1) {
        break;
      }
      const line = buffer.slice(0, newline).replace(/\r$/, "");
      buffer = buffer.slice(newline + 1);
      writeLine(prefix, line);
    }
  });

  const flush = () => {
    const line = buffer.replace(/\r$/, "");
    if (line) {
      writeLine(prefix, line);
    }
    buffer = "";
  };

  stream.on("end", flush);
  proc.on("close", flush);
}

function sameTarget(left: RuntimeRow, right: RuntimeRow): boolean {
  return (
    left.backend === right.backend &&
    left.thread_id === right.thread_id &&
    left.app_name === right.app_name &&
    left.machine_id === right.machine_id &&
    left.container_name === right.container_name
  );
}

function spawnFlySubscriber(runtime: RuntimeRow): LogStreamProcess {
  const appName = flyAppName(runtime);
  const args = ["logs", "-a", appName];
  if (runtime.machine_id) {
    args.push("--machine", runtime.machine_id);
  }
  return spawn("flyctl", args, {
    env: {
      ...process.env,
      ...(flyAccessToken ? { FLY_ACCESS_TOKEN: flyAccessToken } : {}),
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
}

function spawnDockerSubscriber(runtime: RuntimeRow): LogStreamProcess {
  return spawn(
    "docker",
    ["logs", "--follow", "--tail", "25", runtime.container_name],
    {
      env: process.env,
      stdio: ["ignore", "pipe", "pipe"],
    },
  );
}

function spawnSubscriber(runtime: RuntimeRow): LogStreamProcess {
  switch (runtime.backend) {
    case "flyio":
      return spawnFlySubscriber(runtime);
    case "local":
      return spawnDockerSubscriber(runtime);
    default:
      return spawn(
        "bash",
        ["-lc", `echo "unsupported backend ${runtime.backend}"`],
        {
          env: process.env,
          stdio: ["ignore", "pipe", "pipe"],
        },
      );
  }
}

function stopSubscriber(runtimeID: string, reason?: string) {
  const sub = subscribers.get(runtimeID);
  if (!sub) {
    return;
  }
  sub.stopped = true;
  subscribers.delete(runtimeID);
  sub.proc.kill("SIGTERM");
  if (reason) {
    writeLine(linePrefix(sub.runtime), reason);
  }
}

function startSubscriber(runtime: RuntimeRow) {
  const proc = spawnSubscriber(runtime);
  const sub: Subscriber = {
    runtime,
    proc,
    stopped: false,
  };
  subscribers.set(runtime.runtime_id, sub);

  attachLinePrefixer(runtime, proc, proc.stdout);
  attachLinePrefixer(runtime, proc, proc.stderr);

  writeLine(
    linePrefix(runtime),
    runtime.backend === "local"
      ? `attached docker logs for ${runtime.container_name}`
      : `attached fly logs for ${flyAppName(runtime)}`,
  );

  const scheduleRetry = (reason: string) => {
    const current = subscribers.get(runtime.runtime_id);
    if (current !== sub) {
      return;
    }
    if (sub.stopped || shuttingDown) {
      subscribers.delete(runtime.runtime_id);
      return;
    }
    writeLine(linePrefix(runtime), reason);
    subscribers.delete(runtime.runtime_id);
    setTimeout(() => {
      if (shuttingDown) {
        return;
      }
      const desired = desiredRuntimes.get(runtime.runtime_id);
      if (!desired) {
        return;
      }
      startSubscriber(desired);
    }, 2000);
  };

  // A missing/broken CLI (docker, flyctl) surfaces as a spawn "error" with no
  // "close"; without a handler it would crash the whole watcher.
  proc.on("error", (err) => {
    scheduleRetry(`log stream failed to start (${err.message}), retrying...`);
  });

  proc.on("close", (code, signal) => {
    scheduleRetry(
      `log stream exited (${signal ?? code ?? "unknown"}), retrying...`,
    );
  });
}

const desiredRuntimes = new Map<string, RuntimeRow>();
// Local runtimes whose container is currently absent, so the wait is
// announced once instead of on every poll/retry.
const missingContainers = new Set<string>();

// Names of running local containers, or null when docker itself is
// unreachable (treat as unknown rather than "everything is gone").
async function listLocalContainers(): Promise<Set<string> | null> {
  return new Promise((resolve) => {
    const proc = spawn("docker", ["ps", "--format", "{{.Names}}"], {
      env: process.env,
      stdio: ["ignore", "pipe", "pipe"],
    });
    let stdout = "";
    proc.stdout.on("data", (chunk: Buffer | string) => {
      stdout += chunk.toString();
    });
    proc.on("error", () => resolve(null));
    proc.on("close", (code) => {
      if (code !== 0) {
        resolve(null);
        return;
      }
      resolve(
        new Set(
          stdout
            .split("\n")
            .map((line) => line.trim())
            .filter(Boolean),
        ),
      );
    });
  });
}

async function loadRuntimes(): Promise<RuntimeRow[]> {
  const sql = `
WITH runtimes AS (
  SELECT
    r.id::text AS runtime_id,
    r.assistant_thread_id::text AS thread_id,
    r.backend,
    r.state,
    COALESCE(r.backend_metadata_json->>'app_name', '') AS app_name,
    COALESCE(r.backend_metadata_json->>'machine_id', '') AS machine_id,
    COALESCE(r.backend_metadata_json->>'container_name', '') AS container_name
  FROM assistant_runtimes r
  WHERE r.deleted IS FALSE
    AND r.state IN ('starting', 'active')
  ORDER BY r.created_at ASC
)
SELECT COALESCE(json_agg(runtimes), '[]'::json)::text
FROM runtimes;
`.trim();

  const connection = psqlConnection();
  const proc = spawn(
    "psql",
    [
      "-X",
      "-A",
      "-t",
      "-v",
      "ON_ERROR_STOP=1",
      "-d",
      connection.databaseURL,
      "-c",
      sql,
    ],
    {
      env: connection.env,
      stdio: ["ignore", "pipe", "pipe"],
    },
  );

  let stdout = "";
  let stderr = "";
  proc.stdout.on("data", (chunk: Buffer | string) => {
    stdout += chunk.toString();
  });
  proc.stderr.on("data", (chunk: Buffer | string) => {
    stderr += chunk.toString();
  });

  const code = await new Promise<number>((resolve, reject) => {
    proc.on("error", reject);
    proc.on("close", (exitCode) => resolve(exitCode ?? 1));
  });

  if (code !== 0) {
    throw new Error(stderr.trim() || `psql exited with status ${code}`);
  }

  const raw = stdout.trim() || "[]";
  const parsed = JSON.parse(raw) as RuntimeRow[] | null;
  return parsed ?? [];
}

async function reconcileOnce() {
  const runtimes = (await loadRuntimes()).filter(
    (runtime) => runtime.backend === "flyio" || runtime.backend === "local",
  );
  desiredRuntimes.clear();
  for (const runtime of runtimes) {
    desiredRuntimes.set(runtime.runtime_id, runtime);
  }

  if (runtimes.length === 0) {
    if (!waitingForRuntimes) {
      writeLine("watcher", "waiting for active assistant runtimes...");
      waitingForRuntimes = true;
    }
  } else {
    waitingForRuntimes = false;
  }

  // A runtime row can outlive its container (assistant deleted, container
  // manually removed) until the backend reaps it. Attaching `docker logs`
  // to a gone container exits immediately and the retry loop turns that
  // into log spam, so resolve existence once per poll and hold off until
  // the container is actually there.
  const localContainers = runtimes.some(
    (runtime) => runtime.backend === "local",
  )
    ? await listLocalContainers()
    : null;

  for (const [runtimeID, runtime] of desiredRuntimes) {
    // Runtime rows start with empty backend metadata and get populated after
    // Ensure. Wait for the backend's identity field so we attach a single
    // targeted stream instead of the generic fallback, then reattach — avoids
    // the duplicate log stream we were getting while the old fallback
    // subscriber dies.
    if (runtime.backend === "flyio" && !runtime.app_name) {
      continue;
    }
    if (runtime.backend === "local" && !runtime.container_name) {
      continue;
    }
    if (
      runtime.backend === "local" &&
      localContainers !== null &&
      !localContainers.has(runtime.container_name)
    ) {
      if (subscribers.has(runtimeID)) {
        stopSubscriber(runtimeID, "container gone, detaching...");
      }
      if (!missingContainers.has(runtimeID)) {
        missingContainers.add(runtimeID);
        writeLine(
          linePrefix(runtime),
          `container ${runtime.container_name} not running; waiting for it...`,
        );
      }
      continue;
    }
    missingContainers.delete(runtimeID);
    const existing = subscribers.get(runtimeID);
    if (!existing) {
      startSubscriber(runtime);
      continue;
    }
    if (!sameTarget(existing.runtime, runtime)) {
      stopSubscriber(runtimeID, "runtime target changed, reconnecting...");
      startSubscriber(runtime);
    } else {
      existing.runtime = runtime;
    }
  }

  for (const runtimeID of [...subscribers.keys()]) {
    if (!desiredRuntimes.has(runtimeID)) {
      stopSubscriber(runtimeID, "runtime no longer active, detaching...");
    }
  }
  for (const runtimeID of [...missingContainers]) {
    if (!desiredRuntimes.has(runtimeID)) {
      missingContainers.delete(runtimeID);
    }
  }
}

function shutdown(signal: string) {
  if (shuttingDown) {
    return;
  }
  shuttingDown = true;
  writeLine("watcher", `shutting down on ${signal}`);
  for (const runtimeID of [...subscribers.keys()]) {
    stopSubscriber(runtimeID);
  }
  setTimeout(() => process.exit(0), 100).unref();
}

process.on("SIGINT", () => shutdown("SIGINT"));
process.on("SIGTERM", () => shutdown("SIGTERM"));

while (!shuttingDown) {
  try {
    await reconcileOnce();
    lastQueryError = "";
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    if (message !== lastQueryError) {
      writeLine("watcher", `failed to query assistant runtimes: ${message}`);
      lastQueryError = message;
    }
  }
  await new Promise((resolve) => setTimeout(resolve, pollSeconds * 1000));
}
