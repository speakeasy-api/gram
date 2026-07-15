import { describe, expect, test } from "vitest";
import { getResponsiveClasses } from "./utils";

describe("lib/utils", () => {
  describe("getResponsiveClasses", () => {
    const gapMapper = (gap: number) => `gap-${gap}`;

    test("returns responsive classes for object", () => {
      const responsiveClasses = getResponsiveClasses(
        {
          xs: 0,
          sm: 1,
          md: 2,
          lg: 3,
          xl: 4,
          "2xl": 5,
        },
        gapMapper,
      );
      expect(responsiveClasses).toEqual(
        "gap-0 sm:gap-1 md:gap-2 lg:gap-3 xl:gap-4 2xl:gap-5",
      );
    });

    test("filters out undefined values", () => {
      const responsiveClasses = getResponsiveClasses(
        {
          xs: 0,
          sm: 1,
          md: 2,
          lg: undefined,
          xl: 4,
          "2xl": 5,
        },
        gapMapper,
      );
      expect(responsiveClasses).toEqual(
        "gap-0 sm:gap-1 md:gap-2 xl:gap-4 2xl:gap-5",
      );
    });

    test("return single class for single value", () => {
      const responsiveClasses = getResponsiveClasses(5, gapMapper);
      expect(responsiveClasses).toEqual("gap-5");
    });
  });
});
