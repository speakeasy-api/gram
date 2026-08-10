import { describe, expect, it } from "vitest";
import { toolCapability } from "./toolCapability";

describe("toolCapability", () => {
  it("classifies read-only tools as read", () => {
    expect(toolCapability({ readOnlyHint: true })).toBe("read");
    // read-only wins even when other hints are set
    expect(toolCapability({ readOnlyHint: true, destructiveHint: true })).toBe(
      "read",
    );
  });

  it("classifies explicitly destructive, non-read-only tools as destructive", () => {
    expect(toolCapability({ readOnlyHint: false, destructiveHint: true })).toBe(
      "destructive",
    );
  });

  it("classifies non-read-only, non-destructive tools as write", () => {
    expect(
      toolCapability({ readOnlyHint: false, destructiveHint: false }),
    ).toBe("write");
    expect(toolCapability({ readOnlyHint: false })).toBe("write");
  });

  it("returns null when the source asserts nothing", () => {
    expect(toolCapability({})).toBeNull();
    expect(toolCapability(undefined)).toBeNull();
    expect(toolCapability(null)).toBeNull();
    // idempotent/open-world alone don't imply read vs write
    expect(
      toolCapability({ idempotentHint: true, openWorldHint: true }),
    ).toBeNull();
  });

  it("maps OpenAPI HTTP-method-derived hints the way the server infers them", () => {
    // GET → read-only
    expect(
      toolCapability({
        readOnlyHint: true,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: true,
      }),
    ).toBe("read");
    // POST → write
    expect(
      toolCapability({
        readOnlyHint: false,
        destructiveHint: false,
        idempotentHint: false,
        openWorldHint: true,
      }),
    ).toBe("write");
    // DELETE → destructive
    expect(
      toolCapability({
        readOnlyHint: false,
        destructiveHint: true,
        idempotentHint: true,
        openWorldHint: true,
      }),
    ).toBe("destructive");
  });
});
