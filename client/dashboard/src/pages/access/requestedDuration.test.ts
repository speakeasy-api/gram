// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  approvalWindowsFor,
  requestDurationChoices,
  requestedDurationFields,
  requestedDurationLabel,
} from "./requestedDuration";

const morning = new Date(2026, 10, 23, 9, 0, 0);
const noAsk = {
  requestedDurationSeconds: undefined,
  requestedUntil: undefined,
};

describe("requestDurationChoices", () => {
  it("starts with no preference, then the windows an approver can pick", () => {
    expect(requestDurationChoices(morning).map((c) => c.key)).toEqual([
      "none",
      "1h",
      "2h",
      "4h",
      "eod",
    ]);
  });

  it("leaves out End of day when it is under 10 minutes away", () => {
    const late = new Date(2026, 10, 23, 23, 52, 0);
    expect(requestDurationChoices(late).map((c) => c.key)).toEqual([
      "none",
      "1h",
      "2h",
      "4h",
    ]);
  });
});

describe("requestedDurationFields", () => {
  it("sends a fixed window as a duration", () => {
    expect(requestedDurationFields("2h", morning)).toEqual({ duration: "2h" });
  });

  it("sends End of day as the requester's local 23:59", () => {
    expect(requestedDurationFields("eod", morning)).toEqual({
      requestedUntil: new Date(2026, 10, 23, 23, 59, 0),
    });
  });

  it("sends nothing without a preference, or once End of day has passed", () => {
    expect(requestedDurationFields("none", morning)).toEqual({});
    expect(
      requestedDurationFields("eod", new Date(2026, 10, 23, 23, 55, 0)),
    ).toEqual({});
  });
});

describe("requestedDurationLabel", () => {
  it("names a duration or a deadline", () => {
    expect(
      requestedDurationLabel(
        { requestedDurationSeconds: 7200, requestedUntil: undefined },
        morning,
      ),
    ).toBe("for 2 hours");
    expect(
      requestedDurationLabel(
        {
          requestedDurationSeconds: undefined,
          requestedUntil: new Date(2026, 10, 23, 18, 0, 0),
        },
        morning,
      ),
    ).toBe("until 18:00");
    expect(
      requestedDurationLabel(
        {
          requestedDurationSeconds: undefined,
          requestedUntil: new Date(2026, 10, 24, 2, 30, 0),
        },
        morning,
      ),
    ).toBe("until 24 Nov, 02:30");
    expect(requestedDurationLabel(noAsk, morning)).toBeUndefined();
  });
});

describe("approvalWindowsFor", () => {
  it("suggests the window the requester asked for", () => {
    const { suggested } = approvalWindowsFor(
      { requestedDurationSeconds: 4 * 3600, requestedUntil: undefined },
      morning,
    );
    expect(suggested).toBe("4h");
  });

  it("suggests End of day when the deadline is the approver's end of day", () => {
    const { windows, suggested } = approvalWindowsFor(
      {
        requestedDurationSeconds: undefined,
        requestedUntil: new Date(2026, 10, 23, 23, 59, 0),
      },
      morning,
    );
    expect(suggested).toBe("eod");
    expect(windows.map((w) => w.key)).toEqual(["1h", "2h", "4h", "eod"]);
  });

  it("offers any other deadline as a window of its own", () => {
    const until = new Date(2026, 10, 23, 18, 0, 0);
    const { windows, suggested } = approvalWindowsFor(
      { requestedDurationSeconds: undefined, requestedUntil: until },
      morning,
    );
    expect(suggested).toBe("asked");
    const asked = windows.find((w) => w.key === "asked")!;
    expect(asked.label).toBe("Until 18:00");
    expect(asked.expiresAt).toEqual(until);
  });

  it("falls back to the first window without an ask or for a lapsed deadline", () => {
    expect(approvalWindowsFor(noAsk, morning).suggested).toBe("1h");
    const lapsed = approvalWindowsFor(
      {
        requestedDurationSeconds: undefined,
        requestedUntil: new Date(2026, 10, 23, 9, 5, 0),
      },
      morning,
    );
    expect(lapsed.suggested).toBe("1h");
    expect(lapsed.windows.map((w) => w.key)).not.toContain("asked");
  });
});
