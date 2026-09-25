import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactElement } from "react";
import {
  GatewayFrozenToolset,
  type FrozenGatewayConnection,
} from "./GatewayFrozenToolset";
import type { GramGatewayToolsetReview } from "@gram/client/models/components/gramgatewaytoolsetreview.js";

const state = vi.hoisted(() => ({
  enabled: true,
  preview: vi.fn<() => Promise<GramGatewayToolsetReview>>(),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-test" }),
}));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => ({
    data: { gatewayFrozenToolsetsEnabled: state.enabled },
  }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    userSessions: { previewGatewayToolset: state.preview },
  }),
}));
function renderReview(ui: ReactElement) {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  });
  return render(ui, {
    wrapper: ({ children }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    ),
  });
}
afterEach(cleanup);
beforeEach(() => {
  vi.resetAllMocks();
  state.enabled = true;
});
const tool = (name: string, fingerprint: string) => ({
  name,
  fingerprint,
  definition: `{ "name": "${name}", "inputSchema": { "type": "object", "maximum": 9007199254740993 } }`,
});
it("leaves changed and new tools unchecked while preserving unchanged approvals", async () => {
  const onApply = vi.fn<(value: FrozenGatewayConnection | undefined) => void>();
  const old = {
    fingerprint: "old",
    tools: [tool("same", "v1"), tool("changed", "v1"), tool("removed", "v1")],
  };
  renderReview(
    <GatewayFrozenToolset
      gatewayId="gateway"
      pending={false}
      approved={{ review: old, names: old.tools.map((t) => t.name) }}
      onApply={onApply}
    />,
  );
  const next = {
    fingerprint: "new",
    tools: [tool("same", "v1"), tool("changed", "v2"), tool("new", "v1")],
  };
  state.preview.mockResolvedValueOnce(next);
  fireEvent.click(screen.getByRole("button", { name: "Review changes" }));
  await screen.findByRole("checkbox", { name: "same" });
  expect(state.preview).toHaveBeenCalledWith({ metaMcpServerId: "gateway" });
  expect(
    screen.getByRole("checkbox", { name: "same" }).getAttribute("aria-checked"),
  ).toBe("true");
  expect(
    screen
      .getByRole("checkbox", { name: "changed" })
      .getAttribute("aria-checked"),
  ).toBe("false");
  expect(
    screen.getByRole("checkbox", { name: "new" }).getAttribute("aria-checked"),
  ).toBe("false");
  expect(screen.getByText("No longer available: removed")).toBeTruthy();
  expect(screen.getAllByText(/9007199254740993/).length).toBeGreaterThan(0);
  fireEvent.click(
    screen.getByRole("button", { name: "Freeze 1 tool and reconnect" }),
  );
  expect(onApply).toHaveBeenCalledWith({ review: next, names: ["same"] });
  fireEvent.click(screen.getByRole("checkbox", { name: "same" }));
  fireEvent.click(
    screen.getByRole("button", { name: "Freeze 0 tools and reconnect" }),
  );
  expect(onApply).toHaveBeenLastCalledWith({ review: next, names: [] });
});
it("clears an old review before a new preview and keeps an issued freeze visible with the flag off", async () => {
  const approved = {
    review: { fingerprint: "old", tools: [tool("same", "v1")] },
    names: ["same"],
  };
  const onApply = vi.fn<(value: FrozenGatewayConnection | undefined) => void>();
  const { rerender } = renderReview(
    <GatewayFrozenToolset
      gatewayId="gateway"
      pending={false}
      approved={approved}
      onApply={onApply}
    />,
  );
  state.preview.mockResolvedValueOnce(approved.review);
  fireEvent.click(screen.getByRole("button", { name: "Review changes" }));
  await screen.findByRole("button", { name: "Freeze 1 tool and reconnect" });
  expect(
    screen.getByRole("button", { name: "Freeze 1 tool and reconnect" }),
  ).toBeTruthy();
  state.preview.mockRejectedValueOnce(new Error("Inventory unavailable"));
  fireEvent.click(screen.getByRole("button", { name: "Review changes" }));
  await waitFor(() =>
    expect(screen.getByRole("alert").textContent).toBe("Inventory unavailable"),
  );
  expect(
    screen.queryByRole("button", { name: "Freeze 1 tool and reconnect" }),
  ).toBeNull();
  state.enabled = false;
  rerender(
    <GatewayFrozenToolset
      gatewayId="gateway"
      pending={false}
      approved={approved}
      onApply={onApply}
    />,
  );
  expect(screen.getByText("1 tool frozen for this connection")).toBeTruthy();
  expect(
    (
      screen.getByRole("button", {
        name: "Review changes",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  fireEvent.click(
    screen.getByRole("button", { name: "Use live tools and reconnect" }),
  );
  expect(onApply).toHaveBeenCalledWith(undefined);
});
it("keeps live recovery available after a first freeze fails and the feature is disabled", () => {
  const onApply = vi.fn();
  const props = {
    gatewayId: "gateway",
    approved: undefined,
    hasUnappliedFreeze: true,
    pending: false,
    onApply,
  };
  const { rerender } = renderReview(<GatewayFrozenToolset {...props} />);
  state.enabled = false;
  rerender(<GatewayFrozenToolset {...props} />);
  expect(
    screen.getByText("Freeze not applied to this connection"),
  ).toBeTruthy();
  expect(
    (
      screen.getByRole("button", {
        name: "Review and freeze",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  fireEvent.click(
    screen.getByRole("button", { name: "Use live tools and reconnect" }),
  );
  expect(onApply).toHaveBeenCalledWith(undefined);
});
