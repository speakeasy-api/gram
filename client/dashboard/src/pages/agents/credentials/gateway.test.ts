import { describe, expect, it } from "vitest";
import { isCredentialSafe } from "./gateway";

describe("isCredentialSafe", () => {
  it.each([
    "https://api.example.com/mcp/gateway/acme/projects/main",
    "http://localhost:3000/mcp",
    "http://127.0.0.1:3000/mcp",
    "http://[::1]:3000/mcp",
  ])("accepts credential-safe endpoint %s", (url) =>
    expect(isCredentialSafe(url)).toBe(true),
  );

  it.each([
    "http://api.example.com/mcp",
    "not a url",
    "ftp://api.example.com/mcp",
  ])("rejects unsafe endpoint %s", (url) =>
    expect(isCredentialSafe(url)).toBe(false),
  );
});
