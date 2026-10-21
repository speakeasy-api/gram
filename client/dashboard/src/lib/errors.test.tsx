import { GramError } from "@gram/client/models/errors/gramerror.js";
import { toast } from "sonner";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { handleAPIError, handleError } from "./errors";

vi.mock("sonner", () => ({
  toast: Object.assign(vi.fn(), { error: vi.fn() }),
}));

function gramError(status: number, message = "request failed"): GramError {
  return new GramError(message, {
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
    vi.mocked(toast).mockClear();
  });

  it("presents an impersonation denial as a neutral privacy notice", () => {
    handleError(
      gramError(
        403,
        "sensitive content cannot be accessed while impersonating an organization",
      ),
      { title: "Request failed" },
    );
    expect(toast.error).not.toHaveBeenCalled();
    expect(toast).toHaveBeenCalledWith(
      "Evidence is protected",
      expect.objectContaining({
        description: "Raw evidence is hidden during support access.",
        id: "sensitive-content-protected",
        icon: expect.anything(),
      }),
    );
    expect(console.warn).toHaveBeenCalledOnce();
    expect(console.error).not.toHaveBeenCalled();
  });

  it("does not toast a silent privacy denial", () => {
    handleError(
      gramError(
        403,
        "sensitive content cannot be accessed while impersonating an organization",
      ),
      { silent: true },
    );
    expect(toast).not.toHaveBeenCalled();
    expect(toast.error).not.toHaveBeenCalled();
  });

  it.each([401, 500])(
    "does not relabel status %s as a privacy denial",
    (status) => {
      handleError(
        gramError(
          status,
          "sensitive content cannot be accessed while impersonating an organization",
        ),
      );
      expect(toast).not.toHaveBeenCalled();
      expect(toast.error).toHaveBeenCalledOnce();
    },
  );

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
