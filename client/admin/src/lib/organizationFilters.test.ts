// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  filtersToSearch,
  NO_FILTERS,
  statusParams,
  statusSelection,
  internalParams,
  internalSelection,
} from "./organizationFilters";

describe("organization status", () => {
  it.each(["all", "active", "disabled"] as const)(
    "writes only canonical %s and maps its API query",
    (status) => {
      const filters = {
        ...NO_FILTERS,
        type: ["pro"],
        trial: ["running"],
        disabled: status === "all" ? [] : [status],
      };
      const search = filtersToSearch(filters);
      expect(search).toEqual({
        type: ["pro"],
        trial: ["running"],
        disabledStatus: status === "all" ? undefined : status,
      });
      expect(search).not.toHaveProperty("disabled");
      expect(search).not.toHaveProperty("disabledOnly");
      expect(statusParams(search)).toEqual({ disabled_status: status });
    },
  );

  it.each(["all", "active", "disabled"] as const)(
    "canonical %s wins over every legacy selection",
    (status) => {
      for (const disabledOnly of [true, false, undefined]) {
        for (const disabled of [
          undefined,
          ["active"],
          ["disabled"],
          ["active", "disabled"],
        ] as const) {
          expect(
            statusSelection({
              disabledStatus: status,
              disabledOnly,
              disabled: disabled ? [...disabled] : undefined,
            }),
          ).toEqual(status === "all" ? [] : [status]);
        }
      }
    },
  );

  it("treats both legacy states and cleared filters as All", () => {
    expect(
      filtersToSearch({ ...NO_FILTERS, disabled: ["active", "disabled"] })
        .disabledStatus,
    ).toBeUndefined();
    expect(statusParams(filtersToSearch(NO_FILTERS))).toEqual({
      disabled_status: "all",
    });
    expect(
      statusSelection({ disabledOnly: false, disabled: ["active"] }),
    ).toEqual([]);
  });
});

describe("internal label", () => {
  it("maps the internal filter to the request", () => {
    expect(internalParams({})).toEqual({ internal_status: "all" });
    expect(internalParams({ internalStatus: "all" })).toEqual({
      internal_status: "all",
    });
    expect(internalParams({ internalStatus: "internal" })).toEqual({
      internal_status: "internal",
    });
    expect(internalParams({ internalStatus: "external" })).toEqual({
      internal_status: "external",
    });
  });

  it("writes only a chosen internal status to the URL", () => {
    expect(
      filtersToSearch({ ...NO_FILTERS, internal: ["external"] }).internalStatus,
    ).toBe("external");
    expect(
      filtersToSearch({ ...NO_FILTERS, internal: ["internal"] }).internalStatus,
    ).toBe("internal");
    expect(filtersToSearch(NO_FILTERS).internalStatus).toBeUndefined();
    // Both at once is every organization, which is the default and so absent.
    expect(
      filtersToSearch({ ...NO_FILTERS, internal: ["internal", "external"] })
        .internalStatus,
    ).toBeUndefined();
  });

  it("reads the chosen status back for the controls", () => {
    expect(internalSelection({})).toEqual([]);
    expect(internalSelection({ internalStatus: "all" })).toEqual([]);
    expect(internalSelection({ internalStatus: "external" })).toEqual([
      "external",
    ]);
  });
});
