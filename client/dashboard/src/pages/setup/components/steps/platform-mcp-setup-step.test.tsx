import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { StepSupportProvider } from "../step-container";
import { PlatformMCPSetupStep } from "./platform-mcp-setup-step";

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) => children,
}));
vi.mock("@/pages/org/PlatformMCP", () => ({
  PlatformMCPOnboardingContent: () => <div>Platform MCP setup</div>,
}));

afterEach(cleanup);

describe("PlatformMCPSetupStep", () => {
  it("places the shared support action immediately before Complete", () => {
    const onSupport = vi.fn();
    render(
      <StepSupportProvider onSupport={onSupport}>
        <PlatformMCPSetupStep
          onComplete={() => {}}
          onBack={() => {}}
          onSkip={() => {}}
          continueLabel="Complete"
        />
      </StepSupportProvider>,
    );

    const support = screen.getByRole("button", { name: "Get support" });
    const complete = screen.getByRole("button", { name: "Complete" });
    expect(support.nextElementSibling).toBe(complete);

    fireEvent.click(support);
    expect(onSupport).toHaveBeenCalledOnce();
  });
});
