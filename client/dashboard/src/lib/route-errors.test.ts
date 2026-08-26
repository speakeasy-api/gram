import { GramError } from "@gram/client/models/errors/gramerror.js";
import { describe, expect, it } from "vitest";
import {
  isGramSessionUnauthorizedError,
  isSessionInfoQueryKey,
  isUnauthorizedError,
} from "./route-errors";
import { queryKeySessionInfo } from "@gram/client/react-query/sessionInfo.core.js";

function gramError(status: number): GramError {
  return new GramError("request failed", {
    response: new Response(null, { status }),
    request: new Request("https://app.getgram.ai/rpc/example"),
    body: "",
  });
}

/**
 * Mimics the AI SDK's MCPClientError: a plain Error subclass carrying the
 * HTTP status as `statusCode`, thrown when a proxied MCP upstream rejects
 * its credentials. Not a Gram error, so it must never be read as Gram
 * session expiry.
 */
function mcpClientError(statusCode: number): Error {
  const error = new Error(
    `MCP HTTP Transport Error: POSTing to endpoint (HTTP ${statusCode})`,
  );
  Object.assign(error, { statusCode });
  return error;
}

describe("isGramSessionUnauthorizedError", () => {
  it("matches a Gram API 401", () => {
    expect(isGramSessionUnauthorizedError(gramError(401))).toBe(true);
  });

  it("ignores Gram errors with other statuses", () => {
    expect(isGramSessionUnauthorizedError(gramError(403))).toBe(false);
    expect(isGramSessionUnauthorizedError(gramError(500))).toBe(false);
  });

  it("ignores non-Gram 401s such as a proxied MCP upstream rejection", () => {
    const error = mcpClientError(401);
    // The broad predicate still sees a 401 — that contrast is the point:
    // only the Gram-scoped predicate may drive the /login redirect.
    expect(isUnauthorizedError(error)).toBe(true);
    expect(isGramSessionUnauthorizedError(error)).toBe(false);
  });

  it("ignores errors with no status at all", () => {
    expect(isGramSessionUnauthorizedError(new Error("network down"))).toBe(
      false,
    );
    expect(isGramSessionUnauthorizedError(null)).toBe(false);
  });
});

describe("isSessionInfoQueryKey", () => {
  it("matches the generated auth.info query key", () => {
    expect(isSessionInfoQueryKey(queryKeySessionInfo({}))).toBe(true);
    expect(
      isSessionInfoQueryKey(queryKeySessionInfo({ gramSession: "abc" })),
    ).toBe(true);
  });

  it("rejects other queries' keys", () => {
    expect(
      isSessionInfoQueryKey(["@gram/client", "toolsets", "list", {}]),
    ).toBe(false);
    expect(isSessionInfoQueryKey([])).toBe(false);
  });
});
