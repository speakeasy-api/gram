import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { TimeRangePicker } from "./time-range-picker";

vi.mock("@/elements/lib/errorTracking", () => ({ trackError: vi.fn() }));
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

it.each([true, false])(
  "routes calendar picks to the calendar callback when supplied: %s",
  (calendarHandler) => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 0, 20, 12));
    const onCalendarRangeChange = vi.fn<(from: Date, to: Date) => void>();
    const onCustomRangeChange =
      vi.fn<(from: Date, to: Date, label?: string) => void>();
    render(
      <TimeRangePicker
        onCalendarRangeChange={
          calendarHandler ? onCalendarRangeChange : undefined
        }
        onCustomRangeChange={onCustomRangeChange}
      />,
    );
    fireEvent.click(
      screen.getByPlaceholderText("e.g., 3 days ago, last week..."),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Select from calendar..." }),
    );
    fireEvent.click(screen.getByRole("button", { name: "10" }));
    expect(onCalendarRangeChange).not.toHaveBeenCalled();
    expect(onCustomRangeChange).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "12" }));
    const called = calendarHandler
      ? onCalendarRangeChange
      : onCustomRangeChange;
    const unused = calendarHandler
      ? onCustomRangeChange
      : onCalendarRangeChange;
    expect(called).toHaveBeenCalledExactlyOnceWith(
      new Date(2026, 0, 10),
      new Date(2026, 0, 12),
    );
    expect(unused).not.toHaveBeenCalled();
  },
);
