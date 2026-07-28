import { describe, test, expect } from "vitest";
import * as z from "zod";
import { Gram } from "./framework.ts";
import { fromGram } from "./mcp.ts";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";

describe("fromGram", () => {
  async function setup(g: Gram<any, any>) {
    const server = fromGram(g, { name: "test", version: "0.0.0" });
    const [serverTransport, clientTransport] =
      InMemoryTransport.createLinkedPair();
    await server.connect(serverTransport);

    const client = new Client({ name: "test-client", version: "0.0.0" });
    await client.connect(clientTransport);

    return client;
  }

  describe("isError on text responses", () => {
    test("isError is false for a 200 text response", async () => {
      const g = new Gram().tool({
        name: "greet",
        description: "Greets someone",
        inputSchema: { name: z.string() },
        async execute(ctx, input) {
          return ctx.text(`Hello ${input.name}`);
        },
      });

      const client = await setup(g);
      const result = await client.callTool({
        name: "greet",
        arguments: { name: "world" },
      });

      expect(result.isError).toBe(false);
      expect(result.content).toEqual([{ type: "text", text: "Hello world" }]);
    });

    test("isError is true for a 500 text response", async () => {
      const g = new Gram().tool({
        name: "fail",
        description: "Always fails",
        inputSchema: {},
        async execute() {
          return new Response("Internal server error", {
            status: 500,
            headers: { "Content-Type": "text/plain" },
          });
        },
      });

      const client = await setup(g);
      const result = await client.callTool({
        name: "fail",
        arguments: {},
      });

      expect(result.isError).toBe(true);
      expect(result.content).toEqual([
        { type: "text", text: "Internal server error" },
      ]);
    });

    test("isError is true for a 400 text response", async () => {
      const g = new Gram().tool({
        name: "validate",
        description: "Validates input",
        inputSchema: {},
        async execute() {
          return new Response("Bad request", {
            status: 400,
            headers: { "Content-Type": "text/plain" },
          });
        },
      });

      const client = await setup(g);
      const result = await client.callTool({
        name: "validate",
        arguments: {},
      });

      expect(result.isError).toBe(true);
      expect(result.content).toEqual([{ type: "text", text: "Bad request" }]);
    });
  });

  describe("isError on JSON responses", () => {
    test("isError is false for a 200 JSON response", async () => {
      const g = new Gram().tool({
        name: "add",
        description: "Adds numbers",
        inputSchema: { a: z.number(), b: z.number() },
        async execute(ctx, input) {
          return ctx.json({ sum: input.a + input.b });
        },
      });

      const client = await setup(g);
      const result = await client.callTool({
        name: "add",
        arguments: { a: 1, b: 2 },
      });

      expect(result.isError).toBe(false);
      expect(result.content).toEqual([{ type: "text", text: '{"sum":3}' }]);
    });

    test("isError is true for a 500 JSON response", async () => {
      const g = new Gram().tool({
        name: "fail",
        description: "Always fails with JSON",
        inputSchema: {},
        async execute() {
          return new Response(JSON.stringify({ error: "boom" }), {
            status: 500,
            headers: { "Content-Type": "application/json" },
          });
        },
      });

      const client = await setup(g);
      const result = await client.callTool({
        name: "fail",
        arguments: {},
      });

      expect(result.isError).toBe(true);
      expect(result.content).toEqual([
        { type: "text", text: '{"error":"boom"}' },
      ]);
    });
  });

  describe("isError on image responses", () => {
    test("isError is false for a 200 image response", async () => {
      const pixel = Buffer.from(
        "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg==",
        "base64",
      );

      const g = new Gram().tool({
        name: "pixel",
        description: "Returns a pixel",
        inputSchema: {},
        async execute() {
          return new Response(pixel, {
            status: 200,
            headers: { "Content-Type": "image/png" },
          });
        },
      });

      const client = await setup(g);
      const result = await client.callTool({
        name: "pixel",
        arguments: {},
      });

      expect(result.isError).toBe(false);
      expect(result.content).toEqual([
        expect.objectContaining({ type: "image", mimeType: "image/png" }),
      ]);
    });

    test("isError is true for a 500 image response", async () => {
      const g = new Gram().tool({
        name: "broken-image",
        description: "Fails to generate image",
        inputSchema: {},
        async execute() {
          return new Response(Buffer.from("err"), {
            status: 500,
            headers: { "Content-Type": "image/png" },
          });
        },
      });

      const client = await setup(g);
      const result = await client.callTool({
        name: "broken-image",
        arguments: {},
      });

      expect(result.isError).toBe(true);
    });
  });

  describe("isError on unhandled content types", () => {
    test("isError is true for an unrecognized content type", async () => {
      const g = new Gram().tool({
        name: "binary",
        description: "Returns binary data",
        inputSchema: {},
        async execute() {
          return new Response("data", {
            status: 200,
            headers: { "Content-Type": "application/octet-stream" },
          });
        },
      });

      const client = await setup(g);
      const result = await client.callTool({
        name: "binary",
        arguments: {},
      });

      expect(result.isError).toBe(true);
      expect(result.content).toEqual([
        {
          type: "text",
          text: "Unhandled content type: application/octet-stream. Create a handler for this type in the MCP server.",
        },
      ]);
    });
  });

  // Regression coverage for AGE-2779: errors raised while handling a tool call
  // must be intercepted and surfaced as normal `isError` results rather than
  // bubbling up as an opaque MCP "Internal Error".
  describe("error interception", () => {
    test("ctx.fail() yields an isError result with only the message", async () => {
      const g = new Gram().tool({
        name: "fail",
        description: "Fails via ctx.fail",
        inputSchema: {},
        async execute(ctx) {
          return ctx.fail({ error: "something broke" }, { status: 500 });
        },
      });

      const client = await setup(g);
      // Must resolve (not reject): the thrown Response is intercepted.
      const result = await client.callTool({ name: "fail", arguments: {} });

      expect(result.isError).toBe(true);
      expect(result.content).toEqual([
        { type: "text", text: '{"error":"something broke"}' },
      ]);
      // No stack trace leaks into the user-facing message.
      expect(JSON.stringify(result.content)).not.toContain("stack");
    });

    test("a thrown JavaScript error is reported with its message", async () => {
      const g = new Gram().tool({
        name: "throws",
        description: "Throws a raw error",
        inputSchema: {},
        async execute() {
          throw new Error("kaboom");
        },
      });

      const client = await setup(g);
      const result = await client.callTool({ name: "throws", arguments: {} });

      expect(result.isError).toBe(true);
      expect(result.content).toEqual([{ type: "text", text: "kaboom" }]);
    });

    test("an input validation failure yields an isError result", async () => {
      const g = new Gram().tool({
        name: "needsName",
        description: "Requires a name",
        inputSchema: { name: z.string() },
        async execute(ctx, input) {
          return ctx.text(`Hello ${input.name}`);
        },
      });

      const client = await setup(g);
      // Missing required `name` triggers validation failure (ctx.fail, status 400).
      const result = await client.callTool({
        name: "needsName",
        arguments: {},
      });

      expect(result.isError).toBe(true);
      expect(JSON.stringify(result.content)).not.toContain("stack");
    });
  });

  describe("clientInfo on ToolContext", () => {
    // A tool that echoes back whatever `ctx.clientInfo` it was given.
    const echoClientInfo = new Gram().tool({
      name: "whoami",
      description: "Echoes the calling client's info",
      inputSchema: {},
      async execute(ctx) {
        return ctx.json({ clientInfo: ctx.clientInfo ?? null });
      },
    });

    function parseClientInfo(result: Awaited<ReturnType<Client["callTool"]>>) {
      const content = result.content as Array<{ type: string; text: string }>;
      return JSON.parse(content[0]!.text).clientInfo;
    }

    test("populates ctx.clientInfo from the initialize handshake", async () => {
      const client = await setup(echoClientInfo);
      const result = await client.callTool({ name: "whoami", arguments: {} });

      // setup() connects a Client named "test-client"; that identity is what
      // the server captures during the MCP initialize handshake.
      expect(parseClientInfo(result)).toEqual({
        name: "test-client",
        version: "0.0.0",
      });
    });

    test("prefers per-call _meta clientInfo over the handshake identity", async () => {
      const client = await setup(echoClientInfo);
      const result = await client.callTool({
        name: "whoami",
        arguments: {},
        _meta: {
          "io.modelcontextprotocol/clientInfo": {
            name: "vega",
            version: "2.1.0",
          },
        },
      });

      expect(parseClientInfo(result)).toEqual({
        name: "vega",
        version: "2.1.0",
      });
    });

    test("ignores a malformed _meta clientInfo and falls back to the handshake", async () => {
      const client = await setup(echoClientInfo);
      const result = await client.callTool({
        name: "whoami",
        arguments: {},
        // Missing `name` → not a usable clientInfo; fall back to the handshake.
        _meta: { "io.modelcontextprotocol/clientInfo": { version: "9.9.9" } },
      });

      expect(parseClientInfo(result)).toEqual({
        name: "test-client",
        version: "0.0.0",
      });
    });
  });
});
