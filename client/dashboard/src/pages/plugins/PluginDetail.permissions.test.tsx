import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import PluginDetail from "./PluginDetail";

const state = vi.hoisted(() => ({ loading: false, orgRead: false }));
const adminQuery = vi.hoisted(() => vi.fn());
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    isLoading: state.loading,
    hasScope: (scope: string) => scope === "org:read" && state.orgRead,
  }),
}));
vi.mock("./PluginDistributionDetail", () => ({
  PluginDistributionDetail: () => <div>Distribution-only skills</div>,
}));
vi.mock("@gram/client/react-query/plugin", () => ({
  usePluginSuspense: adminQuery,
}));

afterEach(cleanup);
beforeEach(() => {
  state.loading = false;
  state.orgRead = false;
  adminQuery.mockClear();
});

describe("plugin detail permission boundary", () => {
  it("mounts only distribution UI without org:read", () => {
    render(<PluginDetail />);
    expect(screen.getByText("Distribution-only skills")).toBeTruthy();
    expect(adminQuery).not.toHaveBeenCalled();
    expect(screen.queryByText("Publish now")).toBeNull();
  });

  it("does not mount either query tree until grants are loaded", () => {
    state.loading = true;
    const { container } = render(<PluginDetail />);
    expect(container.innerHTML).toBe("");
    expect(adminQuery).not.toHaveBeenCalled();
  });
});
