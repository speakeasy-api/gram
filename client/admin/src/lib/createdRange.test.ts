import { describe, expect, it } from "vitest";
import {
  createdRange,
  createdRangeErrors,
  createdPresetRange,
  recognizeCreatedPreset,
} from "./createdRange";

describe("inclusive UTC created days", () => {
  it.each([
    ["today", "2024-03-01"],
    ["7", "2024-02-24"],
    ["14", "2024-02-17"],
    ["30", "2024-02-01"],
  ] as const)(
    "resolves %s including today across leap February",
    (preset, from) => {
      expect(
        createdPresetRange(preset, new Date("2024-02-29T18:30:00-08:00")),
      ).toEqual({ createdFrom: from, createdTo: "2024-03-01" });
    },
  );
  it.each([
    ["2023-03-01T00:01:00Z", "7", "2023-02-23"],
    ["2025-01-01T00:01:00Z", "14", "2024-12-19"],
    ["2025-01-01T00:01:00Z", "30", "2024-12-03"],
    ["2024-03-11T00:01:00Z", "7", "2024-03-05"],
  ] as const)(
    "handles month/year and DST boundaries %s %s",
    (now, preset, from) => {
      const range = createdPresetRange(preset, new Date(now));
      expect(range).toEqual({ createdFrom: from, createdTo: now.slice(0, 10) });
      expect(recognizeCreatedPreset(range, new Date(now))).toBe(preset);
    },
  );
  it("validates Gregorian leap centuries", () => {
    expect(createdRangeErrors({ createdFrom: "2000-02-29" })).toEqual({});
    expect(createdRangeErrors({ createdTo: "1900-02-29" })).toHaveProperty(
      "createdTo",
    );
  });
  it("crosses years and uses UTC rather than local days", () => {
    expect(createdPresetRange("7", new Date("2025-01-01T00:01:00Z"))).toEqual({
      createdFrom: "2024-12-26",
      createdTo: "2025-01-01",
    });
  });
  it.each([
    "2023-02-29",
    "2024-02-30",
    "2024-04-31",
    "2024-13-01",
    "2024-1-01",
    " 2024-01-01",
    "0000-01-01",
    "2024-01-01T00:00:00Z",
    20240101,
    null,
  ])("drops invalid bound %s independently", (createdFrom) => {
    expect(createdRange({ createdFrom, createdTo: "2024-02-29" })).toEqual({
      createdFrom: undefined,
      createdTo: "2024-02-29",
    });
    expect(createdRangeErrors({ createdFrom })).toHaveProperty("createdFrom");
  });
  it("accepts independent inclusive endpoints and rejects reversed pairs", () => {
    for (const range of [
      { createdFrom: "2024-02-29" },
      { createdTo: "2024-02-29" },
      { createdFrom: "2024-02-29", createdTo: "2024-02-29" },
    ]) {
      expect(createdRangeErrors(range)).toEqual({});
      expect(createdRange(range)).toMatchObject(range);
    }
    expect(
      createdRange({ createdFrom: "2025-01-01", createdTo: "2024-12-31" }),
    ).toEqual({ createdFrom: undefined, createdTo: undefined });
    expect(
      createdRangeErrors({
        createdFrom: "2025-01-01",
        createdTo: "2024-12-31",
      }),
    ).toHaveProperty("createdTo");
    expect(
      createdRangeErrors({ createdFrom: "", createdTo: undefined }),
    ).toEqual({});
  });
  it("recognizes snapshots against the current UTC day, never stale Today", () => {
    const range = createdPresetRange("today", new Date("2024-12-31T23:59:59Z"));
    expect(
      recognizeCreatedPreset(range, new Date("2024-12-31T23:59:59Z")),
    ).toBe("today");
    expect(
      recognizeCreatedPreset(range, new Date("2025-01-01T00:00:00Z")),
    ).toBe("custom");
    expect(recognizeCreatedPreset({})).toBe("all");
    expect(createdPresetRange("all")).toEqual({
      createdFrom: undefined,
      createdTo: undefined,
    });
  });
});
