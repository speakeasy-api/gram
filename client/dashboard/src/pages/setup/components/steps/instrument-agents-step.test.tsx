import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { InstrumentAgentsStep } from "./instrument-agents-step";

vi.mock("@/pages/device-agent/device-agent-setup", () => ({
  DeviceAgentSetup: () => <div>Device agent setup</div>,
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

vi.mock("../marketplace-section", () => ({
  MarketplaceSection: () => null,
}));
vi.mock("../confirm-traffic-section", () => ({
  ConfirmTrafficSection: () => null,
}));

afterEach(cleanup);

describe("InstrumentAgentsStep", () => {
  it("leaves Claude Code and Cowork to the Anthropic observability card", () => {
    render(<InstrumentAgentsStep onComplete={() => {}} onBack={() => {}} />);

    // Radix tabs activate on mousedown, not click.
    fireEvent.mouseDown(screen.getByRole("tab", { name: /Manual Setup/ }), {
      button: 0,
    });

    expect(screen.queryByRole("button", { name: /Claude Cowork/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /Claude Code/ })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /Cursor/ }));
    expect(screen.getByText("Opened platform: cursor")).toBeTruthy();
  });
});
