import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { GatewayFrozenToolset } from "./GatewayFrozenToolset";
import type { GramGatewayToolsetReview } from "@gram/client/models/components/gramgatewaytoolsetreview.js";

const state = vi.hoisted(() => ({
  onSuccess: undefined as
    | undefined
    | ((review: GramGatewayToolsetReview) => void),
  enabled: true,
  mutate: vi.fn(),
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: state.enabled ? "enabled" : "disabled" }),
}));
vi.mock("@gram/client/react-query/previewGatewayToolset.js", () => ({
  usePreviewGatewayToolsetMutation: ({
    onSuccess,
  }: {
    onSuccess: (review: GramGatewayToolsetReview) => void;
  }) => {
    state.onSuccess = onSuccess;
    return { mutate: state.mutate, isPending: false, isError: false };
  },
}));
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.enabled = true;
});
const tool = (name: string, fingerprint: string) => ({
  name,
  fingerprint,
  definition: { name, inputSchema: { type: "object" } },
});
it("leaves changed and new tools unchecked while preserving unchanged approvals", () => {
  const onApply = vi.fn();
  const old = {
    fingerprint: "old",
    tools: [tool("same", "v1"), tool("changed", "v1"), tool("removed", "v1")],
  };
  render(
    <GatewayFrozenToolset
      gatewayId="gateway"
      pending={false}
      approved={{ review: old, names: old.tools.map((t) => t.name) }}
      onApply={onApply}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Review changes" }));
  const next = {
    fingerprint: "new",
    tools: [tool("same", "v1"), tool("changed", "v2"), tool("new", "v1")],
  };
  act(() => state.onSuccess?.(next));
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
it("clears an old review before a new preview and keeps an issued freeze visible with the flag off", () => {
  const approved = {
    review: { fingerprint: "old", tools: [tool("same", "v1")] },
    names: ["same"],
  };
  const onApply = vi.fn();
  const { rerender } = render(
    <GatewayFrozenToolset
      gatewayId="gateway"
      pending={false}
      approved={approved}
      onApply={onApply}
    />,
  );
  act(() => state.onSuccess?.(approved.review));
  expect(
    screen.getByRole("button", { name: "Freeze 1 tool and reconnect" }),
  ).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Review changes" }));
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
