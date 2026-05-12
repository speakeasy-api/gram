import type { ReactNode } from "react";
import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { MemoryRouter, useNavigate } from "react-router";
import { DEFAULT_HOOK_TYPES } from "./observeFilterConstants";
import { useObserveFilters } from "./useObserveFilters";

function useObserveFiltersWithNavigation() {
  const filters = useObserveFilters();
  const navigate = useNavigate();
  return { filters, navigate };
}

describe("useObserveFilters", () => {
  it("derives chips and hook types from browser navigation", () => {
    function RouterWrapper({ children }: { children: ReactNode }) {
      return (
        <MemoryRouter
          initialEntries={[
            "/?server=api&user=alex@example.com&hookTypes=mcp",
            "/?server=api-v2&user=becca@example.com&hookTypes=local,skill",
          ]}
          initialIndex={1}
        >
          {children}
        </MemoryRouter>
      );
    }

    const { result } = renderHook(() => useObserveFiltersWithNavigation(), {
      wrapper: RouterWrapper,
    });

    expect(result.current.filters.activeFilters).toEqual([
      {
        display: "api-v2",
        filters: ["api-v2"],
        path: "gram.tool_call.source",
      },
      {
        display: "becca@example.com",
        filters: ["becca@example.com"],
        path: "user.email",
      },
    ]);
    expect(result.current.filters.selectedHookTypes).toEqual([
      "local",
      "skill",
    ]);

    act(() => result.current.navigate(-1));

    expect(result.current.filters.activeFilters).toEqual([
      {
        display: "api",
        filters: ["api"],
        path: "gram.tool_call.source",
      },
      {
        display: "alex@example.com",
        filters: ["alex@example.com"],
        path: "user.email",
      },
    ]);
    expect(result.current.filters.selectedHookTypes).toEqual(["mcp"]);
  });

  it("falls back to default hook types when the URL param is cleared", () => {
    function RouterWrapper({ children }: { children: ReactNode }) {
      return (
        <MemoryRouter initialEntries={["/?hookTypes=local"]}>
          {children}
        </MemoryRouter>
      );
    }

    const { result } = renderHook(() => useObserveFiltersWithNavigation(), {
      wrapper: RouterWrapper,
    });

    expect(result.current.filters.selectedHookTypes).toEqual(["local"]);

    act(() => result.current.navigate("/"));

    expect(result.current.filters.selectedHookTypes).toEqual(
      DEFAULT_HOOK_TYPES,
    );
  });
});
