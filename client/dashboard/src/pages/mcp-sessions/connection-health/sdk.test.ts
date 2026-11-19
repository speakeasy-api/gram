// @vitest-environment node
import type { GramCore } from "@gram/client/core.js";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { listAttempts } from "./sdk";

const call = vi.hoisted(() => ({ request: undefined as unknown }));

vi.mock("@gram/client/funcs/mcpConnectionHealthListAttempts.js", () => ({
  mcpConnectionHealthListAttempts: (_client: unknown, request: unknown) => {
    call.request = request;
    return Promise.resolve({
      ok: true,
      value: {
        loggingEnabled: true,
        connectionViewerScope: "own",
        nextCursor: "next",
        attempts: [
          {
            id: "ref-1",
            time: new Date("2026-11-17T11:04:12Z"),
            firstStartedAt: new Date("2026-11-17T09:48:03Z"),
            count: 14,
            connectionOutcome: "in_progress",
            connectionStepGroup: "service_sign_in",
            connectionFailureSource: "server",
            app: { family: "", name: "Unlisted app" },
            server: { id: "gw", kind: "gateway", name: "Engineering Tools" },
            member: { id: "m", kind: "mcp", name: "Linear" },
          },
        ],
      },
    });
  },
}));

describe("listAttempts", () => {
  // Inside the 30-day log, so the range is sent as given.
  beforeEach(() => {
    vi.useFakeTimers({ now: new Date("2026-11-17T12:00:00.000Z") });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("sends Dates and reads the SDK's enum field names back as the contract's", async () => {
    const result = await listAttempts({} as GramCore, "proj", {
      userSessionIssuerId: "issuer",
      from: "2026-11-16T11:00:00.000Z",
      to: "2026-11-17T11:00:00.000Z",
      filters: { outcomes: ["failed"] },
    });
    expect(call.request).toMatchObject({
      gramProject: "proj",
      listAttemptsRequestBody: {
        userSessionIssuerId: "issuer",
        from: new Date("2026-11-16T11:00:00.000Z"),
        to: new Date("2026-11-17T11:00:00.000Z"),
        filters: { outcomes: ["failed"] },
      },
    });
    expect(result.viewerScope).toBe("own");
    expect(result.nextCursor).toBe("next");
    expect(result.attempts?.[0]).toMatchObject({
      time: "2026-11-17T11:04:12.000Z",
      outcome: "in_progress",
      step: "service_sign_in",
      source: "server",
      member: { name: "Linear" },
    });
  });
});
