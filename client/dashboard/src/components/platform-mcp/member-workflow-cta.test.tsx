import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { MemberWorkflowCTA } from "./member-workflow-cta";
import type { ReactNode } from "react";

const state = vi.hoisted(() => ({
  hasScope: vi.fn(),
  capture: vi.fn(),
  refetch: vi.fn(),
  queryEnabled: false,
  onboarding: {
    data: {
      enabled: true,
      connectionAuthorized: false,
      connectionAuthState: "unauthorized",
    },
    isError: false,
  },
  setupOpen: false,
  onSetupComplete: undefined as undefined | (() => void),
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-1" }),
}));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: state.capture }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: state.hasScope, isLoading: false, error: null }),
}));
vi.mock("@/hooks/useOrganizationPlatformMCPOnboarding", () => ({
  useOrganizationPlatformMCPOnboarding: (
    _id: string,
    options: { enabled: boolean },
  ) => {
    state.queryEnabled = options.enabled;
    return { ...state.onboarding, refetch: state.refetch };
  },
}));
vi.mock("@/pages/org/PlatformMCP", () => ({
  PlatformMCPOnboardingContent: ({
    setupOpen,
    onSetupComplete,
  }: {
    setupOpen: boolean;
    onSetupComplete: () => void;
  }) => {
    state.setupOpen = setupOpen;
    state.onSetupComplete = onSetupComplete;
    return null;
  },
}));
vi.mock("@/components/ui/Dialog", () => ({
  Dialog: Object.assign(
    ({ open, children }: { open: boolean; children: ReactNode }) =>
      open ? <div data-testid="prompt-dialog">{children}</div> : null,
    {
      Content: ({ children }: { children: ReactNode }) => <div>{children}</div>,
      Header: ({ children }: { children: ReactNode }) => <div>{children}</div>,
      Title: ({ children }: { children: ReactNode }) => <h2>{children}</h2>,
      Description: ({ children }: { children: ReactNode }) => <p>{children}</p>,
    },
  ),
}));
vi.mock("@/components/ui/CopyButton", () => ({
  CopyButton: ({ text }: { text: string }) => (
    <button type="button" data-testid="copy-prompt" data-text={text}>
      Copy
    </button>
  ),
}));

const props = {
  workflow: "skill_improve" as const,
  label: "Improve with your agent",
  description: "Improve this skill",
  prompt: "Review skill ID skill-1 without saving.",
  scope: "skill:write" as const,
  resourceId: "project-1",
  projectSlug: "project-slug",
};

beforeEach(() => {
  vi.clearAllMocks();
  state.hasScope.mockImplementation(
    (scope: string, resource?: string) =>
      scope === "skill:write" && resource === "project-1",
  );
  state.onboarding.data = {
    enabled: true,
    connectionAuthorized: false,
    connectionAuthState: "unauthorized",
  };
  state.onboarding.isError = false;
  state.setupOpen = false;
  state.onSetupComplete = undefined;
});
afterEach(cleanup);

describe("MemberWorkflowCTA", () => {
  it("only promotes enabled workflows to eligible members", () => {
    const { rerender } = render(<MemberWorkflowCTA {...props} />);
    expect(state.hasScope).toHaveBeenCalledWith("skill:write", "project-1");
    expect(state.queryEnabled).toBe(true);
    expect(
      screen.getByRole("button", { name: "Connect your agent" }),
    ).toBeTruthy();
    expect(state.capture).toHaveBeenCalledWith("platform_mcp_member_cta", {
      action: "impression",
      workflow: "skill_improve",
    });

    state.hasScope.mockReturnValue(false);
    rerender(<MemberWorkflowCTA {...props} />);
    expect(state.queryEnabled).toBe(false);
    expect(
      screen.queryByRole("button", { name: "Connect your agent" }),
    ).toBeNull();

    state.hasScope.mockImplementation((scope: string) => scope === "org:admin");
    rerender(<MemberWorkflowCTA {...props} />);
    expect(state.queryEnabled).toBe(false);
    expect(
      screen.queryByRole("button", { name: "Connect your agent" }),
    ).toBeNull();

    state.hasScope.mockImplementation(
      (scope: string) => scope === "skill:write",
    );
    state.onboarding.isError = true;
    rerender(<MemberWorkflowCTA {...props} />);
    expect(
      screen.queryByRole("button", { name: "Connect your agent" }),
    ).toBeNull();
  });

  it("records one impression per resource and a selection when clicked", () => {
    const { rerender } = render(<MemberWorkflowCTA {...props} />);
    rerender(<MemberWorkflowCTA {...props} />);
    expect(state.capture).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "Connect your agent" }));
    expect(state.capture).toHaveBeenLastCalledWith("platform_mcp_member_cta", {
      action: "selected",
      workflow: "skill_improve",
    });
    expect(state.setupOpen).toBe(true);
    expect(screen.queryByTestId("prompt-dialog")).toBeNull();
    act(() => state.onSetupComplete?.());
    expect(state.refetch).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("copy-prompt").getAttribute("data-text")).toBe(
      props.prompt,
    );
    state.hasScope.mockImplementation(
      (scope: string, resource?: string) =>
        scope === "skill:write" && resource === "project-2",
    );
    rerender(<MemberWorkflowCTA {...props} resourceId="project-2" />);
    expect(state.capture).toHaveBeenCalledTimes(3);
  });

  it("opens the prompt directly for an active connection", () => {
    state.onboarding.data = {
      enabled: true,
      connectionAuthorized: true,
      connectionAuthState: "active",
    };
    render(<MemberWorkflowCTA {...props} />);
    fireEvent.click(
      screen.getByRole("button", { name: "Improve with your agent" }),
    );
    expect(state.setupOpen).toBe(false);
    expect(screen.getByTestId("copy-prompt").getAttribute("data-text")).toBe(
      props.prompt,
    );
  });
});
