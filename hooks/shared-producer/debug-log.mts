import { appendFile, mkdir } from "node:fs/promises";

import os from "node:os";
import path from "node:path";

const DEBUG_FLAG_ENV = "GRAM_HOOKS_DEBUG";
const DEBUG_LOG_PATH_ENV = "GRAM_HOOKS_DEBUG_LOG_PATH";

function normalizeFlag(value: string | undefined): boolean {
  if (!value) {
    return false;
  }

  const normalized = value.trim().toLowerCase();
  return (
    normalized === "1" ||
    normalized === "true" ||
    normalized === "yes" ||
    normalized === "on"
  );
}

function resolveLogPath(env: NodeJS.ProcessEnv = process.env): string {
  const explicitPath = env[DEBUG_LOG_PATH_ENV]?.trim();
  if (explicitPath) {
    return explicitPath;
  }

  return path.join(os.homedir(), ".gram", "hooks-debug.log");
}

export function isHookDebugEnabled(
  env: NodeJS.ProcessEnv = process.env,
): boolean {
  return normalizeFlag(env[DEBUG_FLAG_ENV]);
}

export async function logHookDebug(
  component: string,
  event: string,
  data?: unknown,
): Promise<void> {
  if (!isHookDebugEnabled()) {
    return;
  }

  const logPath = resolveLogPath();
  const record = {
    ts: new Date().toISOString(),
    component,
    event,
    pid: process.pid,
    data: data ?? null,
  };

  try {
    await mkdir(path.dirname(logPath), { recursive: true, mode: 0o700 });
    await appendFile(logPath, `${JSON.stringify(record)}\n`, {
      encoding: "utf8",
      mode: 0o600,
    });
  } catch {
    // fail-open: debug logging must never affect hook flow
  }
}
