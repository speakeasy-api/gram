import { describe, test, expect, vi, beforeEach, afterEach } from "vitest";
import * as z from "zod";
import { Gram, assert } from "./framework.ts";

test("calls one registered tool", async () => {
  const g = new Gram().tool({
    name: "echo",
    description: "Echoes the input",
    inputSchema: { message: z.string() },
    async execute(ctx, input) {
      return ctx.json({ echoed: input.message });
    },
  });

  const response = await g.handleToolCall({
    name: "echo",
    input: { message: "Hello, world!" },
  });
  expect(response.status).toBe(200);
  expect(response.headers.get("Content-Type")).toBe("application/json");

  const data = await response.json();
  expect(data).toEqual({ echoed: "Hello, world!" });
});

test("calls many registered tools", async () => {
  const g = new Gram()
    .tool({
      name: "echo",
      description: "Echoes the input",
      inputSchema: { message: z.string() },
      async execute(ctx, input) {
        return ctx.json({ echoed: input.message });
      },
    })
    .tool({
      name: "add",
      description: "Add two numbers",
      inputSchema: { a: z.number(), b: z.number() },
      async execute(ctx, input) {
        return ctx.json({ sum: input.a + input.b });
      },
    });

  const res1 = await g.handleToolCall({
    name: "echo",
    input: { message: "Hello, world!" },
  });
  expect(res1.status).toBe(200);
  expect(res1.headers.get("Content-Type")).toBe("application/json");

  let data1 = await res1.json();
  expect(data1).toEqual({ echoed: "Hello, world!" });
  data1 satisfies { echoed: string };

  const res2 = await g.handleToolCall({
    name: "add",
    input: { a: 1, b: 2 },
  });
  expect(res2.status).toBe(200);
  expect(res2.headers.get("Content-Type")).toBe("application/json");

  const data2 = await res2.json();
  expect(data2).toEqual({ sum: 3 });
  data2 satisfies { sum: number };
});

test("throws on unrecognized tool", async () => {
  const g = new Gram()
    .tool({
      name: "echo",
      description: "Echoes the input",
      inputSchema: { message: z.string() },
      async execute(ctx, input) {
        return ctx.json({ echoed: input.message });
      },
    })
    .tool({
      name: "add",
      description: "Add two numbers",
      inputSchema: { a: z.number(), b: z.number() },
      async execute(ctx, input) {
        return ctx.json({ sum: input.a + input.b });
      },
    });

  await expect(
    // Make it look like we have this tool for TypeScript. We just want to
    // test that the right error is thrown at runtime.
    (g as any).handleToolCall({
      name: "fail",
      input: { value: "unreachable" },
    }),
  ).rejects.toThrow("Tool not found: fail");
});

test("supports environment variables", async () => {
  const g = new Gram({
    env: { GREETING: "Hello!" },
    envSchema: { GREETING: z.string() },
  }).tool({
    name: "echo",
    description: "Echoes the input",
    inputSchema: {},
    async execute(ctx) {
      return ctx.json({ echoed: ctx.env?.["GREETING"] || "fail" });
    },
  });

  const response = await g.handleToolCall({
    name: "echo",
    input: {},
  });
  expect(response.status).toBe(200);
  expect(response.headers.get("Content-Type")).toBe("application/json");

  const data = await response.json();
  expect(data).toEqual({ echoed: "Hello!" });
});

test("supports text tools", async () => {
  const g = new Gram()
    .tool({
      name: "add",
      description: "Add two numbers",
      inputSchema: { a: z.number(), b: z.number() },
      async execute(ctx, input) {
        return ctx.json({ sum: input.a + input.b });
      },
    })
    .tool({
      name: "shout",
      description: "Shouts the input",
      inputSchema: { message: z.string() },
      async execute(ctx, input) {
        return ctx.text(input.message.toUpperCase() + "!!!");
      },
    });

  // Call two tools to verify that there are no side effects between calls.
  let response: Response = await g.handleToolCall({
    name: "add",
    input: { a: 1, b: 2 },
  });
  expect(response.status).toBe(200);
  expect(response.headers.get("Content-Type")).toBe("application/json");
  let data: unknown = await response.json();
  expect(data).toEqual({ sum: 3 });

  response = await g.handleToolCall({
    name: "shout",
    input: { message: "hello" },
  });
  expect(response.status).toBe(200);
  expect(response.headers.get("Content-Type")).toBe("text/plain;charset=UTF-8");

  data = await response.text();
  expect(data).toBe("HELLO!!!");
});

test("supports html tools", async () => {
  const g = new Gram().tool({
    name: "shout",
    description: "Shouts the input",
    inputSchema: { message: z.string() },
    async execute(ctx, input) {
      return ctx.html(`<h1>${input.message.toUpperCase()}!!!</h1>`);
    },
  });

  const response = await g.handleToolCall({
    name: "shout",
    input: { message: "hello" },
  });
  expect(response.status).toBe(200);
  expect(response.headers.get("Content-Type")).toBe("text/html");

  const data = await response.text();
  expect(data).toBe("<h1>HELLO!!!</h1>");
});

test("supports plain Response values", async () => {
  const g = new Gram().tool({
    name: "shout",
    description: "Shouts the input",
    inputSchema: { message: z.string() },
    async execute(_ctx, input) {
      return new Response(input.message.toUpperCase() + "!!!", {
        status: 200,
        headers: {
          "Content-Type": "text/plain;charset=UTF-8",
        },
      });
    },
  });

  const response = await g.handleToolCall({
    name: "shout",
    input: { message: "hello" },
  });
  expect(response.status).toBe(200);
  expect(response.headers.get("Content-Type")).toBe("text/plain;charset=UTF-8");

  const data = await response.text();
  expect(data).toBe("HELLO!!!");
});

test("generates a manifest", () => {
  const g = new Gram({
    envSchema: {
      MESSAGE: z.string().describe("The message to shout"),
      API_KEY: z.string(),
    },
  })
    .tool({
      name: "echo",
      inputSchema: { message: z.string() },
      async execute(ctx, input) {
        return ctx.json({ echoed: input.message });
      },
    })
    .tool({
      name: "add",
      description: "Add two numbers",
      inputSchema: { a: z.number(), b: z.number() },
      async execute(ctx, input) {
        return ctx.json({ sum: input.a + input.b });
      },
    })
    .tool({
      name: "shout",
      description: "Shouts the input",
      inputSchema: {},
      async execute(ctx) {
        return ctx.text(ctx.env?.["MESSAGE"]?.toUpperCase() + "!!!");
      },
    });

  expect(g.manifest()).toEqual({
    version: "0.0.0",
    tools: [
      {
        name: "echo",
        inputSchema: expect.objectContaining({
          type: "object",
          properties: { message: { type: "string" } },
          required: ["message"],
        }),
        variables: {
          MESSAGE: { description: "The message to shout" },
          API_KEY: {},
        },
      },
      {
        name: "add",
        description: "Add two numbers",
        inputSchema: expect.objectContaining({
          type: "object",
          properties: { a: { type: "number" }, b: { type: "number" } },
          required: ["a", "b"],
        }),
        variables: {
          MESSAGE: { description: "The message to shout" },
          API_KEY: {},
        },
      },
      {
        name: "shout",
        description: "Shouts the input",
        inputSchema: expect.objectContaining({
          type: "object",
          properties: {},
        }),
        variables: {
          MESSAGE: { description: "The message to shout" },
          API_KEY: {},
        },
      },
    ],
  });
});

test("assert throws response with default status 500", () => {
  expect(() => {
    assert(false, { error: "Something went wrong" });
  }).toThrow(Response);

  try {
    assert(false, { error: "Something went wrong" });
  } catch (err) {
    expect(err).toBeInstanceOf(Response);
    const response = err as Response;
    expect(response.status).toBe(500);
    expect(response.headers.get("Content-Type")).toBe("application/json");
  }
});

test("assert throws response with custom status", async () => {
  try {
    assert(false, { error: "Bad request" }, { status: 400 });
  } catch (err) {
    expect(err).toBeInstanceOf(Response);
    const response = err as Response;
    expect(response.status).toBe(400);
    expect(response.headers.get("Content-Type")).toBe("application/json");

    const data = await response.json();
    expect(data).toMatchObject({ error: "Bad request" });
    expect(data).toHaveProperty("stack");
  }
});

test("assert does not throw when condition is true", () => {
  expect(() => {
    assert(true, { error: "This should not throw" });
  }).not.toThrow();
});

describe("with fake timers", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  test("propagates abort signal", async () => {
    const g = new Gram().tool({
      name: "waiter",
      description: "Waits for a signal",
      inputSchema: {},
      async execute(ctx) {
        const { promise, resolve } = Promise.withResolvers<void>();
        ctx.signal.addEventListener("abort", () => {
          resolve();
        });
        await promise;
        return ctx.json({ done: ctx.signal.aborted });
      },
    });

    const controller = new AbortController();
    const callPromise = g.handleToolCall(
      {
        name: "waiter",
        input: {},
      },
      { signal: controller.signal },
    );

    // Abort after the tool execution has started
    setTimeout(() => controller.abort(), 1000);
    vi.runAllTimers();

    const response = await callPromise;
    expect(response.status).toBe(200);
    expect(response.headers.get("Content-Type")).toBe("application/json");

    const data = await response.json();
    expect(data).toEqual({ done: true });
  });
});
