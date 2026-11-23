import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { formatTimeRangeLabel, TimeRangePicker } from "./time-range-picker";

afterEach(cleanup);

describe("formatTimeRangeLabel", () => {
  it("names the times of a range that does not fall on day boundaries", () => {
    expect(
      formatTimeRangeLabel(
        new Date(2026, 10, 16, 11, 34),
        new Date(2026, 10, 17, 23, 34),
      ),
    ).toBe("Nov 16, 11:34 AM – Nov 17, 11:34 PM");
  });

  it("names only the days of a range from midnight to the final day's midnight", () => {
    expect(
      formatTimeRangeLabel(new Date(2026, 10, 16), new Date(2026, 10, 17)),
    ).toBe("Nov 16 – Nov 17");
  });

  it("names only the days of a range that runs to the end of its final day", () => {
    expect(
      formatTimeRangeLabel(
        new Date(2026, 10, 16),
        new Date(2026, 10, 17, 23, 59, 59, 999),
      ),
    ).toBe("Nov 16 – Nov 17");
  });
});

it("sizes the field to a range that names its times", () => {
  render(
    <TimeRangePicker
      customRange={{
        from: new Date(2026, 10, 16, 11, 34),
        to: new Date(2026, 10, 17, 23, 34),
      }}
    />,
  );
  const field = screen.getByDisplayValue("Nov 16, 11:34 AM – Nov 17, 11:34 PM");
  expect(Number(field.getAttribute("size"))).toBeGreaterThanOrEqual(
    "Nov 16, 11:34 AM – Nov 17, 11:34 PM".length,
  );
});
