import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { OrganizationGateway } from "@gram/client/models/components/organizationgateway.js";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { OrganizationGatewayDiscoverySection } from "./OrganizationGatewayDiscoverySection";

const state = vi.hoisted(() => ({
  pending: false,
  mutate: vi.fn(),
  scope: vi.fn(),
  invalidate: vi.fn().mockResolvedValue(undefined),
  success: undefined as undefined | (() => Promise<void>),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: (props: { children: React.ReactNode; scope: string }) => {
    state.scope(props);
    return props.children;
  },
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
}));
vi.mock("@gram/client/react-query/organizationGateway.js", () => ({
  invalidateAllOrganizationGateway: () => state.invalidate(),
}));
vi.mock(
  "@gram/client/react-query/setOrganizationGatewayDiscoveryMode.js",
  () => ({
    useSetOrganizationGatewayDiscoveryModeMutation: ({
      onSuccess,
    }: {
      onSuccess: () => Promise<void>;
    }) => {
      state.success = onSuccess;
      return { mutate: state.mutate, isPending: state.pending, isError: false };
    },
  }),
);

const gateway: OrganizationGateway = {
  available: true,
  url: "https://app.example.test/mcp/gateway/test",
  discoveryMode: "progressive",
  codeModeEnabled: false,
};
const show = (
  overrides: Partial<OrganizationGateway> = {},
  { codeModeEntitled = true as boolean | undefined, canManage = true } = {},
) =>
  render(
    <TooltipProvider>
      <OrganizationGatewayDiscoverySection
        gateway={{ ...gateway, ...overrides }}
        codeModeEntitled={codeModeEntitled}
        canManage={canManage}
        isCurrentOrganization={() => true}
      />
    </TooltipProvider>,
  );
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.pending = false;
});

it("saves the organization gateway's default mode and refreshes the gateway", async () => {
  show();
  fireEvent.click(screen.getByRole("combobox"));
  fireEvent.click(await screen.findByRole("option", { name: "Direct" }));
  fireEvent.click(screen.getByRole("button", { name: /save/i }));
  expect(state.mutate).toHaveBeenCalledWith({
    request: {
      setGatewayDiscoveryModeRequestBody: { discoveryMode: "direct" },
    },
  });
  await act(async () => state.success?.());
  expect(state.invalidate).toHaveBeenCalled();
});

it("shows the saved default and keeps it read-only for members who cannot manage the gateway", () => {
  show({ discoveryMode: "direct" }, { canManage: false });
  const select = screen.getByRole("combobox") as HTMLButtonElement;
  expect(select.disabled).toBe(true);
  expect(select.textContent).toContain("Direct");
  expect(
    (screen.getByRole("button", { name: /save/i }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  expect(state.scope).toHaveBeenCalledWith(
    expect.objectContaining({ scope: "org:admin" }),
  );
});

it("only lets Code Mode be chosen when the organization can use it", async () => {
  const view = show();
  fireEvent.click(screen.getByRole("combobox"));
  expect(
    screen
      .getByRole("option", { name: "Code Mode (unavailable)" })
      .getAttribute("aria-disabled"),
  ).toBe("true");
  view.unmount();
  show({ codeModeEnabled: true });
  fireEvent.click(screen.getByRole("combobox"));
  fireEvent.click(await screen.findByRole("option", { name: "Code Mode" }));
  fireEvent.click(screen.getByRole("button", { name: /save/i }));
  expect(state.mutate).toHaveBeenCalledWith({
    request: {
      setGatewayDiscoveryModeRequestBody: { discoveryMode: "code_mode" },
    },
  });
});

it("shows Code Mode disabled with the not-enabled tooltip to an organization without it", async () => {
  show({}, { codeModeEntitled: false });
  fireEvent.click(screen.getByRole("combobox"));
  const option = screen.getByRole("option", {
    name: "Code Mode (not enabled)",
  });
  expect(option.getAttribute("aria-disabled")).toBe("true");
  await userEvent.hover(option.parentElement!);
  expect(
    (await screen.findAllByText(/Contact\s+Speakeasy to turn it on/i)).length,
  ).toBeGreaterThan(0);
});

it("explains that a saved Code Mode default serves Progressive while Code Mode is unavailable", () => {
  show({ discoveryMode: "code_mode", codeModeEnabled: false });
  expect(screen.getByText(/The code runtime is unavailable/i)).toBeTruthy();
  expect(
    screen.getByText(/connections currently use Progressive discovery/i),
  ).toBeTruthy();
});

it("follows a refreshed default instead of keeping a stale draft", async () => {
  const view = show();
  fireEvent.click(screen.getByRole("combobox"));
  fireEvent.click(await screen.findByRole("option", { name: "Direct" }));
  view.rerender(
    <TooltipProvider>
      <OrganizationGatewayDiscoverySection
        gateway={{ ...gateway, discoveryMode: "direct" }}
        codeModeEntitled={true}
        canManage={true}
        isCurrentOrganization={() => true}
      />
    </TooltipProvider>,
  );
  view.rerender(
    <TooltipProvider>
      <OrganizationGatewayDiscoverySection
        gateway={{ ...gateway, discoveryMode: "progressive" }}
        codeModeEntitled={true}
        canManage={true}
        isCurrentOrganization={() => true}
      />
    </TooltipProvider>,
  );
  expect(screen.getByRole("combobox").textContent).toContain("Progressive");
  expect(
    (screen.getByRole("button", { name: /save/i }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
});

it("does not blame the runtime while the Code Mode entitlement is unknown", () => {
  render(
    <TooltipProvider>
      <OrganizationGatewayDiscoverySection
        gateway={{ ...gateway, discoveryMode: "code_mode" }}
        codeModeEntitled={undefined}
        canManage={true}
        isCurrentOrganization={() => true}
      />
    </TooltipProvider>,
  );
  expect(screen.queryByText(/code runtime is unavailable/i)).toBeNull();
  expect(
    screen.getByText(/Code Mode is unavailable for your organization/i),
  ).toBeTruthy();
});
