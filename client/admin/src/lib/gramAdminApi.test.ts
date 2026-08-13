import { afterEach, describe, expect, it, vi } from "vitest";
import {
  GramAdminError,
  errorMessage,
  logout,
  toSearchParams,
} from "@/lib/gramAdminApi";

describe("toSearchParams", () => {
  it("repeats the key for each item of an array", () => {
    const qs = toSearchParams({ type: ["free", "pro"], q: "", page: 2 });
    expect(qs.toString()).toBe("type=free&type=pro&page=2");
  });

  it("omits the values the API reads as unset", () => {
    const qs = toSearchParams({
      q: undefined,
      cursor: "",
      type: [],
      include_disabled: false,
    });
    expect(qs.toString()).toBe("");
  });

  it("encodes a value that needs escaping", () => {
    const qs = toSearchParams({ q: "a b&c" });
    expect(qs.toString()).toBe("q=a+b%26c");
  });
});

describe("errorMessage", () => {
  it("prefers the message the server sent when the operator can act on it", () => {
    const e = new GramAdminError(
      400,
      { name: "bad_request", message: "at least one field must be supplied" },
      "gram admin 400 Bad Request",
    );
    expect(errorMessage(e)).toBe("at least one field must be supplied");
  });

  // A 5xx message is the handler's verb phrase, "list organizations", not a
  // sentence. The status line says more.
  it("keeps the status line for a server fault", () => {
    const e = new GramAdminError(
      500,
      { name: "internal", message: "list organizations" },
      "gram admin 500 Internal Server Error",
    );
    expect(errorMessage(e)).toBe("gram admin 500 Internal Server Error");
  });

  it("falls back to the status line when the body carries no message", () => {
    const e = new GramAdminError(404, null, "gram admin 404 Not Found");
    expect(errorMessage(e)).toBe("gram admin 404 Not Found");
  });
});

describe("logout", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("reads a 204 as success and asks for the account chooser", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(null, { status: 204 })),
    );

    await logout();

    expect(window.location.href).toContain("prompt=select_account");
  });

  // The 401 handler retries with prompt=none, which the provider honours
  // silently. Taking it here would sign the operator back in behind the Logout
  // they just pressed.
  it("reports a 401 instead of signing the operator back in", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(null, { status: 401 })),
    );
    const before = window.location.href;

    await expect(logout()).rejects.toThrow(GramAdminError);

    expect(window.location.href).toBe(before);
  });
});
