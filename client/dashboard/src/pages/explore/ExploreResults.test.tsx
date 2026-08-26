import type { ExploreMetaResult } from "@gram/client/models/components/exploremetaresult.js";
import type { ExploreQueryResult } from "@gram/client/models/components/explorequeryresult.js";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { ExploreResultsBody } from "./ExploreResults";

const META: ExploreMetaResult = {
  datasets: [
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
          filterOps: ["in"],
        },
        {
          name: "response_model",
          label: "Response model",
          type: "string",
          role: "dimension",
          unit: "",
          description: "Model reported in the AI provider response",
          filterOps: ["in"],
        },
        {
          name: "cost_usd",
          label: "Cost",
          type: "float",
          role: "measure",
          unit: "usd",
          description: "AI usage cost in USD",
          filterOps: ["gte"],
        },
        {
          name: "input_tokens",
          label: "Input tokens",
          type: "int",
          role: "measure",
          unit: "tokens",
          description: "Input tokens",
          filterOps: ["gte"],
        },
      ],
    },
  ],
};

const RESULT: ExploreQueryResult = {
  calculations: ["SUM(input_tokens)", "SUM(cost_usd)"],
  dataset: "turn_usage",
  granularitySeconds: 3600,
  groupBy: [],
  rows: [
    {
      bucket: "2026-08-18T00:00:00Z",
      group: [],
      values: {
        "SUM(input_tokens)": 1200,
        "SUM(cost_usd)": 1.25,
      },
    },
  ],
};

afterEach(cleanup);

describe("ExploreResultsBody", () => {
  it("rejects mixed-unit calculations on a shared chart axis", () => {
    render(
      <ExploreResultsBody
        result={RESULT}
        meta={META}
        chartType="line"
        loading={false}
        errorMessage={null}
        height={300}
      />,
    );

    expect(screen.getByRole("alert").textContent).toContain(
      "cannot combine calculations with different units",
    );
  });

  it("labels and formats summary values from the result dataset schema", () => {
    render(
      <ExploreResultsBody
        result={{
          ...RESULT,
          calculations: ["SUM(cost_usd)"],
        }}
        meta={META}
        chartType="number"
        loading={false}
        errorMessage={null}
        height={260}
      />,
    );

    expect(screen.getByText("Cost (sum)")).toBeTruthy();
    expect(screen.getByText("$1.25")).toBeTruthy();
  });

  it("uses calculated group names as result headers", () => {
    render(
      <ExploreResultsBody
        result={{
          ...RESULT,
          calculations: ["SUM(cost_usd)"],
          groupBy: ["Is Claude"],
          rows: [
            {
              ...RESULT.rows[0]!,
              group: ["true"],
            },
          ],
        }}
        meta={META}
        chartType="table"
        loading={false}
        errorMessage={null}
        height={260}
      />,
    );

    expect(screen.getByText("Is Claude")).toBeTruthy();
    expect(screen.getByText("true")).toBeTruthy();
  });
});
