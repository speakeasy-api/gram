import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { useMeterPeriod } from "./use-meter-period";

afterEach(cleanup);

describe("meter calendar ranges", () => {
  it.each([
    {
      from: [2026, 0, 31],
      last: [2026, 3, 29],
      end: "2026-04-30T00:00:00.000Z",
    },
    {
      from: [2025, 10, 30],
      last: [2026, 1, 27],
      end: "2026-02-28T00:00:00.000Z",
    },
    {
      from: [2023, 10, 30],
      last: [2024, 1, 28],
      end: "2024-02-29T00:00:00.000Z",
    },
  ])(
    "retains the last valid range when an inclusive end exceeds $end",
    ({ from, last, end }) => {
      const { result } = renderHook(() => useMeterPeriod([]));
      const start = new Date(from[0]!, from[1]!, from[2]!);
      const lastDay = new Date(last[0]!, last[1]!, last[2]!);
      act(() => result.current.setPickedRange(start, lastDay));
      expect(result.current.period?.to.toISOString()).toBe(end);
      const accepted = result.current.period;

      const tooLate = new Date(last[0]!, last[1]!, last[2]! + 1);
      act(() => result.current.setPickedRange(start, tooLate));
      expect(result.current.period).toEqual(accepted);
    },
  );
});
