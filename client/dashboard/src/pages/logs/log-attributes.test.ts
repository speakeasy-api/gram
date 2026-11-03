import { describe, expect, it } from "vitest";
import { httpStatusCode } from "./log-attributes";

describe("httpStatusCode", () => {
  it("reads a numeric status as stored", () => {
    expect(httpStatusCode({ http: { response: { status_code: 200 } } })).toBe(
      200,
    );
  });

  it("reads a status stored as a string", () => {
    expect(httpStatusCode({ http: { response: { status_code: "500" } } })).toBe(
      500,
    );
  });

  it("has no status when the attribute is missing", () => {
    expect(httpStatusCode({ http: {} })).toBeNaN();
    expect(
      httpStatusCode({ http: { response: { status_code: "" } } }),
    ).toBeNaN();
  });
});
