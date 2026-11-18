// @vitest-environment node
import { safeBase64Decode } from "@/components/observe/observeFilterUtils";
import { describe, expect, it } from "vitest";
import {
  logsSearch,
  presetWindow,
  RETENTION_MS,
  startsBeforeRetention,
  toAttemptFilters,
  type LogFilterValues,
} from "./filters";

const NOW = new Date(2026, 10, 17, 11, 5, 0);

function values(overrides: Partial<LogFilterValues> = {}): LogFilterValues {
  return {
    date: { preset: "1d", customRange: null, customLabel: null },
    outcome: [],
    server: null,
    person: null,
    service: null,
    app: null,
    appVersion: null,
    step: [],
    message: [],
    network: null,
    ...overrides,
  };
}

describe("toAttemptFilters", () => {
  it("leaves unset dimensions out so the API reads them as all", () => {
    expect(toAttemptFilters(values())).toEqual({
      outcomes: undefined,
      serverIds: undefined,
      services: undefined,
      people: undefined,
      apps: undefined,
      appVersions: undefined,
      steps: undefined,
      reasons: undefined,
      networks: undefined,
    });
  });

  it("maps URL values to the contract's filter names and drops unknown enums", () => {
    const filters = toAttemptFilters(
      values({
        outcome: ["failed", "exploded"],
        step: ["sign_in_renewal", "nope"],
        person: "user:user_dana",
        message: ["session_expired"],
        app: "unlisted",
      }),
    );
    expect(filters.outcomes).toEqual(["failed"]);
    expect(filters.steps).toEqual(["sign_in_renewal"]);
    expect(filters.people).toEqual(["user:user_dana"]);
    expect(filters.reasons).toEqual(["session_expired"]);
    expect(filters.apps).toEqual([""]);
  });

  it("round-trips the unlisted app through the URL", () => {
    const params = new URLSearchParams(logsSearch({ apps: [""] }, NOW));
    expect(params.get("app")).toBe("unlisted");
  });
});

describe("logsSearch", () => {
  it("opens an open-ended window labeled from its start", () => {
    const params = new URLSearchParams(
      logsSearch(
        {
          from: new Date(2026, 10, 17, 9, 48).toISOString(),
          services: ["linear.app"],
          outcomes: ["failed"],
          reasons: ["a", "b", "c"],
        },
        NOW,
      ),
    );
    expect(params.get("to")).toBe(NOW.toISOString());
    expect(safeBase64Decode(params.get("label")!)).toBe("Since 09:48");
    expect(params.get("service")).toBe("linear.app");
    expect(params.get("outcome")).toBe("failed");
    expect(params.get("message")).toBe("a,b,c");
  });

  it("keeps single-value dimensions to one value", () => {
    const params = new URLSearchParams(
      logsSearch({ serverIds: ["one", "two"], range: "7d" }, NOW),
    );
    expect(params.get("server")).toBe("one");
    expect(params.get("range")).toBe("7d");
  });

  it("writes nothing for the default range", () => {
    expect(logsSearch({ range: "1d" }, NOW)).toBe("");
  });
});

describe("retention", () => {
  it("accepts the last 30 days and refuses anything older", () => {
    expect(startsBeforeRetention(presetWindow("30d", NOW), NOW)).toBe(false);
    const older = {
      from: new Date(NOW.getTime() - RETENTION_MS - 3_600_000).toISOString(),
      to: NOW.toISOString(),
    };
    expect(startsBeforeRetention(older, NOW)).toBe(true);
  });
});
