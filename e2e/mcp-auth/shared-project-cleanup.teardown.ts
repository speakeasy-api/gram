import { rmSync } from "node:fs";
import { resolve } from "node:path";
import { test } from "@playwright/test";
import { Dashboard } from "./dashboard.ts";
import { sharedProject } from "./project-mode.ts";

test("remove the shared project", async ({ page }) => {
  // mcp-auth:all runs each spec separately and removes it after the last one.
  test.skip(process.env.MCP_AUTH_KEEP_SHARED_PROJECT === "1");
  const project = sharedProject();
  if (!project) return;
  const dashboard = new Dashboard(page);
  await dashboard.login();
  dashboard.createdProjects.push(project);
  dashboard.projectURL = project.url;
  await dashboard.removeProject();
  rmSync(resolve(process.env.MCP_AUTH_RUN_DIR!, "shared-project.json"));
});
