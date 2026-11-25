import { afterEach, describe, expect, it, vi } from "vitest";
import { channelsRequest, channelsMcpEndpoint } from "./api";

vi.mock("@/lib/utils", () => ({
  getApiBaseURL: () => "https://dashboard.example",
  getServerURL: () => "https://api.example",
}));
const target = { organizationSlug: "test-org", projectSlug: "test-project" };
afterEach(() => vi.unstubAllGlobals());

describe("Channels browser API", () => {
  it("uses the existing browser session and project, not a fabricated bearer identity", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(new Response(JSON.stringify({ enabled: true })));
    vi.stubGlobal("fetch", fetcher);
    await channelsRequest(target);
    expect(fetcher).toHaveBeenCalledWith(
      "https://dashboard.example/channels/test-org/test-project/api",
      expect.objectContaining({
        method: "GET",
        credentials: "include",
        headers: {
          "Content-Type": "application/json",
        },
        body: undefined,
      }),
    );
  });

  it("posts the agreed operation and arguments envelope", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(new Response(JSON.stringify({ id: "thread-1" })));
    vi.stubGlobal("fetch", fetcher);
    await channelsRequest(target, "channels_follow", {
      threadId: "thread-1",
      follow: true,
    });
    expect(fetcher).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          operation: "channels_follow",
          arguments: { threadId: "thread-1", follow: true },
        }),
      }),
    );
  });

  it.each([
    [401, "authentication required"],
    [403, "forbidden"],
  ])("reports a %i without a fallback login", async (status, error) => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(new Response(JSON.stringify({ error }), { status }));
    vi.stubGlobal("fetch", fetcher);
    await expect(channelsRequest(target)).rejects.toMatchObject({
      message: error,
      status,
    });
    expect(fetcher).toHaveBeenCalledTimes(1);
  });
});

it("constructs an encoded, exact organization/project MCP resource", () => {
  expect(channelsMcpEndpoint(target)).toBe(
    "https://api.example/channels/test-org/test-project/mcp",
  );
  expect(
    channelsMcpEndpoint({
      organizationSlug: "other-org",
      projectSlug: "test-project",
    }),
  ).not.toBe(channelsMcpEndpoint(target));
  expect(
    channelsMcpEndpoint({
      organizationSlug: "org/name",
      projectSlug: "project name",
    }),
  ).toBe("https://api.example/channels/org%2Fname/project%20name/mcp");
});
