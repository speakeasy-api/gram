import { expect, expectTypeOf, test } from "vitest";
import * as z from "zod";
import * as sdk from "./index.ts";
import { Functions, Gram } from "./framework.ts";
import * as mcp from "./mcp.ts";

// The Gram names predate the Speakeasy rename. They must stay the very same
// objects as the new names so existing code keeps working unchanged.

test("Gram is the Functions class", () => {
  expect(Gram).toBe(Functions);
  expect(sdk.Gram).toBe(sdk.Functions);

  const legacy = new Gram();
  expect(legacy).toBeInstanceOf(Functions);
  expect(new Functions()).toBeInstanceOf(Gram);
});

test("Gram works in type positions and as a base class", async () => {
  const tools = new Functions().tool({
    name: "echo",
    inputSchema: { message: z.string() },
    async execute(ctx, input) {
      return ctx.json({ echoed: input.message });
    },
  });

  const asLegacy: Gram<any, any> = tools;
  const asCurrent: Functions<any, any> = asLegacy;
  expectTypeOf<Gram>().toEqualTypeOf<Functions>();

  class Extended extends Gram {}
  expect(new Extended()).toBeInstanceOf(Functions);

  const res = await asCurrent.handleToolCall({
    name: "echo",
    input: { message: "hi" },
  });
  expect(await res.json()).toEqual({ echoed: "hi" });
});

test("legacy MCP helpers are the new ones", () => {
  expect(mcp.fromGram).toBe(mcp.fromFunctions);
  expect(mcp.withGram).toBe(mcp.withFunctions);
});

test("fromFunctions serves a Functions instance", () => {
  const server = mcp.fromFunctions(new Functions(), {
    name: "test",
    version: "0.0.0",
  });
  expect(server).toBeDefined();
});
