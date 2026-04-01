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
});
