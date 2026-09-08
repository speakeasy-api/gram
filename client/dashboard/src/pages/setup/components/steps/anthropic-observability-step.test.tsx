import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AnthropicObservabilityStep } from "./anthropic-observability-step";

const publishStatus = vi.hoisted(() => ({
  current: {
    data: { connected: false } as Record<string, unknown>,
    isLoading: false,
  },
}));

vi.mock("@gram/client/react-query/publishStatus", () => ({
  usePublishStatus: () => publishStatus.current,
}));
vi.mock("../marketplace-section", () => ({
  MarketplaceSection: () => <div>Marketplace section</div>,
}));
vi.mock("../confirm-traffic-section", () => ({
  ConfirmTrafficSection: () => <div>Confirm traffic section</div>,
}));
vi.mock("../platform-instrumentation-sheet", () => ({
  PlatformInstrumentationSheet: ({
    open,
    initialPlatformId,
  }: {
    open: boolean;
    initialPlatformId?: string;
  }) => (open ? <div>Opened platform: {initialPlatformId}</div> : null),
}));

afterEach(cleanup);

beforeEach(() => {
  publishStatus.current = { data: { connected: false }, isLoading: false };
});

function renderStep() {
  return render(
    <AnthropicObservabilityStep onComplete={() => {}} onBack={() => {}} />,
  );
}

describe("AnthropicObservabilityStep", () => {
  it("covers Claude Code and Cowork with marketplace and traffic sections", () => {
    publishStatus.current = {
      data: {
        connected: true,
        repoUrl: "https://github.com/acme/acme-plugins",
      },
      isLoading: false,
    };

    renderStep();

    expect(screen.getByText("Set up Anthropic observability")).toBeTruthy();
    expect(screen.getByText("Marketplace section")).toBeTruthy();
    expect(screen.getByText("Confirm traffic section")).toBeTruthy();
    expect(screen.getByText("0 of 2 connected")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /Claude Code/ }));
    expect(screen.getByText("Opened platform: claude")).toBeTruthy();
    expect(screen.getByText("Connect Cursor")).toBeTruthy();
    expect(screen.getByText("Optional")).toBeTruthy();
  });

  it("holds both sets of instructions until the marketplace is published", () => {
    renderStep();

    const buttons = [
      screen.getByRole("button", { name: /Claude Code/ }),
      screen.getByRole("button", { name: /Claude Cowork/ }),
      screen.getByRole("button", { name: /Cursor/ }),
    ] as HTMLButtonElement[];
    expect(buttons.every((button) => button.disabled)).toBe(true);
    expect(
      screen.getByText(/Publish the marketplace above first/),
    ).toBeTruthy();
  });
});
