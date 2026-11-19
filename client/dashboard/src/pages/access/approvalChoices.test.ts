// @vitest-environment node
import { describe, expect, it } from "vitest";
import { approvalChoices } from "./approvalChoices";

describe("approvalChoices", () => {
  it("offers 1, 2 and 4 hours and the end of the day", () => {
    const now = new Date(2026, 10, 17, 9, 0, 0);
    const choices = approvalChoices(now);

    expect(choices.map((c) => c.key)).toEqual(["1h", "2h", "4h", "eod"]);
    expect(choices[0]!.expiresAt.getTime() - now.getTime()).toBe(3_600_000);
    expect(choices[3]!.expiresAt).toEqual(new Date(2026, 10, 17, 23, 59, 0));
  });

  it("leaves out the end of the day when it is under 10 minutes away", () => {
    const choices = approvalChoices(new Date(2026, 10, 17, 23, 52, 0));

    expect(choices.map((c) => c.key)).toEqual(["1h", "2h", "4h"]);
  });

  it("leaves out the end of the day when it is more than a day away", () => {
    const zone = process.env.TZ;
    process.env.TZ = "America/New_York";
    try {
      // 1 November 2026 gains an hour in New York: 23:59 is 24h29m after
      // half past midnight, longer than a temporary grant can last.
      const choices = approvalChoices(new Date(2026, 10, 1, 0, 30, 0));
      expect(choices.map((c) => c.key)).toEqual(["1h", "2h", "4h"]);
    } finally {
      process.env.TZ = zone;
    }
  });
});
