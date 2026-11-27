// @vitest-environment node
import { expect, it } from "vitest";
import { buildSystemPrompt } from "./systemPrompt";

const snapshot = {
  name: "Robin",
  model: "anthropic/claude-opus-5",
  status: "active",
  instructions: "Help the team.",
  toolsets: [{ slug: "legacy-tools" }],
  mcpServers: [{ slug: "legacy-server" }],
  skills: [],
};

it("guides Kit assistants through Project Gateway access and built-in Slack setup", () => {
  const prompt = buildSystemPrompt({
    mode: "edit",
    snapshot,
    usesProjectMCPAccess: true,
  });

  expect(prompt).toContain("Overview tab");
  expect(prompt).toContain("Project Gateway");
  expect(prompt).toContain("propose_slack_setup");
  expect(prompt).toContain("request_slack_credentials");
  expect(prompt).toContain(
    "MCP server credentials are managed on the server itself",
  );
  expect(prompt).not.toContain("attach_toolset");
  expect(prompt).not.toContain("attach_mcp_server");
  expect(prompt).not.toContain("legacy-tools");
  expect(prompt).not.toContain("legacy-server");
});

it("keeps legacy attachment guidance for non-Kit assistants", () => {
  const prompt = buildSystemPrompt({
    mode: "edit",
    snapshot,
    usesProjectMCPAccess: false,
  });

  expect(prompt).toContain("attach_toolset");
  expect(prompt).toContain("legacy-tools");
  expect(prompt).toContain("legacy-server");
});
