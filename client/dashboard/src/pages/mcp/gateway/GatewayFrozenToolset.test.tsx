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
it("does not imply changes before review or reconnect an unchanged selection", async () => {
  const review = {
    fingerprint: "same-inventory",
    tools: [tool("one", "v1"), tool("two", "v1"), tool("excluded", "v1")],
  };
  const approved = { review, names: ["two", "one"] };
  const onApply = vi.fn<(value: FrozenGatewayConnection | undefined) => void>();
  state.preview.mockResolvedValueOnce(review);
  renderReview(
    <GatewayFrozenToolset
      enabled
      gatewayId="gateway"
      pending={false}
      approved={approved}
      onApply={onApply}
    />,
  );
  expect(screen.queryByRole("button", { name: /review changes/i })).toBeNull();
  expect(screen.queryByRole("status")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Review toolset" }));
  expect((await screen.findByRole("status")).textContent).toBe(
    "No changes to the frozen toolset.",
  );
  const apply = screen.getByRole<HTMLButtonElement>("button", {
    name: "Freeze 2 tools and reconnect",
  });
  expect(apply.disabled).toBe(true);
  fireEvent.click(apply);
  expect(onApply).not.toHaveBeenCalled();

  fireEvent.click(screen.getByRole("checkbox", { name: "excluded" }));
  expect(screen.queryByRole("status")).toBeNull();
  expect(apply.disabled).toBe(false);
  fireEvent.click(screen.getByRole("checkbox", { name: "excluded" }));
  expect(apply.disabled).toBe(true);
  expect(screen.getByRole("status").textContent).toBe(
    "No changes to the frozen toolset.",
  );
});

it.each([false, true])(
  "keeps failed reconnect recovery available for an unchanged empty freeze (unapplied: %s)",
  async (hasUnappliedFreeze) => {
    const approved = {
      review: { fingerprint: "empty-inventory", tools: [] },
      names: [],
    };
    const onApply =
      vi.fn<(value: FrozenGatewayConnection | undefined) => void>();
    state.preview.mockResolvedValueOnce(approved.review);
    renderReview(
      <GatewayFrozenToolset
        enabled
        gatewayId="gateway"
        pending={false}
        approved={approved}
        hasUnappliedFreeze={hasUnappliedFreeze}
        onApply={onApply}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Review toolset" }));
    const apply = await screen.findByRole<HTMLButtonElement>("button", {
      name: "Freeze 0 tools and reconnect",
    });
    expect(apply.disabled).toBe(!hasUnappliedFreeze);
    fireEvent.click(apply);
    expect(onApply).toHaveBeenCalledTimes(hasUnappliedFreeze ? 1 : 0);
  },
);

it("can reapply the same frozen selection after a failed reconnect", async () => {
  const approved = {
    review: { fingerprint: "inventory", tools: [tool("same", "v1")] },
    names: ["same"],
  };
  const onApply = vi.fn<(value: FrozenGatewayConnection | undefined) => void>();
  const props = {
    gatewayId: "gateway",
    approved,
    onApply,
    enabled: true,
    pending: false,
  };
  state.preview.mockResolvedValueOnce(approved.review);
  const { rerender } = renderReview(<GatewayFrozenToolset {...props} />);
  fireEvent.click(screen.getByRole("button", { name: "Review toolset" }));
  const apply = await screen.findByRole<HTMLButtonElement>("button", {
    name: "Freeze 1 tool and reconnect",
  });
  expect(apply.disabled).toBe(true);

  // A failed live switch or token refresh has no unapplied freeze, but needs
  // to be able to mint the same frozen selection again.
  rerender(<GatewayFrozenToolset {...props} reconnectFailed />);
  expect(screen.queryByRole("status")).toBeNull();
  expect(apply.disabled).toBe(false);
  fireEvent.click(apply);
  expect(onApply).toHaveBeenCalledWith(approved);
});

it("leaves changed and new tools unchecked while preserving unchanged approvals", async () => {
  const onApply = vi.fn<(value: FrozenGatewayConnection | undefined) => void>();
  const old = {
    fingerprint: "old",
    tools: [tool("same", "v1"), tool("changed", "v1"), tool("removed", "v1")],
  };
  renderReview(
    <GatewayFrozenToolset
      enabled={state.enabled}
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
  fireEvent.click(screen.getByRole("button", { name: "Review toolset" }));
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
it("clears an old review before a new preview and keeps an issued freeze visible with the feature disabled", async () => {
  const approved = {
    review: { fingerprint: "old", tools: [tool("same", "v1")] },
    names: ["same"],
  };
  const onApply = vi.fn<(value: FrozenGatewayConnection | undefined) => void>();
  const { rerender } = renderReview(
    <GatewayFrozenToolset
      enabled={state.enabled}
      gatewayId="gateway"
      pending={false}
      approved={approved}
      onApply={onApply}
    />,
  );
  state.preview.mockResolvedValueOnce(approved.review);
  fireEvent.click(screen.getByRole("button", { name: "Review toolset" }));
  await screen.findByRole("button", { name: "Freeze 1 tool and reconnect" });
  expect(
    screen.getByRole("button", { name: "Freeze 1 tool and reconnect" }),
  ).toBeTruthy();
  state.preview.mockRejectedValueOnce(new Error("Inventory unavailable"));
  fireEvent.click(screen.getByRole("button", { name: "Review toolset" }));
  await waitFor(() =>
    expect(screen.getByRole("alert").textContent).toBe("Inventory unavailable"),
  );
  expect(
    screen.queryByRole("button", { name: "Freeze 1 tool and reconnect" }),
  ).toBeNull();
  state.enabled = false;
  rerender(
    <GatewayFrozenToolset
      enabled={state.enabled}
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
        name: "Review toolset",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  fireEvent.click(
    screen.getByRole("button", { name: "Use live tools and reconnect" }),
  );
  expect(onApply).toHaveBeenCalledWith(undefined);
});
it("keeps live recovery available after a first freeze fails and the feature is disabled", () => {
  const onApply = vi.fn<(value: FrozenGatewayConnection | undefined) => void>();
  const props = {
    gatewayId: "gateway",
    approved: undefined,
    hasUnappliedFreeze: true,
    pending: false,
    onApply,
  };
  const { rerender } = renderReview(
    <GatewayFrozenToolset {...props} enabled={state.enabled} />,
  );
  state.enabled = false;
  rerender(<GatewayFrozenToolset {...props} enabled={state.enabled} />);
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
