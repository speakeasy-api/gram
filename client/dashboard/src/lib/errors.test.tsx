import { GramError } from "@gram/client/models/errors/gramerror.js";
import { toast } from "sonner";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { handleAPIError, handleError } from "./errors";

vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));

function gramError(status: number): GramError {
  return new GramError("request failed", {
    response: new Response(null, { status }),
    request: new Request("https://app.getgram.ai/rpc/example"),
    body: "",
  });
}

describe("handleError", () => {
  beforeEach(() => {
    vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.spyOn(console, "error").mockImplementation(() => {});
  });
  afterEach(() => {
    vi.restoreAllMocks();
    vi.mocked(toast.error).mockClear();
  });

  // A 4xx is the API answering a request it understood; it belongs in the
  // toast, not in RUM's error count that pages on-call.
  it("logs an expected 4xx below error level and still toasts", () => {
    handleError(gramError(403));

    expect(console.warn).toHaveBeenCalledOnce();
    expect(console.error).not.toHaveBeenCalled();
    expect(toast.error).toHaveBeenCalledOnce();
  });

  it("logs a 5xx as an error", () => {
    handleError(gramError(503));

    expect(console.error).toHaveBeenCalledOnce();
    expect(console.warn).not.toHaveBeenCalled();
    expect(toast.error).toHaveBeenCalledOnce();
  });

  it("logs a non-API error as an error", () => {
    handleError(new Error("boom"));

    expect(console.error).toHaveBeenCalledOnce();
    expect(console.warn).not.toHaveBeenCalled();
  });

  it("keeps the SDK error through handleAPIError", () => {
    handleAPIError(gramError(404), "fallback");

    expect(console.warn).toHaveBeenCalledOnce();
    expect(console.error).not.toHaveBeenCalled();
    expect(toast.error).toHaveBeenCalledOnce();
  });
});
