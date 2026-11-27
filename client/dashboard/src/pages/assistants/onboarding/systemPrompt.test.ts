import { expect, it } from "vitest";
import { getIntegrationDoc } from "./docs";
import { buildSystemPrompt } from "./systemPrompt";

it("removes legacy write instructions from Kit setup while retaining Slack and the Default flow", () => {
  const snapshot = {
    name: "Casey",
    model: "model",
    status: "active",
    instructions: "",
    toolsets: [{ slug: "legacy" }],
    mcpServers: [],
    skills: [],
  };
  const kit = buildSystemPrompt({
    mode: "edit",
    snapshot,
    usesProjectMCPAccess: true,
  });
  expect(kit).not.toMatch(
    /\b(?:attach_mcp_server|detach_mcp_server|attach_toolset|detach_toolset|create_toolset|add_tools_to_toolset)\b/,
  );
  expect(kit).not.toContain("`legacy`");
  expect(kit).toContain("direct Connect grant");
  expect(kit).toContain("propose_slack_setup");
  expect(buildSystemPrompt({ mode: "edit", snapshot })).toContain(
    "attach_toolset",
  );
  expect(getIntegrationDoc("slack", true)?.body).toContain(
    "request_environment_secrets",
  );
  expect(getIntegrationDoc("slack", true)?.body).toContain(
    "send a test message in Slack",
  );
});
