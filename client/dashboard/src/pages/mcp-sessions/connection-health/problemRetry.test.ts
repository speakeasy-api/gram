import { GramError } from "@gram/client/models/errors/gramerror.js";
import { describe, expect, it } from "vitest";
import { problemRetry } from "./api";

function apiError(status: number): GramError {
  return new GramError("request failed", {
    response: new Response(null, { status }),
    request: new Request("https://app.example.com/rpc/example"),
    body: "",
  });
}

describe("problemRetry", () => {
  it("does not retry a read that ran out of time", () => {
    expect(problemRetry(0, apiError(503))).toBe(false);
  });

  it("retries other server errors, not refusals", () => {
    expect(problemRetry(0, apiError(500))).toBe(true);
    expect(problemRetry(0, apiError(404))).toBe(false);
  });
});
