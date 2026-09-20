import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FilterDraft } from "./exploreModel";
import { FilterRow } from "./FilterRow";

const testState = vi.hoisted(() => ({
  values: [] as { value: string; count: number }[],
  isPending: false,
  isError: false,
  /** Every (dataset, dimension, enabled) the picker asked for, in order. */
  asks: [] as { dataset: string; dimension: string; enabled: boolean }[],
}));

vi.mock("./useDimensionValues", () => ({
  DIMENSION_VALUES_LIMIT: 200,
  useDimensionValues: (
    dataset: string,
    dimension: string,
    _window: string,
    enabled: boolean,
  ) => {
    testState.asks.push({ dataset, dimension, enabled });
    return {
      data:
        enabled && !testState.isPending && !testState.isError
          ? { dataset, dimension, values: testState.values }
          : undefined,
      isPending: testState.isPending || !enabled,
      isError: testState.isError,
    };
  },
}));

const dataset: AnalyticsDataset = {
  name: "tool_calls",
  kind: "event",
  grain: "tool call",
  description: "One row per tool call.",
  fields: [
    {
      name: "tool_name",
      type: "string",
      role: "dimension",
      default: true,
      operators: ["equals", "in"],
    },
    {
      name: "user",
      type: "string",
      role: "dimension",
      default: false,
      operators: ["in"],
    },
  ],
};

/** A row that keeps its own draft, the way the builder does. */
function Harness({
  initial,
  onChange,
}: {
  initial: FilterDraft;
  onChange: (next: FilterDraft) => void;
}) {
  const [filter, setFilter] = useState(initial);
  return (
    <FilterRow
      dataset={dataset}
      window="24h"
      filter={filter}
      onChange={(next) => {
        setFilter(next);
        onChange(next);
      }}
      onRemove={() => {}}
    />
  );
}

describe("FilterRow", () => {
  beforeEach(() => {
    testState.values = [
      { value: "Bash", count: 9 },
      { value: "Read", count: 6 },
      { value: "Grep", count: 4 },
    ];
    testState.isPending = false;
    testState.isError = false;
    testState.asks = [];
  });

  afterEach(() => {
    cleanup();
  });

  it("lists the dimension's values with their counts once the picker opens", () => {
    render(
      <Harness
        initial={{ field: "tool_name", operator: "in", values: [] }}
        onChange={() => {}}
      />,
    );
    // Nothing is fetched until the picker opens.
    expect(testState.asks.every((ask) => !ask.enabled)).toBe(true);

    fireEvent.click(screen.getByRole("combobox", { name: "Filter values" }));
    expect(testState.asks.at(-1)).toEqual({
      dataset: "tool_calls",
      dimension: "tool_name",
      enabled: true,
    });
    expect(screen.getByText("Bash")).toBeTruthy();
    expect(screen.getByText("9")).toBeTruthy();
    expect(screen.getByText("Grep")).toBeTruthy();
    expect(screen.getByText("4")).toBeTruthy();
  });

  it("picks one value for equals and closes", () => {
    const onChange = vi.fn<(next: FilterDraft) => void>();
    render(
      <Harness
        initial={{ field: "tool_name", operator: "equals", values: [] }}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByRole("combobox", { name: "Filter value" }));
    fireEvent.click(screen.getByText("Read"));
    expect(onChange).toHaveBeenLastCalledWith({
      field: "tool_name",
      operator: "equals",
      values: ["Read"],
    });
    expect(screen.queryByText("Grep")).toBeNull();
  });

  it("collects several values for in, shown as chips", () => {
    const onChange = vi.fn<(next: FilterDraft) => void>();
    render(
      <Harness
        initial={{ field: "tool_name", operator: "in", values: [] }}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByRole("combobox", { name: "Filter values" }));
    fireEvent.click(screen.getByText("Bash"));
    fireEvent.click(screen.getByText("Grep"));
    expect(onChange).toHaveBeenLastCalledWith({
      field: "tool_name",
      operator: "in",
      values: ["Bash", "Grep"],
    });
    expect(screen.getByRole("button", { name: "Remove Bash" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Remove Grep" })).toBeTruthy();
  });

  it("adds a typed value that is not in the list, set apart from listed ones", () => {
    const onChange = vi.fn<(next: FilterDraft) => void>();
    render(
      <Harness
        initial={{ field: "tool_name", operator: "in", values: [] }}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByRole("combobox", { name: "Filter values" }));
    fireEvent.change(screen.getByPlaceholderText("Search or type a value"), {
      target: { value: "Edit" },
    });
    expect(screen.getByText("No listed value matches.")).toBeTruthy();
    fireEvent.click(screen.getByText(/Use “Edit”/));
    expect(onChange).toHaveBeenLastCalledWith({
      field: "tool_name",
      operator: "in",
      values: ["Edit"],
    });
  });

  it("says so while values load and when the window holds none", () => {
    testState.isPending = true;
    const { unmount } = render(
      <Harness
        initial={{ field: "tool_name", operator: "in", values: [] }}
        onChange={() => {}}
      />,
    );
    fireEvent.click(screen.getByRole("combobox", { name: "Filter values" }));
    expect(screen.getByText("Loading values…")).toBeTruthy();
    unmount();

    testState.isPending = false;
    testState.values = [];
    render(
      <Harness
        initial={{ field: "tool_name", operator: "in", values: [] }}
        onChange={() => {}}
      />,
    );
    fireEvent.click(screen.getByRole("combobox", { name: "Filter values" }));
    expect(screen.getByText("No values in this window.")).toBeTruthy();
  });
});
