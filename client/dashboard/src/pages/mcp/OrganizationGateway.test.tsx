import userEvent from "@testing-library/user-event";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";

const state = vi.hoisted(() => ({
  setOn: vi.fn(),
  error: false,
  on: false as boolean | undefined,
}));
vi.mock("@/components/page-templates", () => ({
  SettingsPage: ({
    children,
    primaryAction,
    renderHeader,
  }: {
    children: ReactNode;
    primaryAction: ReactNode;
    renderHeader?: (header: ReactNode) => ReactNode;
  }) => (
    <TooltipProvider>
      <div>
        {renderHeader ? renderHeader(primaryAction) : primaryAction}
        {children}
      </div>
    </TooltipProvider>
  ),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "test-org", slug: "test" }),
}));
vi.mock("@/hooks/useIsCurrentOrganization", () => ({
  useIsCurrentOrganization: () => () => true,
}));
vi.mock("@/components/mcp/useOrganizationGatewaySetting", () => ({
  useOrganizationGatewaySetting: () => ({
    features: { data: {}, error: state.error },
    gateway: {
      data: state.on
        ? { available: true, url: "https://api.example.test/mcp/gateway/test" }
        : { available: false, unavailableReason: "turned_off" },
    },
    on: state.on,
    setOn: state.setOn,
    isPending: false,
  }),
}));
vi.mock("@/components/mcp/organization-gateway-setting", () => ({
  OrganizationGatewaySwitch: ({ setOn }: { setOn: (on: boolean) => void }) => (
    <button onClick={() => setOn(true)}>Turn on</button>
  ),
  GatewayNotServed: () => null,
}));
vi.mock("@/components/mcp/OrganizationGatewayCard", () => ({
  SpeakeasyMark: () => null,
}));
vi.mock("@/pages/setup/components/platform-instrumentation-sheet", () => ({
  PlatformInstrumentationSheet: () => null,
}));
vi.mock("../access/MCPAssignments", () => ({
  MCPAssignments: ({ onReady }: { onReady?: (ready: boolean) => void }) => (
    <button onClick={() => onReady?.(true)}>Assignments loaded</button>
  ),
}));
import OrganizationGateway from "./OrganizationGateway";
afterEach(cleanup);
beforeEach(() => {
  state.error = false;
  state.on = false;
  vi.clearAllMocks();
});

it("routes the MCP page switch through access review before mutating", () => {
  render(<OrganizationGateway />);
  fireEvent.click(screen.getByRole("button", { name: "Turn on" }));
  expect(state.setOn).not.toHaveBeenCalled();
  const enable = screen.getByRole("button", {
    name: "Enable Organization Gateway",
  }) as HTMLButtonElement;
  expect(enable.disabled).toBe(true);
  fireEvent.click(screen.getByRole("button", { name: "Assignments loaded" }));
  fireEvent.click(screen.getByRole("checkbox"));
  fireEvent.click(enable);
  expect(state.setOn).toHaveBeenCalledWith(
    true,
    expect.objectContaining({ onSuccess: expect.any(Function) }),
  );
});

it("does not offer mutation when the gateway setting failed to load", () => {
  state.error = true;
  render(<OrganizationGateway />);
  expect(screen.queryByRole("button", { name: "Turn on" })).toBeNull();
  expect(screen.getByText(/load the gateway setting/)).toBeDefined();
});

it.each([false, true])(
  "shows assignments from the gateway card destination when enabled=%s",
  async (on) => {
    state.on = on;
    render(<OrganizationGateway />);
    expect(
      screen
        .getByRole("tab", { name: on ? "Install" : "View assignments" })
        .getAttribute("aria-selected"),
    ).toBe("true");
    await userEvent.click(
      screen.getByRole("tab", { name: "View assignments" }),
    );
    expect(
      screen.getByRole("button", { name: "Assignments loaded" }),
    ).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Enable Organization Gateway" }),
    ).toBeNull();
  },
);

it("keeps the copyable endpoint above both tabs", async () => {
  state.on = true;
  render(<OrganizationGateway />);
  expect(screen.getByRole("region", { name: "Gateway endpoint" })).toBeTruthy();
  expect(
    screen.getByText("https://api.example.test/mcp/gateway/test"),
  ).toBeTruthy();
  expect(screen.getByRole("button", { name: "Copy endpoint" })).toBeTruthy();
  await userEvent.click(screen.getByRole("tab", { name: "View assignments" }));
  expect(
    screen.getByText("https://api.example.test/mcp/gateway/test"),
  ).toBeTruthy();
  expect(screen.queryByRole("heading", { name: "Install" })).toBeNull();
});

it("selects assignments after a disabled setting loads, preserving an explicit tab choice", async () => {
  state.on = undefined;
  const { rerender } = render(<OrganizationGateway />);
  state.on = false;
  rerender(<OrganizationGateway />);
  expect(
    screen
      .getByRole("tab", { name: "View assignments" })
      .getAttribute("aria-selected"),
  ).toBe("true");
  await userEvent.click(screen.getByRole("tab", { name: "Install" }));
  rerender(<OrganizationGateway />);
  expect(
    screen.getByRole("tab", { name: "Install" }).getAttribute("aria-selected"),
  ).toBe("true");
});
