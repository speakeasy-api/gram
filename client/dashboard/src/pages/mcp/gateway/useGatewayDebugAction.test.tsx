import { renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useGatewayDebugAction } from "./useGatewayDebugAction";

const palette = vi.hoisted(() => ({
  addActions: vi.fn(),
  removeActions: vi.fn(),
  navigate: vi.fn(),
}));

vi.mock("@/contexts/CommandPalette", () => ({
  useCommandPalette: () => ({
    addActions: palette.addActions,
    removeActions: palette.removeActions,
  }),
}));
vi.mock("react-router", () => ({ useNavigate: () => palette.navigate }));

afterEach(() => vi.clearAllMocks());

it("offers the current gateway's Debug page and withdraws it on leave", () => {
  const { unmount } = renderHook(() =>
    useGatewayDebugAction("/acme/projects/first/mcp/gateway/g1/debug"),
  );
  expect(palette.addActions).toHaveBeenCalledOnce();
  const [action] = palette.addActions.mock.calls[0]![0];
  expect(action).toMatchObject({
    id: "gateway-debug",
    label: "Debug this gateway",
  });
  action.onSelect();
  expect(palette.navigate).toHaveBeenCalledWith(
    "/acme/projects/first/mcp/gateway/g1/debug",
  );

  unmount();
  expect(palette.removeActions).toHaveBeenCalledWith(["gateway-debug"]);
});

it("offers nothing to a viewer who cannot open it", () => {
  renderHook(() => useGatewayDebugAction(null));
  expect(palette.addActions).not.toHaveBeenCalled();
});
