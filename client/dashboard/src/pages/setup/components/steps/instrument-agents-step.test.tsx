import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { InstrumentAgentsStep } from "./instrument-agents-step";

vi.mock("@/pages/device-agent/device-agent-setup", () => ({
  DeviceAgentOsPicker: ({
    value,
    onChange,
  }: {
    value: string;
    onChange: (os: string) => void;
  }) => (
    <div>
      {["macos", "windows", "linux"].map((os) => (
        <button
          key={os}
          aria-pressed={os === value}
          onClick={() => onChange(os)}
        >
          {os}
        </button>
      ))}
    </div>
  ),
  DeviceAgentInstallStep: ({ os }: { os: string }) => (
    <div>Installer for {os}</div>
  ),
}));
vi.mock("../marketplace-section", () => ({
  MarketplaceSection: () => null,
}));
vi.mock("../confirm-traffic-section", () => ({
  ConfirmTrafficSection: () => null,
}));
vi.mock("../mdm-rollout-table", () => ({
  MdmRolloutTable: () => <div>MDM rollout table</div>,
}));
vi.mock("../mdm-rollout-requirements", () => ({
  MdmRolloutRequirements: () => <div>MDM rollout requirements</div>,
}));

afterEach(cleanup);

describe("InstrumentAgentsStep", () => {
  it("switches the inline installer with the platform tiles, no sheet or fork", () => {
    render(<InstrumentAgentsStep onComplete={() => {}} onBack={() => {}} />);

    expect(screen.getByText("Download installer")).toBeTruthy();
    expect(screen.getByText("Installer for macos")).toBeTruthy();
    expect(screen.queryByRole("tab")).toBeNull();
    expect(screen.queryByRole("dialog")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "windows" }));

    expect(screen.getByText("Installer for windows")).toBeTruthy();
    expect(screen.queryByText("Installer for macos")).toBeNull();
  });

  it("recommends the MDM rollout and offers no per-platform manual setup", () => {
    render(<InstrumentAgentsStep onComplete={() => {}} onBack={() => {}} />);

    expect(screen.getByText("MDM rollout")).toBeTruthy();
    expect(screen.getByText("Recommended")).toBeTruthy();
    expect(screen.getByText("MDM rollout requirements")).toBeTruthy();
    expect(screen.getByText("MDM rollout table")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Cursor/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /Codex/ })).toBeNull();
  });
});
