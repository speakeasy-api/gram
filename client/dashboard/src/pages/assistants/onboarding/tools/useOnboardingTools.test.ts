import { describe, expect, it, vi } from "vitest";
import {
  isOnboardingToolAllowed,
  type OnboardingToolPermissions,
} from "./useOnboardingTools";

vi.mock("@/contexts/Sdk", () => ({ useSdkClient: vi.fn() }));

const assistantReader: OnboardingToolPermissions = {
  skillsEnabled: true,
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
    };

    expect(isOnboardingToolAllowed("create_trigger", triggerWriter)).toBe(true);
    expect(isOnboardingToolAllowed("create_toolset", triggerWriter)).toBe(
      false,
    );
    expect(isOnboardingToolAllowed("create_environment", triggerWriter)).toBe(
      false,
    );
    expect(isOnboardingToolAllowed("propose_slack_setup", triggerWriter)).toBe(
      false,
    );
  });

  it("exposes cross-resource setup only with every required grant", () => {
    const admin = Object.fromEntries(
      Object.keys(assistantReader).map((key) => [key, true]),
    ) as OnboardingToolPermissions;

    expect(isOnboardingToolAllowed("attach_skill", admin)).toBe(true);
    expect(isOnboardingToolAllowed("create_toolset", admin)).toBe(true);
    expect(isOnboardingToolAllowed("create_environment", admin)).toBe(true);
    expect(isOnboardingToolAllowed("propose_slack_setup", admin)).toBe(true);
  });
});
