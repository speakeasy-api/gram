import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { unrecognized } from "@gram/client/types/unrecognized.js";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { GatewayDiscoverySection } from "./GatewayDiscoverySection";

const state = vi.hoisted(() => ({
  codeEnabled: false,
  entitled: true,
  canWrite: true,
  pending: false,
  mutate: vi.fn(),
  scope: vi.fn(),
  invalidate: vi.fn().mockResolvedValue(undefined),
  success: undefined as undefined | (() => Promise<void>),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => state.canWrite }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: (props: {
    children: React.ReactNode;
    scope: string;
    resourceId: string;
  }) => {
    state.scope(props);
    return props.children;
  },
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-test" }),
}));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => ({ data: { codeModeEnabled: state.entitled } }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ invalidateQueries: state.invalidate }),
}));
vi.mock("@gram/client/react-query/getMetaMcpServer.js", () => ({
  invalidateAllGetMetaMcpServer: () => state.invalidate("get"),
}));
vi.mock("@gram/client/react-query/metaMcpServers.js", () => ({
  invalidateAllMetaMcpServers: () => state.invalidate("list"),
}));
vi.mock("@gram/client/react-query/updateMetaMcpServer.js", () => ({
  useUpdateMetaMcpServerMutation: ({
    onSuccess,
  }: {
    onSuccess: () => Promise<void>;
  }) => {
    state.success = onSuccess;
    return { mutate: state.mutate, isPending: state.pending, isError: false };
  },
}));

const gateway = {
  id: "gateway-test",
  projectId: "project-test",
  name: "Gateway",
  discoveryMode: "progressive",
  userSessionIssuerId: "issuer-test",
} as MetaMcpServer;
const show = () =>
  render(
    <TooltipProvider>
      <GatewayDiscoverySection
        metaMcpServer={{
          ...gateway,
          codeModeEnabled: state.codeEnabled,
        }}
      />
    </TooltipProvider>,
  );
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.codeEnabled = false;
  state.entitled = true;
  state.canWrite = true;
  state.pending = false;
});

describe("Gateway discovery settings", () => {
  it("saves the selected mode and refreshes gateway readers", async () => {
    show();
    fireEvent.click(screen.getByRole("combobox"));
    fireEvent.click(await screen.findByRole("option", { name: "Direct" }));
    fireEvent.click(screen.getByRole("button", { name: /save/i }));
    expect(state.mutate).toHaveBeenCalledWith({
      request: {
        updateMetaMcpServerForm: {
          id: gateway.id,
          name: gateway.name,
          discoveryMode: "direct",
        },
      },
    });
    await act(async () => state.success?.());
    expect(state.invalidate).toHaveBeenCalledWith({
      queryKey: ["gatewayInspection"],
    });
    expect(state.invalidate).toHaveBeenCalledWith("get");
    expect(state.invalidate).toHaveBeenCalledWith("list");
  });
  it("disables controls for a reader and supplies project scope feedback", () => {
    state.canWrite = false;
    show();
    expect((screen.getByRole("combobox") as HTMLButtonElement).disabled).toBe(
      true,
    );
    expect(
      (screen.getByRole("button", { name: /save/i }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    expect(state.scope).toHaveBeenCalledWith(
      expect.objectContaining({
        scope: "mcp:write",
        resourceId: gateway.projectId,
      }),
    );
  });
  it("disables controls during a save", async () => {
    const { rerender } = show();
    fireEvent.click(screen.getByRole("combobox"));
    fireEvent.click(await screen.findByRole("option", { name: "Direct" }));
    expect(
      (screen.getByRole("button", { name: /save/i }) as HTMLButtonElement)
        .disabled,
    ).toBe(false);
    state.pending = true;
    rerender(
      <TooltipProvider>
        <GatewayDiscoverySection metaMcpServer={gateway} />
      </TooltipProvider>,
    );
    expect((screen.getByRole("combobox") as HTMLButtonElement).disabled).toBe(
      true,
    );
    expect(
      (screen.getByRole("button", { name: /sav/i }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });
});

it.each(["future_mode", "constructor", "__proto__"])(
  "keeps %s discovery mode read-only",
  (value) => {
    render(
      <TooltipProvider>
        <GatewayDiscoverySection
          metaMcpServer={{ ...gateway, discoveryMode: unrecognized(value) }}
        />
      </TooltipProvider>,
    );
    const select = screen.getByRole("combobox") as HTMLButtonElement;
    expect(select.disabled).toBe(true);
    expect(select.textContent).toContain("Unknown mode");
    expect(screen.getByText(/Refresh the page before changing/)).toBeTruthy();
    const save = screen.getByRole("button", {
      name: /save/i,
    }) as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    fireEvent.click(save);
    expect(state.mutate).not.toHaveBeenCalled();
  },
);

it("blocks a selected draft when refreshed discovery becomes unsupported", async () => {
  const { rerender } = show();
  fireEvent.click(screen.getByRole("combobox"));
  fireEvent.click(await screen.findByRole("option", { name: "Direct" }));
  rerender(
    <TooltipProvider>
      <GatewayDiscoverySection
        metaMcpServer={{
          ...gateway,
          discoveryMode: unrecognized("future_mode"),
        }}
      />
    </TooltipProvider>,
  );
  const save = screen.getByRole("button", {
    name: /save/i,
  }) as HTMLButtonElement;
  expect(save.disabled).toBe(true);
  expect(screen.getByRole("combobox").textContent).toContain("Unknown mode");
  fireEvent.click(save);
  expect(state.mutate).not.toHaveBeenCalled();
});

it("allows changing a direct gateway to progressive", async () => {
  render(
    <TooltipProvider>
      <GatewayDiscoverySection
        metaMcpServer={{ ...gateway, discoveryMode: "direct" }}
      />
    </TooltipProvider>,
  );
  fireEvent.click(screen.getByRole("combobox"));
  fireEvent.click(await screen.findByRole("option", { name: "Progressive" }));
  fireEvent.click(screen.getByRole("button", { name: /save/i }));
  expect(state.mutate).toHaveBeenCalledWith(
    expect.objectContaining({
      request: {
        updateMetaMcpServerForm: {
          id: gateway.id,
          name: gateway.name,
          discoveryMode: "progressive",
        },
      },
    }),
  );
});

it("only offers Python when code mode is available", async () => {
  const view = show();
  fireEvent.click(screen.getByRole("combobox"));
  expect(screen.queryByRole("option", { name: "Code Mode" })).toBeNull();
  view.unmount();
  state.codeEnabled = true;
  show();
  fireEvent.click(screen.getByRole("combobox"));
  fireEvent.click(await screen.findByRole("option", { name: "Code Mode" }));
  fireEvent.click(screen.getByRole("button", { name: /save/i }));
  expect(state.mutate).toHaveBeenCalledWith({
    request: {
      updateMetaMcpServerForm: {
        id: gateway.id,
        name: gateway.name,
        discoveryMode: "code_mode",
      },
    },
  });
});

it("explains Progressive fallback for a saved Code gateway", () => {
  render(
    <TooltipProvider>
      <GatewayDiscoverySection
        metaMcpServer={{
          ...gateway,
          discoveryMode: "code_mode",
          codeModeEnabled: false,
        }}
      />
    </TooltipProvider>,
  );
  expect(
    screen.getByText(/connections currently use Progressive discovery/i),
  ).toBeTruthy();
});

it("disables Code Mode for a gateway that allows anonymous callers", () => {
  render(
    <TooltipProvider>
      <GatewayDiscoverySection
        metaMcpServer={{
          ...gateway,
          userSessionIssuerId: undefined,
          codeModeEnabled: false,
        }}
      />
    </TooltipProvider>,
  );
  fireEvent.click(screen.getByRole("combobox"));
  const option = screen.getByRole("option", {
    name: "Code Mode (requires sign-in)",
  });
  expect(option.getAttribute("aria-disabled")).toBe("true");
});

it("never offers Code Mode to an organization without it", () => {
  state.entitled = false;
  render(
    <TooltipProvider>
      <GatewayDiscoverySection
        metaMcpServer={{
          ...gateway,
          userSessionIssuerId: undefined,
          codeModeEnabled: false,
        }}
      />
    </TooltipProvider>,
  );
  fireEvent.click(screen.getByRole("combobox"));
  expect(screen.queryByRole("option", { name: /Code Mode/ })).toBeNull();
});

it("explains a saved Code gateway in an organization without Code Mode", () => {
  state.entitled = false;
  render(
    <TooltipProvider>
      <GatewayDiscoverySection
        metaMcpServer={{
          ...gateway,
          discoveryMode: "code_mode",
          codeModeEnabled: false,
        }}
      />
    </TooltipProvider>,
  );
  expect(
    screen.getByText(/Code Mode is not enabled for your organization/i),
  ).toBeTruthy();
});
