import type { ExploreMetaResult } from "@gram/client/models/components/exploremetaresult.js";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DEFAULT_SPEC, type ExploreSpec } from "./exploreModel";
import { QueryBuilder } from "./QueryBuilder";

vi.mock("./useRunQuery", () => ({
  useDimensionValues: () => ({
    data: [],
    isPending: false,
  }),
}));

const META: ExploreMetaResult = {
  datasets: [
    {
      name: "events",
      label: "Events",
      category: "event",
      description: "Agent activity",
      grain: "event",
      fields: [
        {
          name: "request_model",
          label: "Request model",
          type: "string",
          role: "dimension",
          unit: "",
          description: "Model requested from the AI provider",
          filterOps: ["in", "not_in", "contains", "exists"],
        },
        {
          name: "response_model",
          label: "Response model",
          type: "string",
          role: "dimension",
          unit: "",
          description: "Model reported in the AI provider response",
          filterOps: ["in", "not_in", "contains", "exists"],
        },
      ],
    },
    {
      name: "turn_usage",
      label: "Turn usage",
      category: "usage",
      description: "Usage for completed turns",
      grain: "turn",
      fields: [
        {
          name: "request_model",
          label: "Request model",
          type: "string",
          role: "dimension",
          unit: "",
          description: "Model requested from the AI provider",
          filterOps: ["in", "not_in", "contains", "exists"],
        },
        {
          name: "response_model",
          label: "Response model",
          type: "string",
          role: "dimension",
          unit: "",
          description: "Model reported in the AI provider response",
          filterOps: ["in", "not_in", "contains", "exists"],
        },
      ],
    },
  ],
};

function BuilderHarness(): JSX.Element {
  const [spec, setSpec] = useState<ExploreSpec>(DEFAULT_SPEC);
  return <QueryBuilder meta={META} spec={spec} onChange={setSpec} />;
}

afterEach(cleanup);

describe("QueryBuilder", () => {
  it("renders the active dataset description from Explore metadata", () => {
    render(<BuilderHarness />);

    expect(screen.getByText(META.datasets[0]!.description)).toBeTruthy();
  });

  it("labels usage datasets as Metrics without changing category semantics", async () => {
    const user = userEvent.setup();
    render(<BuilderHarness />);

    await user.click(screen.getAllByRole("combobox")[0]!);

    expect(screen.getByText("Metrics")).toBeTruthy();
    expect(screen.getByText("Turn usage")).toBeTruthy();
  });

  it("adds condition groups and clears grouping for number charts", async () => {
    const user = userEvent.setup();
    render(<BuilderHarness />);

    await user.click(
      screen.getByRole("button", { name: "Add condition group" }),
    );

    expect(screen.getByLabelText("Condition group name")).toBeTruthy();
    expect(
      screen.getByText(/true group for matches and a false group/i),
    ).toBeTruthy();

    await user.click(screen.getByRole("button", { name: "Number" }));

    expect(screen.queryByLabelText("Condition group name")).toBeNull();
    expect(
      screen
        .getByRole("button", { name: "Add condition group" })
        .hasAttribute("disabled"),
    ).toBe(true);
  });
});
