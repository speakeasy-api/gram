import { spawn } from "node:child_process";
import { existsSync, mkdirSync } from "node:fs";
import { resolve } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";
import { projectMode } from "./project-mode.ts";
import { hostInfoPath } from "./scenario-host.ts";

/** mcp-auth:all supplies MCP_AUTH_RUN_DIR so its specs share one host and project. */
export default async function setup() {
  console.error(`Project mode: ${projectMode()}`);
  if (process.env.MCP_AUTH_RUN_DIR) return;
  const runDir = resolve(
    import.meta.dirname,
    `../../.logs/mcp-auth/runs/${new Date().toISOString().replaceAll(/[:.]/g, "-")}-${process.pid}`,
  );
  mkdirSync(runDir, { recursive: true, mode: 0o700 });
  const host = spawn(
    process.execPath,
    [
      resolve(import.meta.dirname, "mock-host.ts"),
      runDir,
      "--exit-with-parent",
    ],
    { stdio: ["pipe", "inherit", "inherit"] },
  );
  const deadline = Date.now() + 30_000;
  while (!existsSync(hostInfoPath(runDir))) {
    if (host.exitCode !== null || Date.now() > deadline)
      throw new Error("ENV_MOCK_HOST: mock host did not start");
    await sleep(50);
  }
  process.env.MCP_AUTH_RUN_DIR = runDir;
  return async () => {
    host.kill("SIGTERM");
  };
}
