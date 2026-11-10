import { describe, expect, it } from "vitest";
import { registryRemotes, urlPlaceholders } from "./registryRemotes";

describe("registryRemotes", () => {
  it("reads each remote's URL, type and placeholders", () => {
    const remotes = registryRemotes(
      JSON.stringify({
        server: {
          remotes: [
            { type: "streamable-http", url: "https://mcp.example.test/mcp" },
            { type: "sse", url: "https://{region}.example.test/{tenant}/sse" },
          ],
        },
      }),
    );

    expect(remotes).toEqual([
      {
        type: "streamable-http",
        url: "https://mcp.example.test/mcp",
        placeholders: [],
      },
      {
        type: "sse",
        url: "https://{region}.example.test/{tenant}/sse",
        placeholders: ["region", "tenant"],
      },
    ]);
  });

  it("skips malformed records and remotes instead of throwing", () => {
    expect(registryRemotes("not json")).toEqual([]);
    expect(registryRemotes(JSON.stringify({ server: {} }))).toEqual([]);
    expect(
      registryRemotes(
        JSON.stringify({
          server: { remotes: [{ url: 7 }, null, { url: "" }] },
        }),
      ),
    ).toEqual([]);
  });
});

describe("urlPlaceholders", () => {
  it("lists each placeholder once, in order", () => {
    expect(urlPlaceholders("https://{a}.test/{b}/{a}")).toEqual(["a", "b"]);
    expect(urlPlaceholders("https://plain.test/mcp")).toEqual([]);
  });
});
