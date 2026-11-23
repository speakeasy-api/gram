// @vitest-environment node
import { expect, it, vi } from "vitest";

vi.mock("date-fns", () => {
  throw new Error("Date formatting must not load the date-fns barrel");
});

it("formats relative dates without loading the date-fns barrel", async () => {
  const { dateTimeFormatters } = await import("./dates");

  expect(
    dateTimeFormatters.humanize(new Date("2026-01-01T12:00:00Z"), {
      referenceDate: new Date("2026-01-01T12:05:00Z"),
    }),
  ).toBe("5 minutes ago");
});
