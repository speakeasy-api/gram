import { resolve } from "node:path";
import { readJSONFile, writeJSONFile } from "./scenario-store.ts";

export type ProjectMode = "isolated" | "shared";
export type OwnedProject = { name: string; url: string };

export function projectMode(): ProjectMode {
  const mode = process.env.MCP_AUTH_PROJECT_MODE ?? "isolated";
  if (mode !== "isolated" && mode !== "shared")
    throw new Error('MCP_AUTH_PROJECT_MODE must be "isolated" or "shared"');
  return mode;
}

// Playwright restarts its worker after a failure and mcp-auth:all runs each
// spec separately, so the shared project is recorded outside the process.
function sharedProjectPath() {
  const runDir = process.env.MCP_AUTH_RUN_DIR;
  if (!runDir) throw new Error("ENV_MOCK_HOST: MCP_AUTH_RUN_DIR is not set");
  return resolve(runDir, "shared-project.json");
}

export function sharedProject() {
  return readJSONFile<OwnedProject>(sharedProjectPath());
}

export function recordSharedProject(project: OwnedProject) {
  writeJSONFile(sharedProjectPath(), project);
}
