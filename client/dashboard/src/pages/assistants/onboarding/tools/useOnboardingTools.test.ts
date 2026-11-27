// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import {
  isOnboardingToolAllowed,
  type OnboardingToolPermissions,
} from "./useOnboardingTools";

vi.mock("@/contexts/Sdk", () => ({ useSdkClient: vi.fn() }));

const assistantReader: OnboardingToolPermissions = {
  canWriteAssistant: false,
  canReadProject: false,
  canWriteProject: false,
  canReadMCP: false,
  canWriteMCP: false,
  canReadSkills: false,
  canWriteEnvironment: false,
};

describe("isOnboardingToolAllowed", () => {
  it("keeps assistant-only read workflows while suppressing other resources", () => {
    expect(isOnboardingToolAllowed("finish_onboarding", assistantReader)).toBe(
      true,
    );
    expect(isOnboardingToolAllowed("list_triggers", assistantReader)).toBe(
      false,
    );
    expect(isOnboardingToolAllowed("list_toolsets", assistantReader)).toBe(
      false,
    );
    expect(isOnboardingToolAllowed("list_skills", assistantReader)).toBe(false);
  });

  it("keeps trigger permissions independent from MCP and environment writes", () => {
    const triggerWriter = {
      ...assistantReader,
      canWriteAssistant: true,
      canReadProject: true,
      canWriteProject: true,
      canWriteEnvironment: true,
    };

    expect(isOnboardingToolAllowed("create_trigger", triggerWriter)).toBe(true);
    expect(isOnboardingToolAllowed("create_toolset", triggerWriter)).toBe(
      false,
    );
    expect(isOnboardingToolAllowed("create_environment", triggerWriter)).toBe(
      true,
    );
    expect(isOnboardingToolAllowed("update_assistant", triggerWriter)).toBe(
      true,
    );
    expect(isOnboardingToolAllowed("attach_toolset", triggerWriter)).toBe(true);
    expect(isOnboardingToolAllowed("create_trigger", triggerWriter)).toBe(true);
    expect(isOnboardingToolAllowed("propose_slack_setup", triggerWriter)).toBe(
      true,
    );
    expect(
      isOnboardingToolAllowed("request_slack_credentials", triggerWriter),
    ).toBe(true);
  });

  it.each(["update_assistant", "attach_toolset", "create_trigger"] as const)(
    "requires environment write for %s",
    (tool) => {
      expect(
        isOnboardingToolAllowed(tool, {
          ...assistantReader,
          canWriteAssistant: true,
          canReadProject: true,
          canWriteProject: true,
          canReadMCP: true,
          canWriteMCP: true,
        }),
      ).toBe(false);
    },
  );

  it("exposes cross-resource setup only with every required grant", () => {
    const admin = Object.fromEntries(
      Object.keys(assistantReader).map((key) => [key, true]),
    ) as OnboardingToolPermissions;

    expect(isOnboardingToolAllowed("attach_skill", admin)).toBe(true);
    expect(isOnboardingToolAllowed("create_toolset", admin)).toBe(true);
    expect(isOnboardingToolAllowed("create_environment", admin)).toBe(true);
    expect(isOnboardingToolAllowed("propose_slack_setup", admin)).toBe(true);
  });

  it("hides legacy MCP attachment tools for Kit assistants while keeping Slack and MCP reads", () => {
    const kitAdmin: OnboardingToolPermissions = {
      usesProjectMCPAccess: true,
      canWriteAssistant: true,
      canReadProject: true,
      canWriteProject: true,
      canReadMCP: true,
      canWriteMCP: true,
      canReadSkills: true,
      canWriteEnvironment: true,
    };

    for (const tool of [
      "attach_mcp_server",
      "detach_mcp_server",
      "attach_toolset",
      "detach_toolset",
      "create_toolset",
      "add_tools_to_toolset",
    ] as const) {
      expect(isOnboardingToolAllowed(tool, kitAdmin)).toBe(false);
    }
    expect(isOnboardingToolAllowed("list_mcp_servers", kitAdmin)).toBe(true);
    expect(isOnboardingToolAllowed("propose_slack_setup", kitAdmin)).toBe(true);
    expect(isOnboardingToolAllowed("request_slack_credentials", kitAdmin)).toBe(
      true,
    );
  });
});
