// @vitest-environment node
import { GramError } from "@gram/client/models/errors/gramerror.js";
import type { ReactElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
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

  // The SDK's fallback message is the raw response dump; the toast shows
  // plain copy instead, and the API's own message whenever it has one.
  it("shows plain copy instead of the SDK's raw response dump", () => {
    handleError(
      new GramError('API error occurred: Status 502 Body: {"x":1}', {
        response: new Response(null, { status: 502 }),
        request: new Request("https://app.getgram.ai/rpc/example"),
        body: '{"x":1}',
      }),
      { title: "Couldn't rename the server" },
    );

    const markup = renderToStaticMarkup(
      vi.mocked(toast.error).mock.calls[0]?.[0] as ReactElement,
    );
    expect(markup).toContain("Couldn&#x27;t rename the server");
    expect(markup).not.toContain("API error occurred");
    expect(markup).toContain("The service could not be reached");
  });

  it("shows the API's own message under the title", () => {
    handleError(new Error("The inventory is being updated."), {
      title: "Couldn't rename the server",
    });

    const markup = renderToStaticMarkup(
      vi.mocked(toast.error).mock.calls[0]?.[0] as ReactElement,
    );
    expect(markup).toContain("The inventory is being updated.");
  });

  it("keeps the SDK error through handleAPIError", () => {
    handleAPIError(gramError(404), "fallback");

    expect(console.warn).toHaveBeenCalledOnce();
    expect(console.error).not.toHaveBeenCalled();
    expect(toast.error).toHaveBeenCalledOnce();
  });
});
