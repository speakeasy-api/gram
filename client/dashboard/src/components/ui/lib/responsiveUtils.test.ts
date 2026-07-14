import { describe, expect, test } from "vitest";
import { resolveSizeForBreakpoint } from "./responsiveUtils";
import { breakpoints, sizes } from "@/components/ui/lib/types";

describe("resolveSizeForBreakpoint", () => {
  test("single value returns the same value", () => {
    breakpoints.forEach((breakpoint) => {
      sizes.forEach((size) => {
        const result = resolveSizeForBreakpoint(breakpoint, size);

        expect(result).toBe(size);
      });
    });
  });

  test("empty object return fallback", () => {
    breakpoints.forEach((breakpoint) => {
      const result = resolveSizeForBreakpoint(breakpoint, {}, "small");

      expect(result).toBe("small");
    });
  });

  test("returns nearest breakpoint size", () => {
    expect(
      resolveSizeForBreakpoint("2xl", {
        sm: "small",
        md: "medium",
        lg: "large",
      }),
    ).toBe("large");

    expect(
      resolveSizeForBreakpoint("lg", {
        sm: "small",
        md: "medium",
        lg: "large",
      }),
    ).toBe("large");

    expect(
      resolveSizeForBreakpoint("md", {
        sm: "small",
        md: "medium",
        lg: "large",
      }),
    ).toBe("medium");

    expect(
      resolveSizeForBreakpoint("sm", {
        sm: "small",
        md: "medium",
        lg: "large",
      }),
    ).toBe("small");
  });
});
