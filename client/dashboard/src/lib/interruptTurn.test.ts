import { afterEach, describe, expect, it, vi } from "vitest";

import { interruptTurn } from "./interruptTurn";

vi.mock("@/lib/utils", () => ({ getServerURL: () => "http://test.local" }));

afterEach(() => {
  vi.unstubAllGlobals();
});

function stubFetch(response: Response): ReturnType<typeof vi.fn> {
  const spy = vi.fn(async () => response);
  vi.stubGlobal("fetch", spy);
  return spy;
}

describe("interruptTurn", () => {
  it("posts the conversation the server needs to identify the turn", async () => {
    const spy = stubFetch(
      new Response(
        JSON.stringify({
          stopped: true,
          interrupted: true,
          cancelled_queued: 0,
        }),
        { status: 200 },
      ),
    );

    await interruptTurn({
      chatId: "chat-1",
      assistantId: "asst-1",
      projectSlug: "proj",
      sessionToken: "sess",
    });

    expect(spy).toHaveBeenCalledTimes(1);
    const [url, init] = spy.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("http://test.local/rpc/assistants.interruptTurn");
    expect(init.method).toBe("POST");
    // Snake case, because the route is Goa-generated and rejects the payload
    // outright under any other spelling.
    expect(JSON.parse(init.body as string)).toEqual({
      assistant_id: "asst-1",
      chat_id: "chat-1",
    });
    // The /rpc routes authenticate from headers; without the session token a
    // stop from a session-token client 401s while the turn keeps generating.
    const headers = init.headers as Record<string, string>;
    expect(headers["Gram-Session"]).toBe("sess");
    expect(headers["Gram-Project"]).toBe("proj");
  });

  it("omits the session header when there is no token", async () => {
    const spy = stubFetch(new Response("{}", { status: 200 }));

    await interruptTurn({
      chatId: "chat-1",
      assistantId: "asst-1",
      projectSlug: "proj",
    });

    const [, init] = spy.mock.calls[0] as [string, RequestInit];
    expect(init.headers).not.toHaveProperty("Gram-Session");
  });

  it("rejects on a failed request so the caller can report it", async () => {
    stubFetch(new Response("nope", { status: 500 }));

    await expect(
      interruptTurn({
        chatId: "chat-1",
        assistantId: "asst-1",
        projectSlug: "proj",
      }),
    ).rejects.toThrow("interrupt failed: 500");
  });
});
