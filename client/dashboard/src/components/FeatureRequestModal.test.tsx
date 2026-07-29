import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { FeatureRequestModal } from "./FeatureRequestModal";

const telemetryCapture = vi.hoisted(() => vi.fn());

vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: telemetryCapture }),
}));

vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    billing: { href: () => "/billing" },
  }),
}));

afterEach(() => {
  cleanup();
  telemetryCapture.mockReset();
});

describe("FeatureRequestModal", () => {
  it("requires and captures request input in telemetry", () => {
    const onClose = vi.fn();
    render(
      <FeatureRequestModal
        isOpen
        onClose={() => {
          onClose();
        }}
        title="Request an Observability Integration"
        description="Tell us which AI agent your team uses."
        actionType="hooks_agent_integration"
        requestInput={{
          label: "AI agent",
          placeholder: "e.g. GitHub Copilot",
          telemetryField: "requested_agent",
        }}
      />,
    );

    const requestButton = screen.getByRole("button", {
      name: "REQUEST FEATURE",
    });
    expect((requestButton as HTMLButtonElement).disabled).toBe(true);

    fireEvent.change(screen.getByLabelText("AI agent"), {
      target: { value: "GitHub Copilot" },
    });
    expect((requestButton as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(requestButton);

    expect(telemetryCapture).toHaveBeenCalledWith("feature_requested", {
      action: "hooks_agent_integration",
      requested_agent: "GitHub Copilot",
    });
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("clears request input when its parent closes the modal", () => {
    const onClose = vi.fn<() => void>();
    const modal = (isOpen: boolean) => (
      <FeatureRequestModal
        isOpen={isOpen}
        onClose={onClose}
        title="Request an Observability Integration"
        description="Tell us which AI agent your team uses."
        actionType="hooks_agent_integration"
        requestInput={{
          label: "AI agent",
          placeholder: "e.g. GitHub Copilot",
          telemetryField: "requested_agent",
        }}
      />
    );
    const { rerender } = render(modal(true));

    fireEvent.change(screen.getByLabelText("AI agent"), {
      target: { value: "GitHub Copilot" },
    });

    rerender(modal(false));
    rerender(modal(true));

    expect((screen.getByLabelText("AI agent") as HTMLInputElement).value).toBe(
      "",
    );
    expect(
      (
        screen.getByRole("button", {
          name: "REQUEST FEATURE",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });
});
