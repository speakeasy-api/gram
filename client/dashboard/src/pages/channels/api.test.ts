import { afterEach, describe, expect, it, vi } from "vitest";
import { channelsRequest } from "./api";

vi.mock("@/lib/utils", () => ({
  getApiBaseURL: () => "https://dashboard.example",
}));
afterEach(() => vi.unstubAllGlobals());

describe("Channels browser API", () => {
  it("uses the existing browser session and project, not a fabricated bearer identity", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(new Response(JSON.stringify({ enabled: true })));
    vi.stubGlobal("fetch", fetcher);
    await channelsRequest("test-project");
    expect(fetcher).toHaveBeenCalledWith(
      "https://dashboard.example/channels/api",
      expect.objectContaining({
        method: "GET",
        credentials: "include",
        headers: {
          "Content-Type": "application/json",
          "Gram-Project": "test-project",
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
    await channelsRequest("test-project", "channels_follow", {
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

  it("reports authentication and permission errors without a fallback login", async () => {
    const fetcher = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ error: "authentication required" }), {
        status: 401,
      }),
    );
    vi.stubGlobal("fetch", fetcher);
    await expect(channelsRequest("test-project")).rejects.toMatchObject({
      message: "authentication required",
      status: 401,
    });
    expect(fetcher).toHaveBeenCalledTimes(1);
  });
});
