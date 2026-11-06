import { describe, expect, it } from "vitest";
import { STALE_PENDING_TOOL_CALL_MS } from "@/lib/toolCallStatus";
import { getToolLogStatusConfig } from "./toolLogStatus";

const now = Date.UTC(2026, 10, 6, 12, 0, 0);
const startedAgo = (ms: number) => String(BigInt(now - ms) * 1_000_000n);

describe("getToolLogStatusConfig", () => {
  it("labels a recent hook call with no result Pending", () => {
    const status = getToolLogStatusConfig(
      { hookStatus: "pending", startTimeUnixNano: startedAgo(60_000) },
      now,
    );
    expect(status?.label).toBe("Pending");
  });

  it("labels a hook call with no result for a long time Unknown, never Success", () => {
    const status = getToolLogStatusConfig(
      {
        hookStatus: "pending",
        startTimeUnixNano: startedAgo(STALE_PENDING_TOOL_CALL_MS),
      },
      now,
    );
    expect(status?.label).toBe("Unknown");
    expect(status?.dotClassName).not.toContain("emerald");
  });

  it("labels a failed hook call Error in red", () => {
    const status = getToolLogStatusConfig(
      { hookStatus: "failure", startTimeUnixNano: startedAgo(60_000) },
      now,
    );
    expect(status?.label).toBe("Error");
    expect(status?.className).toBe("text-destructive");
  });

  it("keeps blocked and success hook calls and HTTP statuses", () => {
    expect(
      getToolLogStatusConfig(
        { hookStatus: "blocked", startTimeUnixNano: startedAgo(0) },
        now,
      )?.label,
    ).toBe("Blocked");
    expect(
      getToolLogStatusConfig(
        { hookStatus: "success", startTimeUnixNano: startedAgo(0) },
        now,
      )?.label,
    ).toBe("Success");
    expect(
      getToolLogStatusConfig(
        { httpStatusCode: 500, startTimeUnixNano: startedAgo(0) },
        now,
      )?.label,
    ).toBe("Error");
    expect(
      getToolLogStatusConfig(
        { httpStatusCode: 200, startTimeUnixNano: startedAgo(0) },
        now,
      )?.label,
    ).toBe("Success");
  });
});
