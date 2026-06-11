import { describe, expect, it } from "vitest";
import { splitDisplayName } from "./demo-booking";

describe("splitDisplayName", () => {
  it("splits a two-part name", () => {
    expect(splitDisplayName("Jane Smith")).toEqual({
      firstName: "Jane",
      lastName: "Smith",
    });
  });
  it("keeps everything after the first space as the last name", () => {
    expect(splitDisplayName("Jane Van Der Berg")).toEqual({
      firstName: "Jane",
      lastName: "Van Der Berg",
    });
  });
  it("treats a single token as the first name", () => {
    expect(splitDisplayName("Cher")).toEqual({
      firstName: "Cher",
      lastName: "",
    });
  });
  it("returns empty strings for missing/blank input", () => {
    expect(splitDisplayName(undefined)).toEqual({
      firstName: "",
      lastName: "",
    });
    expect(splitDisplayName("   ")).toEqual({ firstName: "", lastName: "" });
  });
  it("trims surrounding whitespace", () => {
    expect(splitDisplayName("  Jane Smith  ")).toEqual({
      firstName: "Jane",
      lastName: "Smith",
    });
  });
});
