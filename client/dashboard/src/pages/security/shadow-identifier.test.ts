import { describe, expect, it } from "vitest";
import { shadowServerFacts } from "./shadow-identifier";

describe("shadowServerFacts", () => {
  it("reads a scoped npm package and version from an npx command", () => {
    expect(shadowServerFacts("npx -y @acme/notion-mcp@1.4.2")).toEqual({
      transport: "stdio",
      pkg: "@acme/notion-mcp",
      version: "1.4.2",
    });
  });

  it("reads a pinned Python package", () => {
    expect(shadowServerFacts("uvx mcp-server-git==0.6.2")).toEqual({
      transport: "stdio",
      pkg: "mcp-server-git",
      version: "0.6.2",
    });
  });

  it("keeps an unversioned scoped package whole", () => {
    expect(shadowServerFacts("npx @acme/tool").pkg).toBe("@acme/tool");
  });

  it("reports the host for a remote URL", () => {
    expect(shadowServerFacts("https://mcp.example.test/sse")).toEqual({
      transport: "http",
      pkg: "mcp.example.test",
      version: null,
    });
  });
});
