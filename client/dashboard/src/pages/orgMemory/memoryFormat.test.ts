import { describe, expect, it } from "vitest";
import {
  childLevel,
  describeDigestSources,
  firstLine,
  formatPeriod,
} from "./memoryFormat";

describe("formatPeriod", () => {
  it("labels a day without shifting it across time zones", () => {
    expect(formatPeriod("day", "2026-10-27", "2026-10-28")).toBe("Tue, Oct 27");
  });

  it("labels a week by its first and last day", () => {
    expect(formatPeriod("week", "2026-10-26", "2026-11-02")).toBe(
      "Oct 26 – Nov 1",
    );
    expect(formatPeriod("week", "2026-10-12", "2026-10-19")).toBe(
      "Oct 12 – 18",
    );
  });

  it("labels a month by name", () => {
    expect(formatPeriod("month", "2026-10-01", "2026-11-01")).toBe(
      "October 2026",
    );
  });

  it("accepts date objects that print as YYYY-MM-DD", () => {
    expect(
      formatPeriod("day", { toString: () => "2026-10-27" }, "2026-10-28"),
    ).toBe("Tue, Oct 27");
  });
});

describe("digest helpers", () => {
  it("steps down one level", () => {
    expect(childLevel("month")).toBe("week");
    expect(childLevel("week")).toBe("day");
    expect(childLevel("day")).toBeNull();
  });

  it("describes what a digest folded", () => {
    expect(
      describeDigestSources({
        level: "day",
        sessionCount: 1,
        memoCount: 2,
        childCount: 0,
      }),
    ).toBe("1 session · 2 memos");
    expect(
      describeDigestSources({
        level: "week",
        sessionCount: 0,
        memoCount: 0,
        childCount: 5,
      }),
    ).toBe("5 days");
  });

  it("previews the first non-empty line", () => {
    expect(firstLine("\n\nShipped the outbox.\nMore detail.")).toBe(
      "Shipped the outbox.",
    );
  });
});
