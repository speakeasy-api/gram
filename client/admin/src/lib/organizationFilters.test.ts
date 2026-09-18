import { describe, expect, it } from "vitest";
import {
  filtersToSearch,
  NO_FILTERS,
  statusParams,
  statusSelection,
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
