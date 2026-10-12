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

  it("reports no version for a trailing separator", () => {
    expect(shadowServerFacts("npx pkg@").version).toBeNull();
    expect(shadowServerFacts("uvx pkg==").version).toBeNull();
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

  it("skips docker option values to find the image", () => {
    expect(
      shadowServerFacts(
        "docker run --rm -i -v /host:/container -e TOKEN --name=mcp acme/mcp-image:1.2",
      ).pkg,
    ).toBe("acme/mcp-image:1.2");
  });

  it("skips uvx option values to find the package", () => {
    expect(shadowServerFacts("uvx --python 3.12 mcp-server-git").pkg).toBe(
      "mcp-server-git",
    );
  });
});
