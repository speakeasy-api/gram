import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes, useNavigate } from "react-router";
import { describe, expect, it } from "vitest";
import { useAttributeSearchParams } from "./useAttributeSearchParams";

function wrapperAt(initialEntry: string) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route path="/logs" element={children} />
        </Routes>
      </MemoryRouter>
    );
  };
}

function useHookWithNavigate() {
  return { ...useAttributeSearchParams(), navigate: useNavigate() };
}

describe("useAttributeSearchParams", () => {
  it("reads the query and chips out of the URL", () => {
    const { result } = renderHook(() => useAttributeSearchParams(), {
      wrapper: wrapperAt("/logs?q=timeout&af=gram.tool.name%3Aeq%3Acharge"),
    });

    expect(result.current.attributeSearchQuery).toBe("timeout");
    expect(result.current.attributeSearchInput).toBe("timeout");
    expect(result.current.attributeFilters).toHaveLength(1);
    expect(result.current.attributeFilters[0]?.path).toBe("gram.tool.name");
    expect(result.current.attributeFilters[0]?.value).toBe("charge");
  });

  it("follows browser navigation instead of freezing at mount", () => {
    const { result } = renderHook(() => useHookWithNavigate(), {
      wrapper: wrapperAt("/logs?q=timeout"),
    });

    act(() => {
      void result.current.navigate(
        "/logs?q=refund&af=user.region%3Aeq%3Aus-east-1",
      );
    });

    expect(result.current.attributeSearchQuery).toBe("refund");
    expect(result.current.attributeSearchInput).toBe("refund");
    expect(result.current.attributeFilters[0]?.path).toBe("user.region");
  });

  it("keeps a stable filter identity while the param is unchanged", () => {
    const { result, rerender } = renderHook(() => useAttributeSearchParams(), {
      wrapper: wrapperAt("/logs?af=gram.tool.name%3Aeq%3Acharge"),
    });

    const first = result.current.attributeFilters;
    rerender();

    // parseFilters mints fresh ids, and this array is part of the traces query
    // key — a new identity per render would refetch the list forever.
    expect(result.current.attributeFilters).toBe(first);
  });

  it("clears the params when the query and chips are emptied", () => {
    const { result } = renderHook(() => useAttributeSearchParams(), {
      wrapper: wrapperAt("/logs?q=timeout&af=gram.tool.name%3Aeq%3Acharge"),
    });

    act(() => result.current.updateAttributeSearchQuery("  "));
    act(() => result.current.updateAttributeFilters([]));

    expect(result.current.attributeSearchQuery).toBeNull();
    expect(result.current.attributeFilters).toEqual([]);
  });
});
