import { act, renderHook } from "@testing-library/react";
import { expect, it } from "vitest";
import { usePagedRows } from "./usePagedRows";

function numbers(count: number): number[] {
  return Array.from({ length: count }, (_, i) => i + 1);
}

function renderPaged(initial: { rows: number[]; resetOn: string[] }) {
  return renderHook(
    ({ rows, resetOn }) => usePagedRows({ rows, pageSize: 10, resetOn }),
    { initialProps: initial },
  );
}

it("slices the rows for the current page", () => {
  const { result } = renderPaged({ rows: numbers(25), resetOn: [] });

  expect(result.current.page).toBe(0);
  expect(result.current.pageRows).toEqual(numbers(10));

  act(() => result.current.setPage(2));

  expect(result.current.page).toBe(2);
  expect(result.current.pageRows).toEqual([21, 22, 23, 24, 25]);
});

it("returns to the first page when the reset key changes", () => {
  const { result, rerender } = renderPaged({
    rows: numbers(25),
    resetOn: ["a"],
  });

  act(() => result.current.setPage(1));
  expect(result.current.page).toBe(1);

  rerender({ rows: numbers(25), resetOn: ["b"] });

  expect(result.current.page).toBe(0);
});

it("clamps to the last page when the rows shrink", () => {
  const { result, rerender } = renderPaged({ rows: numbers(25), resetOn: [] });

  act(() => result.current.setPage(2));
  rerender({ rows: numbers(15), resetOn: [] });

  expect(result.current.page).toBe(1);
  expect(result.current.pageRows).toEqual([11, 12, 13, 14, 15]);
});

it("stays on the clamped page when the rows grow back", () => {
  const { result, rerender } = renderPaged({ rows: numbers(25), resetOn: [] });

  act(() => result.current.setPage(2));
  rerender({ rows: numbers(5), resetOn: [] });
  expect(result.current.page).toBe(0);

  rerender({ rows: numbers(25), resetOn: [] });

  expect(result.current.page).toBe(0);
  expect(result.current.pageRows).toEqual(numbers(10));
});
