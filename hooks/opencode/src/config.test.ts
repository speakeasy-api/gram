import { describe, expect, it } from "vitest";
import { isSecureUrl } from "./config.js";

describe("isSecureUrl", () => {
  it("allows https anywhere", () => {
    expect(isSecureUrl("https://app.getgram.ai")).toBe(true);
  });

  it("allows http only for loopback hosts (local dev)", () => {
    expect(isSecureUrl("http://localhost:8080")).toBe(true);
    expect(isSecureUrl("http://127.0.0.1:8080")).toBe(true);
    expect(isSecureUrl("http://[::1]:8080")).toBe(true);
  });

  it("rejects http to a non-loopback host", () => {
    expect(isSecureUrl("http://evil.example.com")).toBe(false);
  });

  it("rejects garbage", () => {
    expect(isSecureUrl("not-a-url")).toBe(false);
  });
});
