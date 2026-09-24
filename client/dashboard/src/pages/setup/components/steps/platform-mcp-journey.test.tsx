import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { JourneyStepsProvider } from "../journey-steps-provider";
import { useJourneyView } from "../journey-steps";
import { OnboardingStepper } from "../onboarding-stepper";
import { PlatformMCPSetupStep } from "./platform-mcp-setup-step";

const state = vi.hoisted(() => ({ clientFamily: "claude_code" }));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-example" }),
}));
vi.mock("@/hooks/useOrganizationPlatformMCPOnboarding", () => ({
  useOrganizationPlatformMCPOnboarding: () => ({
    data: { enabled: true, clientFamily: state.clientFamily },
  }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) => children,
}));
vi.mock("@/pages/org/PlatformMCP", () => ({
  PlatformMCPOnboardingContent: () => <div>Shared setup body</div>,
}));
afterEach(() => {
  cleanup();
  state.clientFamily = "claude_code";
});

function Rail() {
  const { steps, activeIndex, setActiveIndex } = useJourneyView();
  const location = useLocation();
  return (
    <>
      <OnboardingStepper
        steps={steps.map((step) => ({
          id: String(step.index),
          title: step.title,
          description: "",
          badge: step.badge,
        }))}
        currentStep={steps.findIndex((step) => step.index === activeIndex)}
        onStepClick={(index) => setActiveIndex(steps[index]!.index)}
      />
      <output>{location.search}</output>
    </>
  );
}
function setup(search = "") {
  const onComplete = vi.fn<() => void>();
  render(
    <MemoryRouter initialEntries={[`/speakeasy/setup/platform-mcp${search}`]}>
      <JourneyStepsProvider>
        <Rail />
        <main>
          <PlatformMCPSetupStep onComplete={onComplete} />
        </main>
      </JourneyStepsProvider>
    </MemoryRouter>,
  );
  return onComplete;
}
describe("Platform MCP numbered journey", () => {
  it("registers both numbered steps, supports keyboard rail navigation and keeps completion optional", () => {
    const onComplete = setup();
    const rail = screen.getByRole("navigation", { name: "Progress" });
    expect(within(rail).getByText("1")).toBeTruthy();
    expect(within(rail).getByText("2")).toBeTruthy();
    expect(within(rail).getByText("Optional")).toBeTruthy();
    expect(
      within(screen.getByRole("main")).getAllByRole("heading", {
        name: "Set up Platform MCP",
      }),
    ).toHaveLength(1);
    expect(screen.queryByRole("button", { name: "Copy prompt" })).toBeNull();
    fireEvent.keyDown(
      within(rail).getByRole("button", {
        name: /Import existing MCP servers in Claude/,
      }),
      { key: "Enter" },
    );
    expect(
      within(screen.getByRole("main")).getAllByRole("heading", {
        name: "Import existing MCP servers in Claude",
      }),
    ).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Copy prompt" })).toBeTruthy();
    expect(screen.getByText("?step=add-existing-mcp-servers")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    expect(
      within(screen.getByRole("main")).getByRole("heading", {
        name: "Set up Platform MCP",
      }),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Next step" }));
    fireEvent.click(screen.getByRole("button", { name: "Mark done" }));
    expect(onComplete).toHaveBeenCalledOnce();
  });
  it("preserves a visible keyboard focus ring on both navigation controls", () => {
    setup();
    for (const name of ["Next step", "Back"]) {
      const button = screen.getByRole("button", { name });
      button.focus();
      expect(document.activeElement).toBe(button);
      for (const utility of [
        "focus-visible:ring-2",
        "focus-visible:ring-offset-3",
        "focus-visible:ring-[var(--border-focus)]",
        "focus-visible:ring-offset-[var(--bg-surface-primary-default)]",
      ]) {
        expect(button.classList.contains(utility)).toBe(true);
      }
      expect(button.classList.contains("focus-visible:ring-0")).toBe(false);
      fireEvent.click(button);
    }
  });
  it("supports a direct link to the optional step", () => {
    setup("?step=add-existing-mcp-servers");
    expect(
      screen.getByRole("heading", { level: 1, name: "Platform MCP" }),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("button", { name: "Back" })
        .classList.contains("md:hidden"),
    ).toBe(true);
    expect(screen.queryByRole("button", { name: "Next step" })).toBeNull();
    expect(
      screen
        .getByRole("button", { name: "Mark done" })
        .hasAttribute("disabled"),
    ).toBe(false);
    expect(
      within(screen.getByRole("main")).getByRole("heading", {
        name: "Import existing MCP servers in Claude",
      }),
    ).toBeTruthy();
    expect(
      within(screen.getByRole("main")).queryByRole("heading", {
        name: "Set up Platform MCP",
      }),
    ).toBeNull();
  });
  it("keeps step one registered and completion available without a supported client", () => {
    state.clientFamily = "cursor";
    const onComplete = setup("?step=add-existing-mcp-servers");
    expect(
      within(screen.getByRole("main")).getByRole("heading", {
        name: "Set up Platform MCP",
      }),
    ).toBeTruthy();
    expect(
      screen.queryByText("Import existing MCP servers in Claude"),
    ).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Mark done" }));
    expect(onComplete).toHaveBeenCalledOnce();
  });
});
