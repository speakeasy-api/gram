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

test("supports markdown tools", async () => {
  const g = new Gram().tool({
    name: "shout",
    description: "Shouts the input",
    inputSchema: { message: z.string() },
    async execute(ctx, input) {
      return ctx.markdown(`# ${input.message.toUpperCase()}!!!`);
    },
  });

  const response = await g.handleToolCall({
    name: "shout",
    input: { message: "hello" },
  });
  expect(response.status).toBe(200);
  expect(response.headers.get("Content-Type")).toBe(
    "text/markdown;charset=UTF-8",
  );

  const data = await response.text();
  expect(data).toBe("# HELLO!!!");
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

test("appends one Gram to another", () => {
  const g1 = new Gram({ envSchema: { G1_ONLY_VAR: z.string() } }).tool({
    name: "tool1",
    description: "First tool",
    inputSchema: { someString: z.string() },
    async execute(ctx, input) {
      return ctx.json({ tool: `tool1 test: ${input.someString}` });
    },
  });

  const g2 = new Gram({ envSchema: { G2_ONLY_VAR: z.string() } }).tool({
    name: "tool2",
    description: "Second tool",
    inputSchema: { someNum: z.number() },
    async execute(ctx, input) {
      return ctx.json({ tool: `tool2 test: ${input.someNum}` });
    },
  });

  const merged = g1.extend(g2);

  expect(merged.manifest()).toEqual({
    version: "0.0.0",
    tools: [
      {
        name: "tool1",
        description: "First tool",
        inputSchema: expect.objectContaining({
          type: "object",
          properties: { someString: { type: "string" } },
          required: ["someString"],
        }),
        variables: { G1_ONLY_VAR: {} },
      },
      {
        name: "tool2",
        description: "Second tool",
        inputSchema: expect.objectContaining({
          type: "object",
          properties: { someNum: { type: "number" } },
          required: ["someNum"],
        }),
        variables: { G2_ONLY_VAR: {} },
      },
    ],
  });
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

describe("extend", () => {
  test("extends tools from another Gram instance", async () => {
    const original = new Gram().tool({
      name: "echo",
      description: "Echoes the input",
      inputSchema: { message: z.string() },
      async execute(ctx, input) {
        return ctx.json({ echoed: input.message });
      },
    });

    const other = new Gram().tool({
      name: "add",
      description: "Add two numbers",
      inputSchema: { a: z.number(), b: z.number() },
      async execute(ctx, input) {
        return ctx.json({ sum: input.a + input.b });
      },
    });

    const extended =original.extend(other);

    // Verify that g1 is mutated (not copied)
    expect(extended).toBe(original);

    // Should be able to call tools from both instances
    const res1 = await extended.handleToolCall({
      name: "echo",
      input: { message: "Hello!" },
    });
    expect(res1.status).toBe(200);
    const data1 = await res1.json();
    expect(data1).toEqual({ echoed: "Hello!" });

    const res2 = await extended.handleToolCall({
      name: "add",
      input: { a: 1, b: 2 },
    });
    expect(res2.status).toBe(200);
    const data2 = await res2.json();
    expect(data2).toEqual({ sum: 3 });
  });

  test("overrides tools with same name (last wins)", async () => {
    const g1 = new Gram().tool({
      name: "greet",
      description: "Greets the user",
      inputSchema: {},
      async execute(ctx) {
        return ctx.json({ message: "Hello from g1" });
      },
    });

    const g2 = new Gram().tool({
      name: "greet",
      description: "Greets the user differently",
      inputSchema: {},
      async execute(ctx) {
        return ctx.json({ message: "Hello from g2" });
      },
    });

    const extended =g1.extend(g2);

    const response = await extended.handleToolCall({
      name: "greet",
      input: {},
    });
    expect(response.status).toBe(200);
    const data = await response.json();
    // g2's tool should override g1's tool
    expect(data).toEqual({ message: "Hello from g2" });
  });

  test("preserves lax setting from original instance", async () => {
    const original = new Gram({ lax: true }).tool({
      name: "echo",
      inputSchema: { message: z.string() },
      async execute(ctx, input) {
        return ctx.json({ echoed: input.message });
      },
    });

    const other = new Gram({ lax: false }).tool({
      name: "add",
      inputSchema: { a: z.number(), b: z.number() },
      async execute(ctx, input) {
        return ctx.json({ sum: input.a + input.b });
      },
    });

    const extended =original.extend(other);

    // Should use g1's lax setting (true), so invalid input should pass
    const response = await extended.handleToolCall({
      name: "echo",
      input: { message: 123 } as any, // Invalid type but lax mode
    });
    expect(response.status).toBe(200);

    // Should use g2's lax setting (false), so invalid input should fail
    try {
      await extended.handleToolCall({
        name: "add",
        input: { a: "not a number", b: 2 } as any, // Invalid type, strict mode
      });
      // Should not reach here - validation should throw
      expect.fail("Expected validation to throw");
    } catch (err) {
      expect(err).toBeInstanceOf(Response);
      const response2 = err as Response;
      expect(response2.status).toBe(400);
    }
  });

  test("preserves env vars from original instances", async () => {
    const original = new Gram({
      env: { G1_VAR: "value from g1" },
      envSchema: { G1_VAR: z.string() },
    }).tool({
      name: "getG1Var",
      description: "Gets G1_VAR",
      inputSchema: {},
      async execute(ctx) {
        return ctx.json({ value: ctx.env.G1_VAR });
      },
    });

    const other = new Gram({
      env: { G2_VAR: "value from g2" },
      envSchema: { G2_VAR: z.string() },
    }).tool({
      name: "getG2Var",
      description: "Gets G2_VAR",
      inputSchema: {},
      async execute(ctx) {
        return ctx.json({ value: ctx.env.G2_VAR });
      },
    });

    const extended =original.extend(other);

    // g1's tool should still access G1_VAR
    const res1 = await extended.handleToolCall({ name: "getG1Var", input: {} });
    expect(res1.status).toBe(200);
    const data1 = await res1.json();
    expect(data1).toEqual({ value: "value from g1" });

    // g2's tool should still access G2_VAR (not G1_VAR) even when called through merged
    const res2 = await extended.handleToolCall({ name: "getG2Var", input: {} });
    expect(res2.status).toBe(200);
    const data2 = await res2.json();
    expect(data2).toEqual({ value: "value from g2" });
  });

  test("chains multiple appends", async () => {
    const original = new Gram().tool({
      name: "tool1",
      inputSchema: {},
      async execute(ctx) {
        return ctx.json({ from: "g1" });
      },
    });

    const firstOther = new Gram().tool({
      name: "tool2",
      inputSchema: {},
      async execute(ctx) {
        return ctx.json({ from: "g2" });
      },
    });

    const secondOther = new Gram().tool({
      name: "tool3",
      inputSchema: {},
      async execute(ctx) {
        return ctx.json({ from: "g3" });
      },
    });

    const extended =original.extend(firstOther).extend(secondOther);

    // Should have all three tools
    const res1 = await extended.handleToolCall({ name: "tool1", input: {} });
    expect((await res1.json()).from).toBe("g1");

    const res2 = await extended.handleToolCall({ name: "tool2", input: {} });
    expect((await res2.json()).from).toBe("g2");

    const res3 = await extended.handleToolCall({ name: "tool3", input: {} });
    expect((await res3.json()).from).toBe("g3");
  });
});
