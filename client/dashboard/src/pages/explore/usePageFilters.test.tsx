import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { describe, expect, it, vi } from "vitest";
import { usePageFilters, type PageFilterConfig } from "./usePageFilters";

const testState = vi.hoisted(() => ({
  /** Every dimension asked for values, as dataset/dimension. */
  asked: [] as string[],
  /** How many days each dataset/dimension's values were read over. */
  days: {} as Record<string, number>,
}));

const sessions: AnalyticsDataset = {
  name: "sessions",
  kind: "event",
  grain: "session",
  description: "One row per agent session.",
  fields: [
    {
      name: "user",
      type: "string",
      role: "dimension",
      default: true,
      operators: ["in"],
    },
    {
      name: "model",
      type: "string",
      role: "dimension",
      default: false,
      operators: ["in"],
    },
  ],
};
const toolCalls: AnalyticsDataset = {
  name: "tool_calls",
  kind: "event",
  grain: "tool call",
  description: "One row per tool call.",
  fields: [
    {
      name: "user",
      type: "string",
      role: "dimension",
      default: false,
      operators: ["in"],
    },
    {
      name: "mcp_server",
      type: "string",
      role: "dimension",
      default: false,
      operators: ["in"],
    },
  ],
};

vi.mock("@/contexts/Auth", () => ({ useProject: () => ({ id: "project" }) }));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/react-query/analyticsDescribe.js", () => ({
  useAnalyticsDescribe: () => ({ data: { datasets: [sessions, toolCalls] } }),
}));
vi.mock("@gram/client/funcs/analyticsDimensionValues.js", () => ({
  analyticsDimensionValues: (
    _client: unknown,
    request: {
      dimensionValuesRequestBody: {
        dataset: string;
        dimension: string;
        from: Date;
        to: Date;
      };
    },
  ) => {
    const { dataset, dimension, from, to } = request.dimensionValuesRequestBody;
    testState.asked.push(`${dataset}/${dimension}`);
    testState.days[`${dataset}/${dimension}`] = Math.round(
      (to.getTime() - from.getTime()) / 86_400_000,
    );
    return Promise.resolve({
      ok: true,
      value: {
        dataset,
        dimension,
        values: [{ value: `${dimension}-a`, count: 2 }],
      },
    });
  },
}));

const CONFIG: PageFilterConfig = {
  fields: [
    { field: "user", label: "User" },
    { field: "mcp_server", label: "Server" },
  ],
  defaultPreset: "7d",
};

function wrapper(entry: string) {
  const client = new QueryClient();
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[entry]}>{children}</MemoryRouter>
    </QueryClientProvider>
  );
}

describe("usePageFilters", () => {
  it("offers the date range and only the fields the page names, in order", () => {
    const { result } = renderHook(() => usePageFilters(CONFIG), {
      wrapper: wrapper("/page"),
    });
    expect(
      result.current.toolbar.schema.map((dimension) => [
        dimension.id,
        dimension.label,
        dimension.kind,
      ]),
    ).toEqual([
      ["date", "Date range", "daterange"],
      ["user", "User", "multiselect"],
      ["mcp_server", "Server", "multiselect"],
    ]);
  });

  it("pins the date range and the first two fields; the rest wait behind More filters", () => {
    const { result } = renderHook(
      () =>
        usePageFilters({
          fields: [
            { field: "user", label: "User" },
            { field: "surface", label: "Agent" },
            { field: "model", label: "Model" },
            { field: "status", label: "Status" },
          ],
        }),
      { wrapper: wrapper("/page") },
    );
    expect(
      result.current.toolbar.schema.map((dimension) => [
        dimension.id,
        dimension.pinned ?? false,
      ]),
    ).toEqual([
      ["date", true],
      ["user", true],
      ["surface", true],
      ["model", false],
      ["status", false],
    ]);
  });

  it("opens on the page's default range, with nothing filtered", () => {
    const { result } = renderHook(() => usePageFilters(CONFIG), {
      wrapper: wrapper("/page"),
    });
    expect(result.current.context.window).toEqual({
      preset: "7d",
      customRange: null,
      customLabel: null,
    });
    expect(result.current.context.filters).toEqual({});
  });

  it("reads the shared date and field parameters from the URL into the page context", () => {
    const { result } = renderHook(() => usePageFilters(CONFIG), {
      wrapper: wrapper("/page?range=4h&user=ann,bob"),
    });
    expect(result.current.context.window?.preset).toBe("4h");
    expect(result.current.context.filters).toEqual({ user: ["ann", "bob"] });
  });

  it("offers each field the values its first dataset reports", async () => {
    testState.asked.length = 0;
    const { result } = renderHook(() => usePageFilters(CONFIG), {
      wrapper: wrapper("/page"),
    });
    await waitFor(() =>
      expect(result.current.toolbar.optionsById.mcp_server).toEqual([
        { value: "mcp_server-a", label: "mcp_server-a" },
      ]),
    );
    expect(testState.asked).toContain("sessions/user");
    expect(testState.asked).toContain("tool_calls/mcp_server");
  });

  it("narrows the page to a range dragged on a chart", () => {
    const { result } = renderHook(() => usePageFilters(CONFIG), {
      wrapper: wrapper("/page"),
    });
    const from = new Date(Date.UTC(2026, 8, 14, 10));
    const to = new Date(Date.UTC(2026, 8, 14, 12));
    act(() => result.current.context.onRangeSelect?.(from, to));
    expect(result.current.context.window?.customRange).toEqual({ from, to });
  });

  it("without a default range, leaves each widget its own window and says so", () => {
    const { result } = renderHook(
      () => usePageFilters({ fields: CONFIG.fields }),
      { wrapper: wrapper("/page") },
    );
    expect(result.current.context.window).toEqual({
      preset: null,
      customRange: null,
      customLabel: null,
    });
    const date = result.current.toolbar.schema[0];
    expect(date?.allLabel).toBe("Each widget's window");
  });

  it("offers each field the values of the first of the page's datasets that can filter by it", async () => {
    testState.asked.length = 0;
    const { result } = renderHook(
      () =>
        usePageFilters({
          fields: [
            { field: "user", label: "User" },
            { field: "model", label: "Model" },
          ],
          optionsDatasets: ["tool_calls", "sessions"],
        }),
      { wrapper: wrapper("/page") },
    );
    await waitFor(() =>
      expect(result.current.toolbar.optionsById.model).toHaveLength(1),
    );
    // tool_calls comes first and has user; it has no model, so sessions
    // answers that one.
    expect(testState.asked).toContain("tool_calls/user");
    expect(testState.asked).not.toContain("sessions/user");
    expect(testState.asked).toContain("sessions/model");
  });

  it("with no range picked, reads values over the window the page names, or 30 days", async () => {
    testState.days = {};
    const fields = [{ field: "user", label: "User" }];
    renderHook(() => usePageFilters({ fields, optionsWindow: "90d" }), {
      wrapper: wrapper("/page"),
    });
    await waitFor(() => expect(testState.days["sessions/user"]).toBe(90));

    testState.days = {};
    renderHook(() => usePageFilters({ fields }), {
      wrapper: wrapper("/page"),
    });
    await waitFor(() => expect(testState.days["sessions/user"]).toBe(30));
  });

  it("says whether the URL holds any of its values, and sets the whole bar at once", () => {
    const { result: untouched } = renderHook(() => usePageFilters(CONFIG), {
      wrapper: wrapper("/page"),
    });
    expect(untouched.current.touched).toBe(false);

    const { result } = renderHook(() => usePageFilters(CONFIG), {
      wrapper: wrapper("/page?user=alice"),
    });
    expect(result.current.touched).toBe(true);
    act(() =>
      result.current.apply({
        window: { preset: "30d", customRange: null, customLabel: null },
        filters: { mcp_server: ["github"] },
      }),
    );
    expect(result.current.context.window?.preset).toBe("30d");
    // A field the values leave out is cleared.
    expect(result.current.context.filters).toEqual({ mcp_server: ["github"] });

    // Without a window, the bar returns to the page's default range.
    act(() => result.current.apply({ filters: {} }));
    expect(result.current.context.window?.preset).toBe("7d");
    expect(result.current.context.filters).toEqual({});
  });
});
