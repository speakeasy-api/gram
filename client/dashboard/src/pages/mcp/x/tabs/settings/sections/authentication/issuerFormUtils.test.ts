import { describe, expect, it } from "vitest";
import { availableClientTypes } from "./issuerFormUtils";

describe("availableClientTypes", () => {
  it.each([
    {
      capabilities: { cimdAvailable: true, dcrAvailable: true },
      expected: ["cimd", "dcr", "manual"],
    },
    {
      capabilities: { cimdAvailable: true, dcrAvailable: false },
      expected: ["cimd", "manual"],
    },
    {
      capabilities: { cimdAvailable: false, dcrAvailable: true },
      expected: ["dcr", "manual"],
    },
    {
      capabilities: { cimdAvailable: false, dcrAvailable: false },
      expected: ["manual"],
    },
  ])("orders $expected for $capabilities", ({ capabilities, expected }) => {
    expect(availableClientTypes(capabilities)).toEqual(expected);
  });
});
