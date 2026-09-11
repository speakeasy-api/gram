import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useVerifyRemoteMcpUrl } from "./useVerifyRemoteMcpUrl";

const mocks = vi.hoisted(() => ({
  probe: vi.fn(),
}));

vi.mock("@gram/client/react-query/probeRemoteMcpURL.js", () => ({
  useProbeRemoteMcpURLMutation: () => ({
    mutateAsync: mocks.probe,
    isPending: false,
  }),
}));

beforeEach(() => {
  mocks.probe.mockReset();
});

describe("useVerifyRemoteMcpUrl", () => {
  it.each([
    ["mcp_available", true],
    ["authentication_required", true],
    ["invalid_mcp_response", false],
    ["unreachable", false],
  ] as const)(
    "maps %s to creation eligibility %s",
    async (outcome, verified) => {
      mocks.probe.mockResolvedValue({
        outcome,
        reason: outcome === "unreachable" ? "transport_error" : undefined,
        httpStatus: outcome === "invalid_mcp_response" ? 404 : undefined,
      });
      const hook = renderHook(() =>
        useVerifyRemoteMcpUrl(" https://mcp.example.com/mcp "),
      );

      await act(async () => {
        await hook.result.current.trigger();
      });

      expect(hook.result.current.result?.verified).toBe(verified);
      expect(hook.result.current.result?.outcome).toBe(
        verified ? outcome : undefined,
      );
      expect(mocks.probe).toHaveBeenCalledWith({
        request: {
          probeURLForm: { url: "https://mcp.example.com/mcp" },
        },
      });
    },
  );

  it("omits an unknown HTTP status from invalid-response copy", async () => {
    mocks.probe.mockResolvedValue({ outcome: "invalid_mcp_response" });
    const hook = renderHook(() =>
      useVerifyRemoteMcpUrl("https://mcp.example.com/mcp"),
    );

    await act(async () => {
      await hook.result.current.trigger();
    });

    expect(hook.result.current.result).toEqual({
      verified: false,
      message: "MCP endpoint returned an invalid response",
    });
  });
});
