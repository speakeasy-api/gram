import { describe, expect, it } from "vitest";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { toolSectionRiskMatches } from "./toolRisk";

function riskResult(
  id: string,
  match: string,
  spans?: RiskResult["spans"],
): RiskResult {
  return { id, match, spans } as RiskResult;
}

describe("toolSectionRiskMatches", () => {
  it("uses the attributed argument span instead of a finding's primary function span", () => {
    const result = riskResult("risk-1", "delete_file", [
      { field: "tool.function", match: "delete_file" },
      { field: "tool.args", path: "path", match: "/tmp/report.txt" },
    ]);

    expect(
      toolSectionRiskMatches(
        [result],
        '{"path":"/tmp/report.txt"}',
        "tool.args",
      ),
    ).toEqual([{ value: "/tmp/report.txt", result }]);
  });

  it("does not attach a function-only finding to an empty Arguments section", () => {
    const result = riskResult("risk-1", "delete_file", [
      { field: "tool.function", match: "delete_file" },
    ]);

    expect(toolSectionRiskMatches([result], "", "tool.args")).toEqual([]);
  });

  it("keeps unattributed legacy findings only when their value is visible", () => {
    const visible = riskResult("visible", "DROP TABLE");
    const absent = riskResult("absent", "delete_file");

    expect(
      toolSectionRiskMatches(
        [absent, visible],
        '{"command":"DROP TABLE users"}',
        "tool.args",
      ),
    ).toEqual([{ value: "DROP TABLE", result: visible }]);
  });

  it("selects tool output spans for the Output section", () => {
    const result = riskResult("risk-1", "secret-value", [
      { field: "tool_result", match: "secret-value" },
    ]);

    expect(
      toolSectionRiskMatches(
        [result],
        '{"token":"secret-value"}',
        "tool_result",
      ),
    ).toEqual([{ value: "secret-value", result }]);
  });
});
