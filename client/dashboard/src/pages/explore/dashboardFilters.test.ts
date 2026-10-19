import { describe, expect, it } from "vitest";
import { MAX_FILTER_VALUES } from "./exploreModel";
import {
  barValuesFromSaved,
  sameFilters,
  savedFromContext,
  savedPreset,
} from "./dashboardFilters";

const fields = [
  { field: "user", label: "User" },
  { field: "model", label: "Model" },
];
const from = new Date("2026-10-01T00:00:00Z");
const to = new Date("2026-10-03T00:00:00Z");

describe("savedPreset", () => {
  it("is the saved window when the bar offers it, else seven days", () => {
    expect(savedPreset({ range: { preset: "30d" }, values: {} })).toBe("30d");
    expect(savedPreset({ range: { preset: "24h" }, values: {} })).toBe("1d");
    expect(savedPreset({ range: { preset: "weird" }, values: {} })).toBe("7d");
    expect(savedPreset({ range: { from, to }, values: {} })).toBe("7d");
    expect(savedPreset({ values: {} })).toBe("7d");
  });
});

describe("barValuesFromSaved", () => {
  it("opens on the saved window and the values of the fields the bar offers", () => {
    expect(
      barValuesFromSaved(
        { range: { preset: "30d" }, values: { user: ["alice"], team: ["x"] } },
        fields,
      ),
    ).toEqual({
      window: { preset: "30d", customRange: null, customLabel: null },
      filters: { user: ["alice"], model: [] },
    });
  });

  it("opens on a saved absolute range with its label", () => {
    expect(
      barValuesFromSaved(
        { range: { from, to, label: "Launch week" }, values: {} },
        fields,
      ).window,
    ).toEqual({
      preset: null,
      customRange: { from, to },
      customLabel: "Launch week",
    });
  });

  it("opens on seven days with nothing saved", () => {
    expect(barValuesFromSaved({ values: {} }, fields).window).toEqual({
      preset: "7d",
      customRange: null,
      customLabel: null,
    });
  });
});

describe("savedFromContext", () => {
  it("saves the bar's window and the values picked, leaving empty fields out", () => {
    expect(
      savedFromContext(
        {
          window: { preset: "30d", customRange: null, customLabel: null },
          filters: { user: ["alice"], model: [] },
        },
        fields,
      ),
    ).toEqual({ range: { preset: "30d" }, values: { user: ["alice"] } });
    expect(
      savedFromContext(
        {
          window: { preset: null, customRange: { from, to }, customLabel: "L" },
          filters: {},
        },
        fields,
      ),
    ).toEqual({ range: { from, to, label: "L" }, values: {} });
    expect(savedFromContext({}, fields)).toEqual({ values: {} });
  });

  it("saves at most as many values as the cards answer with", () => {
    const picked = Array.from(
      { length: MAX_FILTER_VALUES + 5 },
      (_, i) => `u${i}`,
    );
    expect(
      savedFromContext({ filters: { user: picked } }, fields).values.user,
    ).toEqual(picked.slice(0, MAX_FILTER_VALUES));
  });
});

describe("sameFilters", () => {
  it("treats no saved range as seven days", () => {
    expect(
      sameFilters(
        { values: {} },
        { range: { preset: "7d" }, values: {} },
        fields,
      ),
    ).toBe(true);
    expect(
      sameFilters(
        { values: {} },
        { range: { preset: "30d" }, values: {} },
        fields,
      ),
    ).toBe(false);
  });

  it("compares absolute ranges by their instants and values as sets", () => {
    expect(
      sameFilters(
        { range: { from, to }, values: { user: ["b", "a"] } },
        {
          range: { from: new Date(from), to: new Date(to) },
          values: { user: ["a", "b"] },
        },
        fields,
      ),
    ).toBe(true);
    expect(
      sameFilters(
        { range: { from, to }, values: {} },
        { range: { preset: "7d" }, values: {} },
        fields,
      ),
    ).toBe(false);
    expect(
      sameFilters(
        { values: { user: ["a"] } },
        { values: { user: [] } },
        fields,
      ),
    ).toBe(false);
  });

  it("ignores a value saved for a field the bar no longer offers", () => {
    expect(
      sameFilters({ values: {} }, { values: { status: ["error"] } }, fields),
    ).toBe(true);
  });
});
