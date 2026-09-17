import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { StepSupportProvider } from "../step-container";
import { PlatformMCPSetupStep } from "./platform-mcp-setup-step";

const followUp = vi.hoisted(() => ({ status: "not started" }));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
    scope,
  }: {
    children: React.ReactNode;
    scope: string;
  }) => (
    <div data-testid="scope-boundary" data-scope={scope}>
      {children}
    </div>
  ),
}));
vi.mock("@/pages/org/PlatformMCP", () => ({
  PlatformMCPOnboardingContent: ({
    onSetupComplete,
  }: {
    onSetupComplete: () => void;
  }) => (
    <div>
      Platform MCP setup
      <button onClick={onSetupComplete}>Finish shared setup</button>
    </div>
  ),
}));

vi.mock("../add-existing-mcp-servers", () => ({
  AddExistingMCPServers: ({
    currentProjectSlug,
  }: {
    currentProjectSlug?: string;
  }) =>
    followUp.status === "hidden" ? null : (
      <div
        data-testid="existing-mcp-follow-up"
        data-project={currentProjectSlug}
      >
        Add existing MCP servers: {followUp.status}
      </div>
    ),
}));

beforeEach(() => {
  followUp.status = "not started";
});
afterEach(cleanup);

describe("PlatformMCPSetupStep", () => {
  it("forwards the destination and places the follow-up immediately after shared setup inside admin scope", () => {
    render(
      <PlatformMCPSetupStep
        currentProjectSlug="example-project"
        onComplete={() => {}}
      />,
    );

    const setup = screen.getByText("Platform MCP setup");
    const panel = screen.getByTestId("existing-mcp-follow-up");
    expect(panel.getAttribute("data-project")).toBe("example-project");
    expect(setup.nextElementSibling).toBe(panel);
    expect(panel.parentElement).toBe(screen.getByTestId("scope-boundary"));
    expect(panel.parentElement?.getAttribute("data-scope")).toBe("org:admin");
  });

  it.each(["hidden", "not started", "skipped", "partial failure", "complete"])(
    "keeps Mark done independent of the follow-up (%s)",
    (status) => {
      followUp.status = status;
      const onComplete = vi.fn<() => void>();
      render(
        <PlatformMCPSetupStep
          currentProjectSlug="example-project"
          onComplete={onComplete}
        />,
      );

      expect(onComplete).not.toHaveBeenCalled();
      fireEvent.click(screen.getByRole("button", { name: "Mark done" }));
      expect(onComplete).toHaveBeenCalledOnce();
    },
  );

  it("preserves the shared onboarding completion callback", () => {
    const onComplete = vi.fn<() => void>();
    render(<PlatformMCPSetupStep onComplete={onComplete} />);

    fireEvent.click(
      screen.getByRole("button", { name: "Finish shared setup" }),
    );
    expect(onComplete).toHaveBeenCalledOnce();
  });

  it("places the shared support action immediately before Mark done", () => {
    const onSupport = vi.fn();
    render(
      <StepSupportProvider onSupport={() => void onSupport()}>
        <PlatformMCPSetupStep onComplete={() => {}} />
      </StepSupportProvider>,
    );

    const support = screen.getByRole("button", { name: "Get support" });
    const complete = screen.getByRole("button", { name: "Mark done" });
    expect(support.nextElementSibling).toBe(complete);

    fireEvent.click(support);
    expect(onSupport).toHaveBeenCalledOnce();
  });
});
