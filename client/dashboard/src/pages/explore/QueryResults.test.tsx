import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DEFAULT_SPEC } from "./exploreModel";
import { QueryResults } from "./QueryResults";

const mocks = vi.hoisted(() => ({
  useRunQuery: vi.fn(),
}));

vi.mock("./ExploreResults", () => ({
  ExploreResultsBody: ({
    result,
  }: {
    result: { dataset: string } | undefined;
  }) => <div>Rendered {result?.dataset}</div>,
}));

vi.mock("./useRunQuery", () => ({
  resultErrorMessage: () => null,
  useRunQuery: mocks.useRunQuery,
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("QueryResults", () => {
  it.each(["events", "turn_usage", "user_usage"])(
    "passes the semantic dataset identity %s to results",
    (dataset) => {
      mocks.useRunQuery.mockReturnValue({
        data: {
          calculations: ["COUNT"],
          dataset,
          granularitySeconds: 0,
          groupBy: [],
          rows: [],
        },
        error: null,
        isPending: false,
      });

      render(
        <QueryResults
          meta={undefined}
          spec={{ ...DEFAULT_SPEC, chartType: "table" }}
        />,
      );

      expect(screen.getByText(`Rendered ${dataset}`)).toBeTruthy();
    },
  );
});
