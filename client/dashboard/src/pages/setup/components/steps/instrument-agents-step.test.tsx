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

afterEach(cleanup);

describe("InstrumentAgentsStep", () => {
  it("warns that Cowork needs manual setup and opens its instructions", () => {
    render(<InstrumentAgentsStep onComplete={() => {}} onBack={() => {}} />);

    expect(screen.getByText("Cowork needs separate setup")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Set it up manually" }));

    expect(screen.getByText("Opened platform: claude-cowork")).toBeTruthy();
  });
});
