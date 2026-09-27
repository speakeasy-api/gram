import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import type { PlatformMCPOnboardingState } from "@gram/client/models/components/platformmcponboardingstate.js";
import { PlatformMCPSetupSheet } from "./PlatformMCP";
import { TooltipProvider } from "@/components/ui/Tooltip";

vi.mock("./platform-mcp-install-walkthrough", () => ({
  PlatformMCPInstallWalkthrough: () => <div>Install instructions</div>,
}));

const incompleteState = {
  connectionAuthorized: false,
  catalogExplored: false,
  registrationComplete: false,
  readinessVerified: false,
  distributionAttached: false,
  mcpUrl: "https://example.com/mcp/platform",
} as PlatformMCPOnboardingState;

function setup(state = incompleteState) {
  const onDone = vi.fn();
  const onDismiss = vi.fn();
  const props = {
    open: true,
    onOpenChange: vi.fn(),
    state,
    canAdminister: true,
    activeClient: {
      id: "claude_code" as const,
      label: "Claude Code",
      description: "Anthropic CLI & IDE agent",
    },
    installMethod: "manual" as const,
    isMutating: false,
    setupError: null,
    onBackToInstallMethod: vi.fn(),
    onConfigurationCopied: vi.fn(),
    onContinueSecureSetup: vi.fn(),
    onDismiss,
    onDone,
  };
  const view = render(
    <TooltipProvider>
      <PlatformMCPSetupSheet {...props} />
    </TooltipProvider>,
  );
  return { ...view, props, onDone, onDismiss };
}

const button = (name: string) =>
  screen.getByRole("button", { name }) as HTMLButtonElement;

const skip = () => fireEvent.click(button("Skip this step"));

afterEach(cleanup);

describe("Platform MCP setup guide", () => {
  it("skips unused MCP steps and finishes without invoking evidence callbacks", () => {
    const { onDone, onDismiss, props } = setup();
    expect(button("Next").disabled).toBe(true);
    skip(); // Install and authenticate
    expect(screen.getByText("Explore the MCP Catalogue")).toBeTruthy();
    skip(); // Explore the MCP Catalogue
    expect(screen.getByText("Register the selected MCP")).toBeTruthy();
    skip(); // Register the selected MCP
    expect(screen.getByText("Finish setting up the MCP")).toBeTruthy();
    skip(); // Finish setting up the MCP
    expect(screen.getByText("Add it to the Default plugin")).toBeTruthy();
    expect(button("Next").disabled).toBe(true);
    fireEvent.click(button("Skip and finish guide"));
    expect(onDone).toHaveBeenCalledOnce();
    expect(onDismiss).not.toHaveBeenCalled();
    expect(props.onConfigurationCopied).not.toHaveBeenCalled();
    expect(props.onContinueSecureSetup).not.toHaveBeenCalled();
  });

  it("keeps a verified connection while skipping MCP registration and distribution", () => {
    const { onDone } = setup({
      ...incompleteState,
      connectionAuthorized: true,
    });
    expect(screen.queryByRole("button", { name: "Skip this step" })).toBeNull();
    fireEvent.click(button("Next"));
    skip(); // Explore the MCP Catalogue
    skip(); // Register the selected MCP
    skip(); // Finish setting up the MCP
    fireEvent.click(button("Skip and finish guide"));
    expect(onDone).toHaveBeenCalledOnce();
  });

  it("keeps the usual Next and Done path when each step has evidence", () => {
    const state = {
      ...incompleteState,
      connectionAuthorized: true,
      catalogExplored: true,
      registrationComplete: true,
      readinessVerified: true,
      distributionAttached: true,
    };
    const { onDone } = setup(state);
    expect(screen.queryByRole("button", { name: "Skip this step" })).toBeNull();
    for (let index = 0; index < 4; index++) {
      fireEvent.click(button("Next"));
    }
    expect(screen.getByText("Setup complete")).toBeTruthy();
    fireEvent.click(button("Done"));
    expect(onDone).toHaveBeenCalledOnce();
  });

  it("does not keep skips when the guide is reopened", () => {
    const { rerender, props } = setup();
    skip();
    rerender(
      <TooltipProvider>
        <PlatformMCPSetupSheet {...props} open={false} />
      </TooltipProvider>,
    );
    rerender(
      <TooltipProvider>
        <PlatformMCPSetupSheet {...props} />
      </TooltipProvider>,
    );
    expect(screen.getByText("Install and authenticate")).toBeTruthy();
    expect(button("Next").disabled).toBe(true);
    expect(button("Skip this step")).toBeTruthy();
  });
});
