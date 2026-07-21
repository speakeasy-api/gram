import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { send } from "./send.js";
import type { IngestBody } from "./mapping.js";

const body: IngestBody = {
  schema_version: "hook.ingest.v1",
  idempotency_key: "idem-1",
  source: {
    adapter: "opencode",
    adapter_version: "0.1.0",
    raw_event_name: "session.created",
  },
  session: { id: "s1" },
  event: { type: "session.started", occurred_at: new Date().toISOString() },
};

describe("send", () => {
  beforeEach(() => {
    process.env.GRAM_URL = "https://gram.test";
    process.env.GRAM_KEY = "test-key";
    process.env.GRAM_PROJECT = "test-project";
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    delete process.env.GRAM_URL;
    delete process.env.GRAM_KEY;
    delete process.env.GRAM_PROJECT;
  });

  it("POSTs once to /rpc/hooks.ingest with auth + idempotency headers on success", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(new Response(null, { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await send(body);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("https://gram.test/rpc/hooks.ingest");
    const headers = init.headers as Record<string, string>;
    expect(headers["Gram-Key"]).toBe("test-key");
    expect(headers["Gram-Project"]).toBe("test-project");
    expect(headers["Idempotency-Key"]).toBe("idem-1");
    expect(JSON.parse(init.body as string)).toEqual(body);
  });

  it("drops the event without fetching when GRAM_URL is not TLS", async () => {
    process.env.GRAM_URL = "http://evil.example.com";
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    await send(body);

    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("retries once then swallows the failure instead of throwing", async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error("network down"));
    vi.stubGlobal("fetch", fetchMock);

    await expect(send(body)).resolves.toBeUndefined();

    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
